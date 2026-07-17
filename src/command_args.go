package main

import "github.com/pkg/errors"

// commandArgs is the constraint satisfied by *T for any arg struct T. The
// "pointer-to-T" form lets validateArgs use a pointer receiver so defaults
// set inside it persist through to the handler.
type commandArgs[T any] interface {
	*T
	validateArgs() error
}

// Vision

type argsStartVision struct {
	Mode string `mapstructure:"mode"` // optional: "ml" (default) or "color"; hybrid tracking → use fetch instead
}

func (args *argsStartVision) validateArgs() error {
	if args.Mode == "" {
		args.Mode = VISION_MODE_ML
	}
	switch args.Mode {
	case VISION_MODE_ML, VISION_MODE_COLOR:
	default:
		return errors.Errorf("vision mode must be %q or %q (for hybrid tracking use the fetch routine)", VISION_MODE_ML, VISION_MODE_COLOR)
	}
	return nil
}

type argsStopVision struct{}

func (*argsStopVision) validateArgs() error { return nil }

type argsTestVision struct {
	Mode string `mapstructure:"mode"` // optional: "ml" (default) or "color"
}

func (args *argsTestVision) validateArgs() error {
	if args.Mode == "" {
		args.Mode = VISION_MODE_ML
	}
	switch args.Mode {
	case VISION_MODE_ML, VISION_MODE_COLOR:
	default:
		return errors.Errorf("test vision mode must be %q or %q", VISION_MODE_ML, VISION_MODE_COLOR)
	}
	return nil
}

// Steering

type argsStartSteering struct{}

func (*argsStartSteering) validateArgs() error { return nil }

type argsStopSteering struct{}

func (*argsStopSteering) validateArgs() error { return nil }

// Assumption: 0 would be a valid non-default servo angle, but way too low in practice.
type argsTestSteering struct {
	LeftAngle    float64 `mapstructure:"left_angle"`    // optional (default: STEERING_MAX_LEFT)
	RightAngle   float64 `mapstructure:"right_angle"`   // optional (default: STEERING_MAX_RIGHT)
	NeutralAngle float64 `mapstructure:"neutral_angle"` // optional (default: STEERING_NEUTRAL)
}

func (args *argsTestSteering) validateArgs() error {
	if args.LeftAngle == 0 {
		args.LeftAngle = float64(STEERING_MAX_LEFT)
	}

	if args.RightAngle == 0 {
		args.RightAngle = float64(STEERING_MAX_RIGHT)
	}

	if args.NeutralAngle == 0 {
		args.NeutralAngle = float64(STEERING_NEUTRAL)
	}

	return nil
}

// Motor

type argsStartMotor struct{}

func (*argsStartMotor) validateArgs() error { return nil }

type argsStopMotor struct{}

func (*argsStopMotor) validateArgs() error { return nil }

type argsTestMotor struct {
	Direction    string  `mapstructure:"direction"`     // required: "forward" or "reverse"
	Power        float64 `mapstructure:"power"`         // optional: 1 (lowest) to 10 (highest) (default: 3)
	DurationSecs float64 `mapstructure:"duration_secs"` // optional: 1 to 10 seconds (default: 1)
}

func (args *argsTestMotor) validateArgs() error {
	if args.Direction != string(MOTOR_DRIVE_DIRECTION_FORWARD) && args.Direction != string(MOTOR_DRIVE_DIRECTION_REVERSE) {
		return errors.Errorf("motor direction must be %q or %q", MOTOR_DRIVE_DIRECTION_FORWARD, MOTOR_DRIVE_DIRECTION_REVERSE)
	}

	if args.Power == 0 {
		args.Power = 3
	}
	if args.Power < 1 || args.Power > 10 {
		return errors.New("motor power must be between 1 and 10")
	}

	if args.DurationSecs == 0 {
		args.DurationSecs = 1
	}
	if args.DurationSecs < 1 || args.DurationSecs > 10 {
		return errors.New("duration_secs must be between 1 and 10")
	}

	return nil
}

// Screen

type argsStartScreen struct{}

func (*argsStartScreen) validateArgs() error { return nil }

type argsStopScreen struct{}

func (*argsStopScreen) validateArgs() error { return nil }

type argsTestScreen struct {
	Msg      string `mapstructure:"msg"`
	Rotation int    `mapstructure:"rotation"` // optional: 0, 45, 90, or 135 — only used when msg is "tennis"
	Position int    `mapstructure:"position"` // optional: 0 (centered, default), or 1-4 (left to right) — only used when msg is "tennis"
}

func (args *argsTestScreen) validateArgs() error {
	if len(args.Msg) == 0 || len(args.Msg) > 18 {
		return errors.New("msg must be between 1 and 18 characters")
	}

	switch args.Rotation {
	case 0, 45, 90, 135:
	default:
		return errors.New("rotation must be 0, 45, 90, or 135")
	}

	if args.Position < 0 || args.Position > 4 {
		return errors.New("position must be 0, 1, 2, 3, or 4")
	}

	return nil
}

// Battery

type argsStartBattery struct{}

func (*argsStartBattery) validateArgs() error { return nil }

type argsStopBattery struct{}

func (*argsStopBattery) validateArgs() error { return nil }

type argsTestBattery struct {
	DurationSecs int `mapstructure:"duration_secs"` // optional (default: 10 secs)
}

func (args *argsTestBattery) validateArgs() error {
	if args.DurationSecs == 0 {
		args.DurationSecs = 10
	}
	if args.DurationSecs < 1 || args.DurationSecs > 30 {
		return errors.New("duration_secs must between 1 and 30")
	}

	return nil
}

// Fetch

type argsStartFetch struct{}

func (*argsStartFetch) validateArgs() error { return nil }

type argsStopFetch struct{}

func (*argsStopFetch) validateArgs() error { return nil }

// Tracking (test-only: no argsStart/argsStop — handleTestTracking blocks for duration_secs)

type argsTestTracking struct {
	Mode         string  `mapstructure:"mode"`          // optional: "ml" (default) or "color"
	DurationSecs float64 `mapstructure:"duration_secs"` // optional: seconds to run (default: 10, max: 120)
}

func (args *argsTestTracking) validateArgs() error {
	if args.Mode == "" {
		args.Mode = VISION_MODE_ML
	}
	switch args.Mode {
	case VISION_MODE_ML, VISION_MODE_COLOR:
	default:
		return errors.Errorf("tracking mode must be %q or %q", VISION_MODE_ML, VISION_MODE_COLOR)
	}

	if args.DurationSecs == 0 {
		args.DurationSecs = TEST_TRACKING_DEFAULT_DURATION_SECS
	}
	if args.DurationSecs < 1 || args.DurationSecs > TEST_TRACKING_MAX_DURATION_SECS {
		return errors.Errorf("duration_secs must be between 1 and %d", TEST_TRACKING_MAX_DURATION_SECS)
	}

	return nil
}

// Braking (test-only: no argsStart/argsStop — handleTestBraking blocks for
// 2 * duration_secs per power phase)

type argsTestBraking struct {
	DurationSecs float64 `mapstructure:"duration_secs"` // optional: seconds to hold each forward/reverse phase (default: 2, max: 5)
}

func (args *argsTestBraking) validateArgs() error {
	if args.DurationSecs == 0 {
		args.DurationSecs = TEST_BRAKING_DEFAULT_DURATION_SECS
	}
	if args.DurationSecs < 1 || args.DurationSecs > TEST_BRAKING_MAX_DURATION_SECS {
		return errors.Errorf("duration_secs must be between 1 and %d", TEST_BRAKING_MAX_DURATION_SECS)
	}

	return nil
}
