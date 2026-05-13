package main

import (
	"context"
	"time"

	"github.com/pkg/errors"
)

func (h *hawkeye) handleStartSteering(_ argsStartSteering) (map[string]any, error) {
	err := h.steeringRoutine.Start(h.steeringLogger, h.steeringTick, STEERING_TICK_RATE)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "started"}, err
}

func (h *hawkeye) handleStopSteering(_ argsStopSteering) (map[string]any, error) {
	err := h.steeringRoutine.Stop()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil)
	if err != nil {
		h.steeringLogger.Warnf("error resetting steering servo back to neutral angle %d after stopping %s routine: %v",
			STEERING_NEUTRAL, STEERING_ROUTINE_NAME, err)
	}

	return map[string]any{"status": "stopped"}, nil
}

// steeringNeutral straightens the wheels on a fresh context, so cleanup still
// runs after the request context is cancelled.
func (h *hawkeye) steeringNeutral() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil); err != nil {
		h.steeringLogger.Warnf("failed to return steering to neutral: %v", err)
	}
}

// steeringTick moves the steering servo to whatever angle was last published on
// steeringDesiredAngle. An unset angle means nothing is steering the car, treated
// as neutral so the wheels re-center rather than holding their last turn.
func (h *hawkeye) steeringTick(ctx context.Context) {
	desiredAngle := STEERING_NEUTRAL
	if requestedAngle := h.steeringDesiredAngle.Load(); requestedAngle != nil {
		desiredAngle = *requestedAngle
	}

	if desiredAngle == h.steeringLastAngle {
		h.steeringThrottledLogger.Infof("no change in steering servo angle %d; skipping move", desiredAngle)
		return
	}

	err := h.steeringServoViam.Move(ctx, uint32(desiredAngle), nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.steeringLogger.Info("stopping due to context cancellation")
			return
		}

		h.steeringLogger.Warnf("error moving steering servo to angle %d: %s", desiredAngle, err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	h.steeringLastAngle = desiredAngle
	h.steeringThrottledLogger.Infof("moved steering servo to angle %d", desiredAngle)
}

func (h *hawkeye) handleTestSteering(args argsTestSteering) (map[string]any, error) {
	return nil, errors.New("use the motor test to test steering")
}
