package main

import (
	"context"
	"math"
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

// steeringTick reads visionLastDetection and drives the steering servo via a PD
// controller. Error = (centerX − frameCenter) in pixels; positive means the ball
// is right of center, which requires a lower servo angle to steer right.
func (h *hawkeye) steeringTick(ctx context.Context) {
	lastDetection := h.visionLastDetection.Load()

	if lastDetection == nil {
		// Reset PD state so a stale error doesn't bias the next acquisition.
		h.steeringPrevError = 0
		h.steeringPrevAt = time.Time{}

		if h.steeringLastAngle == STEERING_NEUTRAL {
			h.steeringThrottledLogger.Infof("found no vision detection to move steering servo; already at neutral angle %d", STEERING_NEUTRAL)
			return
		}

		if err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil); err != nil {
			h.steeringLogger.Warnf("error resetting steering servo back to neutral angle %d after no detection: %v", STEERING_NEUTRAL, err)
			return
		}

		h.steeringLastAngle = STEERING_NEUTRAL
		h.steeringThrottledLogger.Infof("found no vision detection to move steering servo; reset back to neutral angle %d", STEERING_NEUTRAL)
		return
	}

	now := time.Now()
	dt := STEERING_TICK_RATE.Seconds()
	if !h.steeringPrevAt.IsZero() {
		dt = now.Sub(h.steeringPrevAt).Seconds()
	}
	h.steeringPrevAt = now

	pidErr := float64(lastDetection.centerX) - VISION_FRAME_CENTER_X

	p := STEERING_KP * pidErr

	d := 0.0
	if dt > 0 {
		d = STEERING_KD * (pidErr - h.steeringPrevError) / dt
	}
	h.steeringPrevError = pidErr

	output := p + d
	raw := float64(STEERING_NEUTRAL) - output
	clamped := math.Max(float64(STEERING_MAX_RIGHT), math.Min(float64(STEERING_MAX_LEFT), raw))
	centerAngle := servoDegrees(clamped + 0.5)

	h.steeringThrottledLogger.Infof("PD: err=%.1f p=%.2f d=%.2f → angle=%d", pidErr, p, d, centerAngle)

	if centerAngle == h.steeringLastAngle {
		h.steeringThrottledLogger.Infof("no change in steering servo angle %d; skipping move", centerAngle)
		return
	}

	if err := h.steeringServoViam.Move(ctx, uint32(centerAngle), nil); err != nil {
		if errors.Is(err, context.Canceled) {
			h.steeringLogger.Info("stopping due to context cancellation")
			return
		}
		h.steeringLogger.Warnf("error moving steering servo to angle %d: %s", centerAngle, err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	h.steeringLastAngle = centerAngle
}

// convertXToSteeringServoAngle linearly maps a pixel-X to a servo angle.
// Used by the tracking test routine for display/metric purposes only — actual
// autonomous steering uses the PID controller in steeringTick.
func convertXToSteeringServoAngle(x visionPixels) servoDegrees {
	if x <= VISION_MIN_X {
		return STEERING_MAX_LEFT
	}
	if x >= VISION_MAX_X {
		return STEERING_MAX_RIGHT
	}
	frac := float64(x-VISION_MIN_X) / float64(VISION_MAX_X-VISION_MIN_X)
	angle := float64(STEERING_MAX_LEFT) + frac*float64(STEERING_MAX_RIGHT-STEERING_MAX_LEFT)
	return servoDegrees(angle + 0.5)
}

func (h *hawkeye) handleTestSteering(_ argsTestSteering) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.steeringLogger.Infof("moving steering servo to the left (angle: %d)", STEERING_MAX_LEFT)
	err := h.steeringServoViam.Move(ctx, uint32(STEERING_MAX_LEFT), nil)
	if err != nil {
		h.steeringLogger.Warnf("error moving steering servo to the left (angle: %d): %v", STEERING_MAX_LEFT, err)
	}

	time.Sleep(1 * time.Second)

	h.steeringLogger.Infof("moving steering servo to the right (angle: %d)", STEERING_MAX_RIGHT)
	err = h.steeringServoViam.Move(ctx, uint32(STEERING_MAX_RIGHT), nil)
	if err != nil {
		h.steeringLogger.Warnf("error moving steering servo to the right (angle: %d): %v", STEERING_MAX_RIGHT, err)
	}

	time.Sleep(1 * time.Second)

	h.steeringLogger.Infof("moving steering servo to neutral (angle: %d)", STEERING_NEUTRAL)
	err = h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil)
	if err != nil {
		h.steeringLogger.Warnf("error moving steering servo to neutral (angle: %d): %v", STEERING_NEUTRAL, err)
	}

	return map[string]any{"status": "ok"}, nil
}
