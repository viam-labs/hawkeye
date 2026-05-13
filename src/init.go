package main

import (
	"context"
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

	// Shared I2C bus used by the battery and screen routines.
	i2cBus i2c.BusCloser

	// Battery

	batteryRoutine     *util.Routine
	batteryLogger      logging.Logger
	batteryDev         *ina219.Dev
	batteryLastReading atomic.Pointer[batteryReading]

	// Fetch

	fetchRoutine         *util.Routine
	fetchLogger          logging.Logger
	fetchThrottledLogger *util.ThrottledLogger
	fetchState           atomic.Pointer[fetchState]

	fetchSeekTotalDriveDuration time.Duration
	fetchSeekStintStartTime     time.Time
	fetchSeekCoastStartTime     time.Time

	fetchEvaluateChaseStartTime time.Time
	fetchEvaluateChaseStable    *fetchStableWindow

	fetchGripInReachSince time.Time

	fetchKPointTurnLegIndex int

	fetchEvaluateDeliverStartTime time.Time
	fetchEvaluateDeliverArming    sync.WaitGroup

	// Gripper

	gripperServoViam       servo.Servo
	gripperRoutine         *util.Routine
	gripperLogger          logging.Logger
	gripperThrottledLogger *util.ThrottledLogger
	gripperLastAngle       servoDegrees
	gripperDesiredAngle    atomic.Pointer[servoDegrees]

	// Motor

	motorServoViam          servo.Servo
	motorRoutine            *util.Routine
	motorLogger             logging.Logger
	motorThrottledLogger    *util.ThrottledLogger
	motorLastAngle          servoDegrees
	motorDesiredAngle       atomic.Pointer[servoDegrees]
	motorLastDriveDirection driveDirection

	// Screen

	screenRoutine         *util.Routine
	screenLogger          logging.Logger
	screenThrottledLogger *util.ThrottledLogger
	screenDev             *ssd1306.Dev
	screenDesiredImage    atomic.Pointer[screenImage]
	screenBallIndex       int
	screenBallDirection   int

	// Steering

	steeringServoViam       servo.Servo
	steeringRoutine         *util.Routine
	steeringLogger          logging.Logger
	steeringThrottledLogger *util.ThrottledLogger
	steeringLastAngle       servoDegrees
	steeringDesiredAngle    atomic.Pointer[servoDegrees]

	// Vision

	cameraName       string
	visionBallViam   vision.Service
	visionPersonViam vision.Service
	visionViam       atomic.Pointer[vision.Service]

	visionRoutine         *util.Routine
	visionLogger          logging.Logger
	visionThrottledLogger *util.ThrottledLogger
	visionLastDetection   atomic.Pointer[visionDetection]

	visionRecordDir           atomic.Pointer[string]
	visionRecordLastFrameTime time.Time
}

func newHawkeye(
	ctx context.Context,
	deps resource.Dependencies,
	conf resource.Config,
	mainLogger logging.Logger,
) (resource.Resource, error) {
	var (
		batteryLogger  = mainLogger.Sublogger(BATTERY_ROUTINE_NAME)
		fetchLogger    = mainLogger.Sublogger(FETCH_ROUTINE_NAME)
		gripperLogger  = mainLogger.Sublogger(GRIPPER_ROUTINE_NAME)
		motorLogger    = mainLogger.Sublogger(MOTOR_ROUTINE_NAME)
		screenLogger   = mainLogger.Sublogger(SCREEN_ROUTINE_NAME)
		steeringLogger = mainLogger.Sublogger(STEERING_ROUTINE_NAME)
		visionLogger   = mainLogger.Sublogger(VISION_ROUTINE_NAME)

		fetchThrottledLogger    = util.NewThrottledLogger(fetchLogger, util.LOG_EVERY_5_SECS)
		gripperThrottledLogger  = util.NewThrottledLogger(gripperLogger, util.LOG_EVERY_5_SECS)
		motorThrottledLogger    = util.NewThrottledLogger(motorLogger, util.LOG_EVERY_5_SECS)
		screenThrottledLogger   = util.NewThrottledLogger(screenLogger, util.LOG_EVERY_10_SECS)
		steeringThrottledLogger = util.NewThrottledLogger(steeringLogger, util.LOG_EVERY_5_SECS)
		visionThrottledLogger   = util.NewThrottledLogger(visionLogger, util.LOG_EVERY_5_SECS)
	)

	// Both are non-essential, so the module comes up even if any of the three init steps fails.
	i2cBus, batteryDev, screenDev := initI2CBusWithBatteryAndScreenDev(mainLogger, batteryLogger, screenLogger)

	h := &hawkeye{
		Named:      conf.ResourceName().AsNamed(),
		mainLogger: mainLogger,
		i2cBus:     i2cBus,
		// Battery
		batteryRoutine: util.NewRoutineInstance(BATTERY_ROUTINE_NAME),
		batteryLogger:  batteryLogger,
		batteryDev:     batteryDev,
		// Fetch
		fetchRoutine:         util.NewRoutineInstance(FETCH_ROUTINE_NAME),
		fetchLogger:          fetchLogger,
		fetchThrottledLogger: fetchThrottledLogger,
		// Gripper
		gripperRoutine:         util.NewRoutineInstance(GRIPPER_ROUTINE_NAME),
		gripperLogger:          gripperLogger,
		gripperThrottledLogger: gripperThrottledLogger,
		gripperLastAngle:       GRIPPER_ANGLE_NEUTRAL,
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
		// Steering
		steeringRoutine:         util.NewRoutineInstance(STEERING_ROUTINE_NAME),
		steeringLogger:          steeringLogger,
		steeringThrottledLogger: steeringThrottledLogger,
		steeringLastAngle:       STEERING_NEUTRAL,
		// Vision
		visionRoutine:         util.NewRoutineInstance(VISION_ROUTINE_NAME),
		visionLogger:          visionLogger,
		visionThrottledLogger: visionThrottledLogger,
	}

	if err := h.Reconfigure(ctx, deps, conf); err != nil {
		if h.i2cBus != nil {
			_ = h.i2cBus.Close()
		}
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Run one screen tick to display the Viam logo.
	h.screenTick(ctx)

	// Set the steering and gripper to neutral.
	err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil)
	if err != nil {
		h.steeringLogger.Errorf("error initializing steering servo to neutral angle %d: %v", STEERING_NEUTRAL, err)
	}
	err = h.gripperServoViam.Move(ctx, uint32(GRIPPER_ANGLE_NEUTRAL), nil)
	if err != nil {
		h.gripperLogger.Errorf("error initializing gripper servo to neutral angle %d: %v", GRIPPER_ANGLE_NEUTRAL, err)
	}

	return h, nil
}

