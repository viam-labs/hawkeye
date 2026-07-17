package main

import "time"

type visionPixels int

const (
	VISION_ROUTINE_NAME = "vision"

	// The rate at which the vision routine gets a detection from the Viam vision service.
	// Empirically, the elapsed time in handleTestVision takes just under 50ms.
	VISION_TICK_RATE = 50 * time.Millisecond

	// Camera resolution as defined in the Viam config.
	VISION_MIN_X visionPixels = 0
	VISION_MAX_X visionPixels = 640
	VISION_MAX_Y              = 360

	// Horizontal pixel center of the camera frame. Shared by steeringTick's
	// PD controller and fetch's centering/correction checks.
	VISION_FRAME_CENTER_X = float64(VISION_MIN_X+VISION_MAX_X) / 2.0

	// The farthest and closest detection areas with which to start moving towards
	// at high speed and progressively slow down to a stop, respectively.
	// Scaled from 1920x1080 baseline by 1/9 (resolution dropped 3× each axis).
	VISION_MIN_DETECTION_AREA visionPixels = 10 // color detector and ml model minimum
	VISION_MAX_DETECTION_AREA visionPixels = 14_500

	// Accepted labels for tennis ball detections from the ML model.
	// VISION_BALL_LABEL_NUMERIC is emitted due to a Viam app bug where the class
	// name is replaced by its numeric index; both are accepted so either model
	// config works.
	VISION_BALL_LABEL_NUMERIC = "0"
	VISION_BALL_LABEL_NAME    = "tennis ball"

	// Minimum confidence score for a detection to be accepted.
	// Filters low-confidence ML noise. Empirical — tune based on false-positive rate.
	VISION_MIN_CONFIDENCE = 0.5

	// Vision mode selectors for argsStartVision.Mode and argsTestVision.Mode.
	// Hybrid ML+color tracking is handled by the fetch routine, not by start/stop vision.
	VISION_MODE_ML    = "ml"
	VISION_MODE_COLOR = "color"
)

type servoDegrees int

const (
	STEERING_ROUTINE_NAME = "steering"

	// The rate at which to turn the steering to center on a detection.
	// Half the VISION_TICK_RATE ensures no missed readings and no phase drift.
	STEERING_TICK_RATE = VISION_TICK_RATE / 2

	// Empirical measurements of the servo angle for no/min/max steering angle.
	// Physical neutral measured at 101 (servo installed with +11 offset from standard 90).
	STEERING_NEUTRAL   servoDegrees = 101
	STEERING_MAX_LEFT  servoDegrees = 116
	STEERING_MAX_RIGHT servoDegrees = 86
	STEERING_KP                     = 0.05  // proportional: ~matches old linear map right-side authority
	STEERING_KD                     = 0.005 // derivative: dampens oscillation on approach
)

type driveDirection string

const (
	MOTOR_ROUTINE_NAME = "motor"

	// The rate at which to adjust the motor speed when closing in on a detection.
	// Half the VISION_TICK_RATE ensures no missed readings and no phase drift.
	MOTOR_TICK_RATE = VISION_TICK_RATE / 2

	// The last known motor drive direction.
	MOTOR_DRIVE_DIRECTION_NEUTRAL driveDirection = "neutral"
	MOTOR_DRIVE_DIRECTION_FORWARD driveDirection = "forward"
	MOTOR_DRIVE_DIRECTION_REVERSE driveDirection = "reverse"

	// Empirical measurements of the servo angle for no/min/max motor power.
	MOTOR_NEUTRAL       servoDegrees = 90
	MOTOR_FORWARD_LOW   servoDegrees = 99 // adjusted up a bit to go faster
	MOTOR_FORWARD_HIGH  servoDegrees = 101
	MOTOR_REVERSE_LOW   servoDegrees = 81
	MOTOR_REVERSE_RETRY servoDegrees = 76
	MOTOR_REVERSE_HIGH  servoDegrees = 71

	// The aggression with which the motor decelerates when closing in on a detection.
	// A higher number means carrying more speed until the last moment (1 = linear,
	// 2 = quadratic, 3 = cubic, etc).
	MOTOR_DECELERATION_FACTOR = 1

	// Timings for the two drive states that are needed to allow
	// reverse drive after forward drive.
	MOTOR_REVERSE_BRAKE_DURATION   = 1 * time.Second
	MOTOR_REVERSE_NEUTRAL_DURATION = 1500 * time.Millisecond

	TEST_MOTOR_DRIVE_DURATION = 1 * time.Second

	// Duration of the ESC brake pulse applied when transitioning from forward to neutral.
	// A brief MOTOR_REVERSE_HIGH pulse bleeds momentum without entering reverse mode.
	// Empirical — tune on the bench; start at 150ms.
	MOTOR_BRAKE_PULSE_DURATION = 150 * time.Millisecond
)

