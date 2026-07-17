package main

import (
	"context"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/viam-labs/hawkeye/util"
	"go.uber.org/multierr"
	"go.viam.com/rdk/components/camera"
	"go.viam.com/rdk/components/servo"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/generic"
	"go.viam.com/rdk/services/vision"

	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/devices/v3/ina219"
	"periph.io/x/devices/v3/ssd1306"
	"periph.io/x/host/v3"
)

var model = resource.NewModel("viam", "tennis", "hawkeye")

func init() {
	resource.RegisterService(generic.API, model, resource.Registration[resource.Resource, *Config]{
		Constructor: newHawkeye,
	})
}

func main() {
	module.ModularMain(resource.APIModel{API: generic.API, Model: model})
}

// hawkeye tracks all hardware components and routine state.
type hawkeye struct {
	resource.Named
	mainLogger logging.Logger

	// Shared I2C bus used by the screen and battery routines.
	i2cBus i2c.BusCloser

	// Vision

	cameraName            string
	visionViam            vision.Service
	visionColorViam       vision.Service // optional; wired when Config.VisionColor is set
	visionMode            string         // "ml", "color", or "hybrid"; set at start time
	visionRoutine         *util.Routine
	visionLogger          logging.Logger
	visionThrottledLogger *util.ThrottledLogger
	visionLastDetection   atomic.Pointer[visionDetection]

	// Steering

	steeringServoViam       servo.Servo
	steeringRoutine         *util.Routine
	steeringLogger          logging.Logger
	steeringThrottledLogger *util.ThrottledLogger
	steeringLastAngle       servoDegrees
	steeringPrevError       float64
	steeringPrevAt          time.Time

	// Motor

	motorServoViam       servo.Servo
	motorRoutine         *util.Routine
	motorLogger          logging.Logger
	motorThrottledLogger *util.ThrottledLogger
	motorLastAngle       servoDegrees

	// For testing: required to serialize drive direction and
	// engage the correct motor sequences when switching direction
	// (ex: after forward drive, you need to apply brake + neutral
	// before you can reverse).
	motorMutex              sync.Mutex
	motorLastDriveDirection driveDirection

	// Screen

	screenRoutine         *util.Routine
	screenLogger          logging.Logger
	screenThrottledLogger *util.ThrottledLogger
	screenDev             *ssd1306.Dev

	// For managing tennis ball animation.
	screenBallIndex     int
	screenBallDirection int

	// Battery

	batteryRoutine     *util.Routine
	batteryLogger      logging.Logger
	batteryDev         *ina219.Dev
	batteryLastReading atomic.Pointer[batteryReading]

	// Fetch (state written only by fetchVisionTick — no sync needed)

	fetchLogger             logging.Logger
	fetchThrottledLogger    *util.ThrottledLogger
	fetchState              string           // "acquiring", "tracking", "reacquiring", or "done"
	fetchLockZone           *image.Rectangle // ML bbox + margin; nil until first acquisition
	fetchColorMisses        int              // consecutive color misses inside lock zone
	fetchCorrectionAttempts int              // reverse-and-retry attempts since the last ML acquire
	fetchAcquireStreakStart time.Time        // when the current continuous ML detection streak began; zero if none
	fetchStallCheckAt       time.Time        // when the current stall-detection window started; zero if none
	fetchStallCheckArea     visionPixels     // ball area recorded at fetchStallCheckAt
}