// initI2CBusWithBatteryAndScreenDev opens the default I2C bus and initializes the
// INA219 and SSD1306 on it. Each step is independent: failures log through the
// matching sublogger and return nil, so the module keeps running without them.
func initI2CBusWithBatteryAndScreenDev(
	logger, batteryLogger, screenLogger logging.Logger,
) (i2c.BusCloser, *ina219.Dev, *ssd1306.Dev) {
	if _, err := host.Init(); err != nil {
		logger.Errorf("error opening periph host for I2C bus; battery and screen will be unavailable: %v", err)
		return nil, nil, nil
	}

	i2cBus, err := i2creg.Open("")
	if err != nil {
		logger.Errorf("error opening default I2C bus; battery and screen will be unavailable: %v", err)
		return nil, nil, nil
	}

	batteryDev, err := ina219.New(i2cBus, &ina219.Opts{Address: BATTERY_I2C_ADDRESS})
	if err != nil {
		batteryLogger.Errorf("error initializing INA219 controller at 0x%X for battery readings: %v", BATTERY_I2C_ADDRESS, err)
		batteryDev = nil
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

	return i2cBus, batteryDev, screenDev
}

func (h *hawkeye) Reconfigure(ctx context.Context, deps resource.Dependencies, c resource.Config) error {
	h.Close(ctx)

	cfg, err := resource.NativeConfig[*Config](c)
	if err != nil {
		return err
	}

	var (
		cameraDep, cameraErr               = camera.FromProvider(deps, cfg.Camera)
		servoGripperDep, servoGripperErr   = servo.FromProvider(deps, cfg.ServoGripper)
		servoMotorDep, servoMotorErr       = servo.FromProvider(deps, cfg.ServoMotor)
		servoSteeringDep, servoSteeringErr = servo.FromProvider(deps, cfg.ServoSteering)
		visionBallDep, visionBallErr       = vision.FromProvider(deps, cfg.VisionBall)
		visionPersonDep, visionPersonErr   = vision.FromProvider(deps, cfg.VisionPerson)
	)

	err = multierr.Combine(
		cameraErr,
		servoGripperErr,
		servoMotorErr,
		servoSteeringErr,
		visionBallErr,
		visionPersonErr,
	)
	if err != nil {
		return errors.Wrapf(err, "one or more dependencies failed to reconfigure")
	}

	h.cameraName = cameraDep.Name().Name
	h.gripperServoViam = servoGripperDep
	h.motorServoViam = servoMotorDep
	h.steeringServoViam = servoSteeringDep
	h.visionBallViam = visionBallDep
	h.visionPersonViam = visionPersonDep

	h.useVisionBall()

	return nil
}

func (h *hawkeye) Close(ctx context.Context) error {
	_ = h.batteryRoutine.Stop()
	_ = h.fetchRoutine.Stop()
	_ = h.gripperRoutine.Stop()
	_ = h.motorRoutine.Stop()
	_ = h.screenRoutine.Stop()
	_ = h.steeringRoutine.Stop()
	_ = h.visionRoutine.Stop()

	h.fetchEvaluateDeliverArming.Wait()

	h.fetchState.Store(nil)
	h.resetFetchRun()
	h.motorDesiredAngle.Store(nil)
	h.screenDesiredImage.Store(nil)
	h.steeringDesiredAngle.Store(nil)
	h.visionViam.Store(nil)
	h.stopVisionRecording()

	return nil
}

// Config is the hawkeye's Viam config with dependencies.
type Config struct {
	Camera        string `json:"camera"`
	ServoGripper  string `json:"servo_gripper"`
	ServoMotor    string `json:"servo_motor"`
	ServoSteering string `json:"servo_steering"`
	VisionBall    string `json:"vision_ball"`
	VisionPerson  string `json:"vision_person"`
}

// Validate checks required fields and declares implicit dependencies.
func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if err := util.EnsureAllStructFieldsAreSet(*cfg); err != nil {
		return nil, nil, errors.Wrapf(err, "%s: missing attribute in Viam config", path)
	}

	requiredDeps := []string{
		cfg.Camera,
		cfg.ServoGripper,
		cfg.ServoMotor,
		cfg.ServoSteering,
		cfg.VisionBall,
		cfg.VisionPerson,
	}

	optionalDeps := []string{}

	return requiredDeps, optionalDeps, nil
}