const (
	SCREEN_ROUTINE_NAME = "screen"

	// The rate at which the screen routine redraws the OLED.
	// For the tennis ball animation, 500ms gives a 2s full cycle
	// with 4 positions per rolling-ball cycle.
	SCREEN_TICK_RATE = 500 * time.Millisecond

	// Hawkeye onboard OLED is a 128x32 SSD1306 on the default I2C bus at 0x3C.
	SCREEN_WIDTH  = 128
	SCREEN_HEIGHT = 32
)

const (
	BATTERY_ROUTINE_NAME = "battery"

	// PiRacer Pro's onboard INA219 power monitor is on the default I2C bus.
	BATTERY_I2C_ADDRESS = 0x42

	// The log/refresh rate of the battery readings.
	BATTERY_TICK_RATE = 1 * time.Minute

	// Voltage bounds for the PiRacer's two-series / two-parallel pack of
	// 18650 Li-ion cells. 8.4V is a freshly-charged pack; 6.0V is the
	// practical "low" before the BMS cuts off around 5.0V. Used by
	// batteryVoltageToPercent.
	BATTERY_MAX_VOLTAGE = 8.4
	BATTERY_MIN_VOLTAGE = 6.0
)

const (
	FETCH_ROUTINE_NAME = "fetch"

	// Fixed pixel margin added to each side of the ML bounding box to form the
	// color-detector search region (lock zone). A fixed margin keeps the zone tight
	// as the ball grows closer — tune this if the color detector loses the ball
	// between ML re-acquisitions.
	FETCH_LOCK_ZONE_MARGIN_PX = 50

	// Number of consecutive color-detector misses inside the lock zone before
	// switching back to ML re-acquisition. Higher = more tolerance for brief occlusions.
	FETCH_COLOR_MISS_THRESHOLD = 5

	// Minimum duration the ML model must detect a ball continuously
	FETCH_ACQUIRE_DEBOUNCE_DURATION = 1 * time.Second

	// Detection area at which the motor halts (ball is close enough to stop).
	FETCH_STOP_AREA visionPixels = 4_000

	// Stop-area threshold used instead of FETCH_STOP_AREA once at least one
	// correction attempt has happened for the current lock: a leg resuming
	// from a reverse-and-retry has less coast to close the final gap, so it
	// needs to get physically closer before it's actually "close enough".
	// Empirical — tune on hardware.
	FETCH_STOP_AREA_RETRY visionPixels = 14_000

	// Pixel tolerance from frame center within which the ball counts as
	// "directly in front" — no correction maneuver needed before halting.
	FETCH_CENTER_TOLERANCE_PX visionPixels = 30

	// Max reverse-and-retry correction attempts before halting anyway,
	// regardless of centering.
	FETCH_CORRECTION_MAX_ATTEMPTS = 10

	// Max duration of a single reverse-and-retry correction attempt.
	FETCH_CORRECTION_MAX_DURATION = 1500 * time.Millisecond

	// Poll interval for re-checking ball position during a correction attempt.
	FETCH_CORRECTION_POLL_INTERVAL = 50 * time.Millisecond

	// Blind search-drive used when fetch starts and the first ML check finds
	// no ball: drives a gentle forward arc, repolling ML, until locked on or
	// FETCH_SEARCH_MAX_DURATION elapses (then halts fetch). Empirical — tune
	// on hardware.
	FETCH_SEARCH_MAX_DURATION              = 20 * time.Second
	FETCH_SEARCH_MOTOR_SPEED  servoDegrees = 99

	// FETCH_SEARCH_SWEEP_INTERVAL, sweeping left-right instead of circling
	// one direction, so it covers a wider field of view while advancing.
	FETCH_SEARCH_STEERING_ANGLE_LEFT  servoDegrees = 106
	FETCH_SEARCH_STEERING_ANGLE_RIGHT servoDegrees = 96
	FETCH_SEARCH_SWEEP_INTERVAL                    = 1 * time.Second
)

const (
	// "tracking" is a test-only routine: no start/stop/tick, just handleTestTracking.
	// It drives the vision+steering loop synchronously so you can compare ML vs color
	// detector tracking performance in real time.
	TRACKING_ROUTINE_NAME = "tracking"

	// Default and max duration for handleTestTracking.
	TEST_TRACKING_DEFAULT_DURATION_SECS = 10
	TEST_TRACKING_MAX_DURATION_SECS     = 120
)

const (
	// "braking" is a test-only routine: no start/stop/tick, just
	// handleTestBraking. Drives forward then brakes into reverse (exercising
	// motorArmReverse's neutral-then-reverse sequence) at max power, then
	// again at normal power, so the sequence can be watched directly on
	// hardware from the DoCommand tester in the config builder UI.
	BRAKING_ROUTINE_NAME = "braking"

	// Power levels for the two phases: 10 is powerToAngle's max, 5 is a
	// representative mid-range "normal" cruise power.
	BRAKING_TEST_MAX_POWER    = 10
	BRAKING_TEST_NORMAL_POWER = 5

	// Default and max duration each drive phase (forward, then reverse) holds
	// for before moving to the next phase.
	TEST_BRAKING_DEFAULT_DURATION_SECS = 2
	TEST_BRAKING_MAX_DURATION_SECS     = 5
)