func newHawkeye(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	mainLogger logging.Logger,
) (resource.Resource, error) {
	var (
		visionLogger   = mainLogger.Sublogger(VISION_ROUTINE_NAME)
		steeringLogger = mainLogger.Sublogger(STEERING_ROUTINE_NAME)
		motorLogger    = mainLogger.Sublogger(MOTOR_ROUTINE_NAME)
		screenLogger   = mainLogger.Sublogger(SCREEN_ROUTINE_NAME)
		batteryLogger  = mainLogger.Sublogger(BATTERY_ROUTINE_NAME)
		fetchLogger    = mainLogger.Sublogger(FETCH_ROUTINE_NAME)

		visionThrottledLogger   = util.NewThrottledLogger(visionLogger, util.LOG_EVERY_5_SECS)
		steeringThrottledLogger = util.NewThrottledLogger(steeringLogger, util.LOG_EVERY_5_SECS)
		motorThrottledLogger    = util.NewThrottledLogger(motorLogger, util.LOG_EVERY_5_SECS)
		screenThrottledLogger   = util.NewThrottledLogger(screenLogger, util.LOG_EVERY_10_SECS)
		fetchThrottledLogger    = util.NewThrottledLogger(fetchLogger, util.LOG_EVERY_5_SECS)
	)

	// Screen and battery share the I2C bus; both are non-essential so the module
	// will continue to come up if any one of the three init steps fails.
	i2cBus, screenDev, batteryDev := initI2CBusWithScreenAndBatteryDev(mainLogger, screenLogger, batteryLogger)

	h := &hawkeye{
		Named:      conf.ResourceName().AsNamed(),
		mainLogger: mainLogger,
		i2cBus:     i2cBus,
		// Vision
		visionRoutine:         util.NewRoutineInstance(VISION_ROUTINE_NAME),
		visionLogger:          visionLogger,
		visionThrottledLogger: visionThrottledLogger,
		// Steering
		steeringRoutine:         util.NewRoutineInstance(STEERING_ROUTINE_NAME),
		steeringLogger:          steeringLogger,
		steeringThrottledLogger: steeringThrottledLogger,
		steeringLastAngle:       STEERING_NEUTRAL,
		// Motor
		motorRoutine:            util.NewRoutineInstance(MOTOR_ROUTINE_NAME),
		motorLogger:             motorLogger,
		motorThrottledLogger:    motorThrottledLogger,
		motorLastDriveDirection: MOTOR_DRIVE_DIRECTION_NEUTRAL,
		// Screen
		screenRoutine:         util.NewRoutineInstance(SCREEN_ROUTINE_NAME),
		screenLogger:          screenLogger,
		screenThrottledLogger: screenThrottledLogger,
		screenDev:             screenDev,
		screenBallIndex:       0,
		screenBallDirection:   1,
		// Battery
		batteryRoutine: util.NewRoutineInstance(BATTERY_ROUTINE_NAME),
		batteryLogger:  batteryLogger,
		batteryDev:     batteryDev,
		// Fetch
		fetchLogger:          fetchLogger,
		fetchThrottledLogger: fetchThrottledLogger,
		fetchState:           "acquiring",
	}

	if err := h.Reconfigure(ctx, deps, conf); err != nil {
		if h.i2cBus != nil {
			_ = h.i2cBus.Close()
		}
		return nil, err
	}

	// Run one screen tick to display the Viam logo.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	h.screenTick(ctx)

	// Set the steering to neutral.
	err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil)
	if err != nil {
		h.steeringLogger.Errorf("error initializing steering servo to neutral angle %d: %v", STEERING_NEUTRAL, err)
	}

	return h, nil
}

// initI2CBusWithScreenAndBatteryDev opens the default I2C bus and initializes
// the SSD1306 and INA219 controllers that sit on it. Each of the three steps
// is independent — failures are logged via the appropriate sublogger and the
// returned values may be nil so the rest of the module can keep running.
func initI2CBusWithScreenAndBatteryDev(
	logger, screenLogger, batteryLogger logging.Logger,
) (i2c.BusCloser, *ssd1306.Dev, *ina219.Dev) {
	if _, err := host.Init(); err != nil {
		logger.Errorf("error opening periph host for I2C bus; screen and battery will be unavailable: %v", err)
		return nil, nil, nil
	}

	i2cBus, err := i2creg.Open("")
	if err != nil {
		logger.Errorf("error opening default I2C bus; screen and battery will be unavailable: %v", err)
		return nil, nil, nil
	}

	screenDev, err := ssd1306.NewI2C(i2cBus, &ssd1306.Opts{
		W:          SCREEN_WIDTH,
		H:          SCREEN_HEIGHT,
		Rotated:    false,
		Sequential: true,
	})
	if err != nil {
		screenLogger.Errorf("error initializing SSD1306 controller for screen: %v", err)
		screenDev = nil
	}

	batteryDev, err := ina219.New(i2cBus, &ina219.Opts{Address: BATTERY_I2C_ADDRESS})
	if err != nil {
		batteryLogger.Errorf("error initializing INA219 controller at 0x%X for battery readings: %v", BATTERY_I2C_ADDRESS, err)
		batteryDev = nil
	}

	return i2cBus, screenDev, batteryDev
}

