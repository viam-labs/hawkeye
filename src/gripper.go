package main

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/viam-labs/hawkeye/util"
)

func (h *hawkeye) handleStartGripper(args argsStartGripper) (map[string]any, error) {
	err := h.gripperRoutine.Start(h.gripperLogger, h.gripperTick, GRIPPER_TICK_RATE)
	if err != nil {
		return nil, err
	}

	h.gripperDesiredAngle.Store(util.Ptr(servoDegrees(args.Angle)))

	return map[string]any{"status": "started", "angle": args.Angle}, nil
}

func (h *hawkeye) handleStopGripper(_ argsStopGripper) (map[string]any, error) {
	err := h.gripperRoutine.Stop()
	if err != nil {
		return nil, err
	}
	h.gripperDesiredAngle.Store(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = h.gripperServoViam.Move(ctx, uint32(GRIPPER_ANGLE_NEUTRAL), nil)
	if err != nil {
		h.gripperLogger.Warnf("error resetting gripper servo back to open angle %d after stopping %s routine: %v",
			GRIPPER_ANGLE_NEUTRAL, GRIPPER_ROUTINE_NAME, err)
	} else {
		h.gripperLastAngle = GRIPPER_ANGLE_NEUTRAL
	}

	return map[string]any{"status": "stopped"}, nil
}

// gripperTick moves the gripper to gripperDesiredAngle, skipping the move when
// the jaws are already there.
//
// Unlike steering and motor, an unset angle leaves the servo alone rather than
// falling back to neutral: the jaws are usually closed around the ball, and
// neutral would drop it.
func (h *hawkeye) gripperTick(ctx context.Context) {
	desiredAngle := h.gripperDesiredAngle.Load()

	if desiredAngle == nil {
		h.gripperThrottledLogger.Infof("found no requested gripper angle; leaving gripper servo at angle %d", h.gripperLastAngle)
		return
	}

	if *desiredAngle == h.gripperLastAngle {
		h.gripperThrottledLogger.Infof("no change in gripper servo angle %d; skipping move", *desiredAngle)
		return
	}

	err := h.gripperServoViam.Move(ctx, uint32(*desiredAngle), nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.gripperLogger.Info("stopping due to context cancellation")
			return
		}

		h.gripperLogger.Warnf("error moving gripper servo to angle %d: %v", *desiredAngle, err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	h.gripperLastAngle = *desiredAngle
	h.gripperThrottledLogger.Infof("moved gripper servo to angle %d", *desiredAngle)
}

func (h *hawkeye) handleTestGripper(_ argsTestGripper) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.gripperLogger.Infof("closing gripper servo (angle: %d)", GRIPPER_ANGLE_CLOSED)
	err := h.gripperServoViam.Move(ctx, uint32(GRIPPER_ANGLE_CLOSED), nil)
	if err != nil {
		h.gripperLogger.Warnf("error closing gripper servo (angle: %d): %v", GRIPPER_ANGLE_CLOSED, err)
	}

	time.Sleep(1 * time.Second)

	h.gripperLogger.Infof("opening gripper servo (angle: %d)", GRIPPER_ANGLE_OPEN)
	err = h.gripperServoViam.Move(ctx, uint32(GRIPPER_ANGLE_OPEN), nil)
	if err != nil {
		h.gripperLogger.Warnf("error opening gripper servo (angle: %d): %v", GRIPPER_ANGLE_OPEN, err)
	}

	return map[string]any{"status": "ok"}, nil
}
