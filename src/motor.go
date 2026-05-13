package main

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

func (h *hawkeye) handleStartMotor(_ argsStartMotor) (map[string]any, error) {
	err := h.motorRoutine.Start(h.motorLogger, h.motorTick, MOTOR_TICK_RATE)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "started"}, err
}

func (h *hawkeye) handleStopMotor(_ argsStopMotor) (map[string]any, error) {
	err := h.motorRoutine.Stop()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil)
	if err != nil {
		h.steeringLogger.Warnf("error resetting motor servo back to neutral angle %d after stopping %s routine: %v",
			MOTOR_NEUTRAL, MOTOR_ROUTINE_NAME, err)
	}

	return map[string]any{"status": "stopped"}, nil
}

// motorTick drives the motor servo to whatever angle was last published on
// motorDesiredAngle. An unset angle means nothing is driving the car, treated as
// neutral so the throttle drops rather than staying where it was left.
func (h *hawkeye) motorTick(ctx context.Context) {
	desiredAngle := MOTOR_NEUTRAL
	if requestedAngle := h.motorDesiredAngle.Load(); requestedAngle != nil {
		desiredAngle = *requestedAngle
	}

	if desiredAngle == h.motorLastAngle {
		h.motorThrottledLogger.Infof("no change in motor servo angle %d; skipping move", desiredAngle)
		return
	}

	err := h.motorServoViam.Move(ctx, uint32(desiredAngle), nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.motorLogger.Info("stopping due to context cancellation")
			return
		}

		h.motorThrottledLogger.Warnf("error powering motor servo with angle %d: %v", desiredAngle, err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	h.motorLastAngle = desiredAngle
	h.motorLastDriveDirection = convertMotorAngleToDriveDirection(desiredAngle, h.motorLastDriveDirection)
	h.motorThrottledLogger.Infof("powered motor servo with angle %d", desiredAngle)
}

func convertMotorAngleToDriveDirection(angle servoDegrees, priorDirection driveDirection) driveDirection {
	switch {
	case angle > MOTOR_NEUTRAL:
		return MOTOR_DRIVE_DIRECTION_FORWARD
	case angle < MOTOR_NEUTRAL && priorDirection != MOTOR_DRIVE_DIRECTION_FORWARD:
		return MOTOR_DRIVE_DIRECTION_REVERSE
	default:
		return MOTOR_DRIVE_DIRECTION_NEUTRAL
	}
}

// handleTestMotor runs args.Sequence one step at a time, returning the motor to
// neutral when it finishes or fails.
func (h *hawkeye) handleTestMotor(args argsTestMotor) (map[string]any, error) {
	defer h.motorNeutral()
	defer h.steeringNeutral()

	ctx, cancel := context.WithTimeout(context.Background(), computeTestMotorSequenceTimeout(args.Sequence))
	defer cancel()

	stepResults := make([]map[string]any, 0, len(args.Sequence))
	for i, step := range args.Sequence {
		stepResult, err := h.runTestMotorStep(ctx, step)
		if err != nil {
			return nil, errors.Wrapf(err, "error running sequence step #%d (%s)", i+1, step.Action)
		}
		stepResults = append(stepResults, stepResult)
	}

	return map[string]any{"status": "ok", "steps": stepResults}, nil
}

// computeTestMotorSequenceTimeout sizes the deadline from the sequence's own
// timings, plus the brake tap and settle each step can add, plus headroom.
func computeTestMotorSequenceTimeout(sequence []argsTestMotorStep) time.Duration {
	timeout := TEST_MOTOR_SEQUENCE_TIMEOUT_HEADROOM
	for _, step := range sequence {
		timeout += time.Duration(step.DurationSecs * float64(time.Second))
		timeout += MOTOR_REVERSE_BRAKE_DURATION + MOTOR_REVERSE_NEUTRAL_DURATION
	}
	return timeout
}

// runTestMotorStep drives one step of a test sequence. Reversing after a forward
// drive needs the WP-1625's brake-tap arming first (see armMotorForReverse); from
// any other prior state the motor is stopped, so one drive pulse suffices.
func (h *hawkeye) runTestMotorStep(ctx context.Context, step argsTestMotorStep) (map[string]any, error) {
	var (
		action        = motorAction(step.Action)
		duration      = time.Duration(step.DurationSecs * float64(time.Second))
		steeringAngle = servoDegrees(step.SteeringAngle)
	)

	// Point the wheels before anything rolls. validateArgs defaults an unset
	// steering_angle to STEERING_NEUTRAL, so a silent step straightens out.
	h.motorLogger.Infof("moving steering servo to angle %d for %s step", steeringAngle, action)
	if err := h.steeringServoViam.Move(ctx, uint32(steeringAngle), nil); err != nil {
		return nil, errors.Wrapf(err, "error moving steering servo to angle %d", steeringAngle)
	}

	if action == MOTOR_ACTION_BRAKE {
		brakeResult, err := h.runTestMotorBrake(ctx, duration)
		if brakeResult != nil {
			brakeResult["steering_angle"] = int(steeringAngle)
		}
		return brakeResult, err
	}

	var (
		angle             = servoDegrees(step.MotorAngle)
		newDriveDirection = MOTOR_DRIVE_DIRECTION_FORWARD
	)
	if action == MOTOR_ACTION_REVERSE {
		newDriveDirection = MOTOR_DRIVE_DIRECTION_REVERSE
	}

	h.motorLogger.Infof("doing %s motor drive with angle %d for %s (prior=%s)",
		action, angle, duration, h.motorLastDriveDirection)

	brakeTapRequired := (action == MOTOR_ACTION_REVERSE && h.motorLastDriveDirection == MOTOR_DRIVE_DIRECTION_FORWARD)
	if brakeTapRequired {
		if err := h.armMotorForReverse(ctx); err != nil {
			return nil, err
		}
	}

	if err := h.motorServoViam.Move(ctx, uint32(angle), nil); err != nil {
		return nil, errors.Wrapf(err, "error doing %s motor drive with angle %d", action, angle)
	}
	time.Sleep(duration)

	h.motorLastDriveDirection = newDriveDirection

	return map[string]any{
		"action":         step.Action,
		"motor_angle":    int(angle),
		"steering_angle": int(steeringAngle),
		"duration_secs":  step.DurationSecs,
		"brake_tapped":   brakeTapRequired,
	}, nil
}

// runTestMotorBrake pulses MOTOR_REVERSE_HIGH for duration, then settles in
// neutral. The settle leaves the ESC ready for the next step, so the arming
// pattern still holds when a "brake" precedes a "reverse".
//
// Only a forward drive can be braked: the ESC has no braking state to enter from
// reverse, where the same pulse just accelerates the car forward. validateArgs
// rejects that within a sequence; this guard covers the direction left behind by
// an earlier command.
func (h *hawkeye) runTestMotorBrake(ctx context.Context, duration time.Duration) (map[string]any, error) {
	priorDriveDirection := h.motorLastDriveDirection

	if priorDriveDirection != MOTOR_DRIVE_DIRECTION_FORWARD {
		h.motorLogger.Infof("skipping brake: motor is not driving forward (prior=%s)", priorDriveDirection)
		return map[string]any{
			"action":  string(MOTOR_ACTION_BRAKE),
			"skipped": true,
			"reason":  "motor is not driving forward",
			"prior":   string(priorDriveDirection),
		}, nil
	}

	h.motorLogger.Infof("braking motor with angle %d for %s (prior=%s)",
		MOTOR_REVERSE_HIGH, duration, priorDriveDirection)

	if err := h.motorServoViam.Move(ctx, uint32(MOTOR_REVERSE_HIGH), nil); err != nil {
		return nil, errors.Wrapf(err, "error braking motor with angle %d", MOTOR_REVERSE_HIGH)
	}
	time.Sleep(duration)

	if err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil); err != nil {
		return nil, errors.Wrap(err, "error settling motor in neutral after braking")
	}
	time.Sleep(MOTOR_REVERSE_NEUTRAL_DURATION)

	h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_NEUTRAL

	return map[string]any{
		"action":        string(MOTOR_ACTION_BRAKE),
		"motor_angle":   int(MOTOR_REVERSE_HIGH),
		"duration_secs": duration.Seconds(),
		"skipped":       false,
	}, nil
}

// armMotorForReverse runs the pattern the WP-1625's Fwd/Br/Rev mode requires
// before accepting reverse after a forward drive: brake pulse, then settle.
func (h *hawkeye) armMotorForReverse(ctx context.Context) error {
	if err := h.motorServoViam.Move(ctx, uint32(MOTOR_REVERSE_HIGH), nil); err != nil {
		return errors.Wrap(err, "error brake tapping motor using MOTOR_REVERSE_HIGH")
	}
	time.Sleep(MOTOR_REVERSE_BRAKE_DURATION)

	if err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil); err != nil {
		return errors.Wrap(err, "error settling motor in neutral using MOTOR_NEUTRAL")
	}
	time.Sleep(MOTOR_REVERSE_NEUTRAL_DURATION)

	return nil
}

// motorNeutral returns the motor to neutral on a fresh context, so cleanup still
// runs after the request context is cancelled.
func (h *hawkeye) motorNeutral() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil); err != nil {
		h.motorLogger.Warnf("failed to return motor to neutral: %v", err)
	}
}