func (h *hawkeye) Reconfigure(ctx context.Context, deps resource.Dependencies, c resource.Config) error {
	h.Close(ctx)

	cfg, err := resource.NativeConfig[*Config](c)
	if err != nil {
		return err
	}

	var (
		cameraDep, cameraErr               = camera.FromProvider(deps, cfg.Camera)
		visionDep, visionErr               = vision.FromProvider(deps, cfg.Vision)
		servoSteeringDep, servoSteeringErr = servo.FromProvider(deps, cfg.ServoSteering)
		servoMotorDep, servoMotorErr       = servo.FromProvider(deps, cfg.ServoMotor)
	)

	err = multierr.Combine(cameraErr, visionErr, servoSteeringErr, servoMotorErr)
	if err != nil {
		return errors.Wrapf(err, "one or more dependencies failed to reconfigure")
	}

	h.cameraName = cameraDep.Name().Name
	h.visionViam = visionDep
	h.steeringServoViam = servoSteeringDep
	h.motorServoViam = servoMotorDep

	// VisionColor is optional. If configured but unavailable, log and continue —
	// "color" and "hybrid" modes will fall back to the ML detector.
	h.visionColorViam = nil
	if cfg.VisionColor != "" {
		colorDep, colorErr := vision.FromProvider(deps, cfg.VisionColor)
		if colorErr != nil {
			h.visionLogger.Errorf("configured vision_color %q is unavailable; color/hybrid mode will fall back to ML: %v", cfg.VisionColor, colorErr)
		} else {
			h.visionColorViam = colorDep
		}
	}

	return nil
}

func (h *hawkeye) Close(ctx context.Context) error {
	_ = h.visionRoutine.Stop()
	_ = h.steeringRoutine.Stop()
	_ = h.motorRoutine.Stop()
	_ = h.screenRoutine.Stop()
	_ = h.batteryRoutine.Stop()
	return nil
}

// Config is the hawkeye's Viam config with dependencies. VisionColor is optional;
// all other dependencies are required.
type Config struct {
	Camera        string `json:"camera"`
	Vision        string `json:"vision"`
	VisionColor   string `json:"vision_color"` // optional: color detector service for "color"/"hybrid" mode
	ServoSteering string `json:"servo_steering"`
	ServoMotor    string `json:"servo_motor"`
}

// Validate checks required fields and declares implicit dependencies. The
// gripper is optional (for now), so the config is validated field-by-field rather than
// with EnsureAllStructFieldsAreSet (which would force every field to be set).
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	for _, dep := range []struct{ attr, name string }{
		{"camera", cfg.Camera},
		{"vision", cfg.Vision},
		{"servo_steering", cfg.ServoSteering},
		{"servo_motor", cfg.ServoMotor},
	} {
		if dep.name == "" {
			return nil, nil, errors.Errorf("%s: missing required attribute %q in Viam config", path, dep.attr)
		}
	}

	requiredDeps := []string{cfg.Camera, cfg.Vision, cfg.ServoSteering, cfg.ServoMotor}

	var optionalDeps []string
	if cfg.VisionColor != "" {
		optionalDeps = append(optionalDeps, cfg.VisionColor)
	}

	return requiredDeps, optionalDeps, nil
}
