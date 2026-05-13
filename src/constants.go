package main

import "time"

type visionPixels int

const (
	VISION_ROUTINE_NAME = "vision"

	// The rate at which the vision routine gets a detection from the Viam vision service.
	// Empirically, the elapsed time in handleTestVision takes just under 50ms.
	VISION_TICK_RATE = 50 * time.Millisecond

	// Corresponds to the width of the camera's resolution as defined in the Viam config.
	VISION_MIN_X visionPixels = 0
	VISION_MAX_X visionPixels = 640

	// The farthest and closest detection areas with which to start moving towards
	// at high speed and progressively slow down to a stop, respectively.
	VISION_MIN_DETECTION_AREA visionPixels = 150
	VISION_MAX_DETECTION_AREA visionPixels = 50_000
)

type servoDegrees int

const (
	STEERING_ROUTINE_NAME = "steering"

	// The rate at which to turn the steering to center on a detection.
	// Half the VISION_TICK_RATE ensures no missed readings and no phase drift.
	STEERING_TICK_RATE = VISION_TICK_RATE / 2

	// Empirical measurements of the servo angle for no/min/max steering angle.
	STEERING_NEUTRAL   servoDegrees = 90
	STEERING_MAX_LEFT  servoDegrees = 115
	STEERING_MAX_RIGHT servoDegrees = 75
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
	MOTOR_NEUTRAL      servoDegrees = 90
	MOTOR_FORWARD_LOW  servoDegrees = 97
	MOTOR_FORWARD_HIGH servoDegrees = 107
	MOTOR_REVERSE_LOW  servoDegrees = 81
	MOTOR_REVERSE_HIGH servoDegrees = 71

	// The aggression with which the motor decelerates when closing in on a detection.
	// A higher number means carrying more speed until the last moment (1 = linear,
	// 2 = quadratic, 3 = cubic, etc).
	MOTOR_DECELERATION_FACTOR = 1

	// Timings for the two drive states that are needed to allow
	// reverse drive after forward drive.
	MOTOR_REVERSE_BRAKE_DURATION   = 1 * time.Second
	MOTOR_REVERSE_NEUTRAL_DURATION = 1500 * time.Millisecond

	TEST_MOTOR_DRIVE_DURATION = 1 * time.Second
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
