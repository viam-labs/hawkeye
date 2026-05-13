package main

import (
	"path/filepath"

	"github.com/pkg/errors"
)

// commandArgs is the constraint satisfied by *T for any arg struct T. The
// pointer form lets validateArgs use a pointer receiver, so the defaults it sets
// reach the handler.
type commandArgs[T any] interface {
	*T
	validateArgs() error
}

// Battery

type argsStartBattery struct{}

func (*argsStartBattery) validateArgs() error { return nil }

type argsStopBattery struct{}

func (*argsStopBattery) validateArgs() error { return nil }

type argsTestBattery struct {
	DurationSecs int `mapstructure:"duration_secs"` // optional (default: 5 secs)
}

func (args *argsTestBattery) validateArgs() error {
	if args.DurationSecs == 0 {
		args.DurationSecs = 5
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

type argsTestFetch struct{}

func (*argsTestFetch) validateArgs() error { return nil }

// Gripper

type argsStartGripper struct {
	Angle int `mapstructure:"angle"` // optional: GRIPPER_ANGLE_CLOSED to GRIPPER_ANGLE_OPEN (default: GRIPPER_ANGLE_NEUTRAL)
}

func (args *argsStartGripper) validateArgs() error {
	if args.Angle == 0 {
		args.Angle = int(GRIPPER_ANGLE_NEUTRAL)
	}

	if servoDegrees(args.Angle) < GRIPPER_ANGLE_CLOSED || servoDegrees(args.Angle) > GRIPPER_ANGLE_OPEN {
		return errors.Errorf("gripper angle must be between %d (closed) and %d (open), but got %d",
			GRIPPER_ANGLE_CLOSED, GRIPPER_ANGLE_OPEN, args.Angle)
	}

	return nil
}

type argsStopGripper struct{}

func (*argsStopGripper) validateArgs() error { return nil }

type argsTestGripper struct{}

func (*argsTestGripper) validateArgs() error { return nil }

// Motor

type argsStartMotor struct{}

func (*argsStartMotor) validateArgs() error { return nil }

type argsStopMotor struct{}

func (*argsStopMotor) validateArgs() error { return nil }

type argsTestMotor struct {
	Sequence []argsTestMotorStep `mapstructure:"sequence"` // required: 1 to TEST_MOTOR_MAX_SEQUENCE_STEPS steps
}

type argsTestMotorStep struct {
	Action        string  `mapstructure:"action"`         // required: "forward", "reverse", or "brake"
	MotorAngle    int     `mapstructure:"motor_angle"`    // required by "forward" and "reverse": the raw servo angle to drive at; ignored by "brake"
	SteeringAngle int     `mapstructure:"steering_angle"` // optional: the raw servo angle to point the wheels at (default: STEERING_NEUTRAL)
	DurationSecs  float64 `mapstructure:"duration_secs"`  // optional: 1 to 10 seconds (default: TEST_MOTOR_DEFAULT_DRIVE_DURATION)
}

func (args *argsTestMotor) validateArgs() error {
	if len(args.Sequence) == 0 {
		return errors.New("sequence must have at least one step")
	}
	if len(args.Sequence) > TEST_MOTOR_MAX_SEQUENCE_STEPS {
		return errors.Errorf("sequence must have at most %d steps, but got %d",
			TEST_MOTOR_MAX_SEQUENCE_STEPS, len(args.Sequence))
	}

	var priorAction motorAction

	for i := range args.Sequence {
		if err := args.Sequence[i].validate(priorAction); err != nil {
			return errors.Wrapf(err, "sequence step #%d", i+1)
		}
		priorAction = motorAction(args.Sequence[i].Action)
	}

	return nil
}

// validate checks one step against the action before it in the sequence (empty
// for the first). Pointer receiver, called by index, so its defaults persist.
func (step *argsTestMotorStep) validate(priorAction motorAction) error {
	action := motorAction(step.Action)

	switch action {
	case MOTOR_ACTION_FORWARD, MOTOR_ACTION_REVERSE, MOTOR_ACTION_BRAKE:
	default:
		return errors.Errorf("motor action must be %q, %q, or %q",
			MOTOR_ACTION_FORWARD, MOTOR_ACTION_REVERSE, MOTOR_ACTION_BRAKE)
	}

	// A brake after a reverse drive accelerates the car instead of stopping it.
	if action == MOTOR_ACTION_BRAKE && priorAction == MOTOR_ACTION_REVERSE {
		return errors.Errorf("%q cannot follow %q: only a forward drive can be braked",
			MOTOR_ACTION_BRAKE, MOTOR_ACTION_REVERSE)
	}

	// Braking is full-strength by definition, so it carries no angle.
	if action != MOTOR_ACTION_BRAKE {
		if step.MotorAngle < TEST_MOTOR_MIN_ANGLE || step.MotorAngle > TEST_MOTOR_MAX_ANGLE {
			return errors.Errorf("motor_angle must be between %d and %d, but got %d",
				TEST_MOTOR_MIN_ANGLE, TEST_MOTOR_MAX_ANGLE, step.MotorAngle)
		}
		if action == MOTOR_ACTION_FORWARD && servoDegrees(step.MotorAngle) <= MOTOR_NEUTRAL {
			return errors.Errorf("motor_angle %d does not drive %s: it must be above MOTOR_NEUTRAL (%d)",
				step.MotorAngle, MOTOR_ACTION_FORWARD, MOTOR_NEUTRAL)
		}
		if action == MOTOR_ACTION_REVERSE && servoDegrees(step.MotorAngle) >= MOTOR_NEUTRAL {
			return errors.Errorf("motor_angle %d does not drive %s: it must be below MOTOR_NEUTRAL (%d)",
				step.MotorAngle, MOTOR_ACTION_REVERSE, MOTOR_NEUTRAL)
		}
	}

	if step.SteeringAngle == 0 {
		step.SteeringAngle = int(STEERING_NEUTRAL)
	}
	if servoDegrees(step.SteeringAngle) < STEERING_MAX_RIGHT || servoDegrees(step.SteeringAngle) > STEERING_MAX_LEFT {
		return errors.Errorf("steering_angle must be between %d and %d, but got %d",
			STEERING_MAX_RIGHT, STEERING_MAX_LEFT, step.SteeringAngle)
	}

	if step.DurationSecs == 0 {
		step.DurationSecs = TEST_MOTOR_DEFAULT_DRIVE_DURATION.Seconds()
	}
	if step.DurationSecs < 0.05 || step.DurationSecs > 10 {
		return errors.New("duration_secs must be between 0.05 and 10")
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
	Rotation int    `mapstructure:"rotation"` // optional: 0, 45, 90, or 135 — only used when msg is "tennis-ball-rolling"
	Position int    `mapstructure:"position"` // optional: 0 (centered, default), or 1-4 (left to right) — only used when msg is "tennis-ball-rolling"
	Animate  bool   `mapstructure:"animate"`  // optional: roll the ball rather than draw one frame of it — only used when msg is "tennis-ball-rolling"
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

// Steering

type argsStartSteering struct{}

func (*argsStartSteering) validateArgs() error { return nil }

type argsStopSteering struct{}

func (*argsStopSteering) validateArgs() error { return nil }

type argsTestSteering struct{}

func (*argsTestSteering) validateArgs() error { return nil }

// Vision

type argsStartVision struct {
	RecordDir string `mapstructure:"record_dir"` // optional: directory to record annotated frames into (default: don't record)
}

func (args *argsStartVision) validateArgs() error {
	if args.RecordDir == "" {
		return nil
	}

	if !filepath.IsAbs(args.RecordDir) {
		return errors.Errorf("vision record_dir must be an absolute path, but got %q", args.RecordDir)
	}

	return nil
}

type argsStopVision struct{}

func (*argsStopVision) validateArgs() error { return nil }

type argsTestVision struct {
	Detect string `mapstructure:"detect"` // optional: "single" or "pair" (default: "single")
}

func (args *argsTestVision) validateArgs() error {
	switch visionDetectionKind(args.Detect) {
	case VISION_DETECTION_SINGLE, VISION_DETECTION_PAIR:
		return nil
	default:
		return errors.Errorf("vision detect must be %q or %q, but got %q",
			VISION_DETECTION_SINGLE, VISION_DETECTION_PAIR, args.Detect)
	}
}
