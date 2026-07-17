package main

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

// handleTestBraking drives forward then brakes into reverse — exercising
// motorArmReverse's neutral-then-reverse arming sequence, the same one
// motorNeedsArmReverse gates in handleTestMotor — first at
// BRAKING_TEST_MAX_POWER, then again at BRAKING_TEST_NORMAL_POWER, so the
// sequence can be watched directly on hardware (e.g. from the DoCommand
// tester in the config builder UI) instead of only unit-tested in isolation.
func (h *hawkeye) handleTestBraking(args argsTestBraking) (map[string]any, error) {
	if !h.motorMutex.TryLock() {
		return nil, errors.New("motor command already running")
	}
	defer h.motorMutex.Unlock()
	defer h.motorNeutral()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	phases := []struct {
		label string
		power float64
	}{
		{"max", BRAKING_TEST_MAX_POWER},
		{"normal", BRAKING_TEST_NORMAL_POWER},
	}
	holdDuration := time.Duration(args.DurationSecs) * time.Second

	for _, phase := range phases {
		forwardAngle := powerToAngle(phase.power, MOTOR_FORWARD_LOW, MOTOR_FORWARD_HIGH)
		h.motorLogger.Infof("[braking test] driving forward at %s power (angle=%d)", phase.label, forwardAngle)
		if err := h.motorServoViam.Move(ctx, uint32(forwardAngle), nil); err != nil {
			return nil, errors.Wrapf(err, "error driving forward at %s power", phase.label)
		}
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_FORWARD
		time.Sleep(holdDuration)

		h.motorLogger.Infof("[braking test] braking to reverse at %s power — arming first", phase.label)
		if err := h.motorArmReverse(ctx); err != nil {
			return nil, errors.Wrapf(err, "error arming reverse at %s power", phase.label)
		}

		reverseAngle := powerToAngle(phase.power, MOTOR_REVERSE_LOW, MOTOR_REVERSE_HIGH)
		h.motorLogger.Infof("[braking test] driving reverse at %s power (angle=%d)", phase.label, reverseAngle)
		if err := h.motorServoViam.Move(ctx, uint32(reverseAngle), nil); err != nil {
			return nil, errors.Wrapf(err, "error driving reverse at %s power", phase.label)
		}
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_REVERSE
		time.Sleep(holdDuration)

		h.motorLogger.Infof("[braking test] settling to neutral before next phase")
		h.motorNeutral()
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_NEUTRAL
		time.Sleep(500 * time.Millisecond)
	}

	return map[string]any{"status": "ok"}, nil
}
