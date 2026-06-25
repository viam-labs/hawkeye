package main

import (
	"context"
	"math"
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
	return map[string]any{"status": "stopped"}, nil
}

// motorTick reads visionLastDetection and drives the motor servo to an angle derived
// from the detection's area, resetting to neutral when no detection is present.
// When transitioning from forward to neutral, a brief brake pulse is applied first
// to bleed forward momentum before coasting to a stop.
func (h *hawkeye) motorTick(ctx context.Context) {
	lastDetection := h.visionLastDetection.Load()
	if lastDetection == nil {
		if h.motorLastAngle == MOTOR_NEUTRAL {
			h.motorThrottledLogger.Info("found no vision detection to move steering servo; remaining at neutral")
			return
		}

		h.motorThrottledLogger.Infof("found no vision detection to move steering servo; resetting to neutral angle %d", MOTOR_NEUTRAL)
		err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil)
		if err != nil {
			h.motorThrottledLogger.Warnf("error resetting motor servo to neutral angle %d: %v", MOTOR_NEUTRAL, err)
		}

		return
	}

	prevDirection := h.motorLastDriveDirection
	motorAngle := h.convertAreaToMotorServoAngleAndSetLastDriveDirection(lastDetection.area)

	if motorAngle == h.motorLastAngle {
		h.motorThrottledLogger.Infof("no change in motor servo angle %d; skipping move", motorAngle)
		return
	}

	if brakePulseNeeded(motorAngle, prevDirection) {
		h.motorThrottledLogger.Info("applying brake pulse before neutral")
		if err := h.motorServoViam.Move(ctx, uint32(MOTOR_REVERSE_HIGH), nil); err != nil {
			if errors.Is(err, context.Canceled) {
				h.motorLogger.Info("stopping due to context cancellation")
				return
			}
			h.motorThrottledLogger.Warnf("error applying brake pulse: %v", err)
		} else {
			time.Sleep(MOTOR_BRAKE_PULSE_DURATION)
		}
	}

	err := h.motorServoViam.Move(ctx, uint32(motorAngle), nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.motorLogger.Info("stopping due to context cancellation")
			return
		}

		h.motorThrottledLogger.Warnf("error powering motor servo with angle %d: %v", motorAngle, err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	h.motorLastAngle = motorAngle
	h.motorThrottledLogger.Infof("powered motor servo with angle %d", motorAngle)
}

// convertAreaToMotorServoAngleAndSetLastDriveDirection maps a detection area in
// [VISION_MIN_DETECTION_AREA, VISION_MAX_DETECTION_AREA] to a forward-drive
// servo angle in [MOTOR_FORWARD_HIGH, MOTOR_FORWARD_LOW].
//
// Out-of-band areas return MOTOR_NEUTRAL: below the min is treated as visual
// noise (don't accelerate toward it), above the max as "object too close".
func (h *hawkeye) convertAreaToMotorServoAngleAndSetLastDriveDirection(area visionPixels) servoDegrees {
	// Don't do anything if the detection is too far away. Prevents motor jitters due to visual noise.
	if area < VISION_MIN_DETECTION_AREA {
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_NEUTRAL
		return MOTOR_NEUTRAL
	}

	if area > VISION_MAX_DETECTION_AREA {
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_NEUTRAL
		return MOTOR_NEUTRAL
	}

	h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_FORWARD

	frac := float64(area-VISION_MIN_DETECTION_AREA) / float64(VISION_MAX_DETECTION_AREA-VISION_MIN_DETECTION_AREA)
	angle := float64(MOTOR_FORWARD_HIGH) + math.Pow(frac, MOTOR_DECELERATION_FACTOR)*float64(MOTOR_FORWARD_LOW-MOTOR_FORWARD_HIGH)
	return servoDegrees(angle + 0.5)
}

// handleTestMotor drives the motor in args.Direction. When reversing after a
// forward drive, the WP-1625's Fwd/Br/Rev mode requires a brake-tap arming
// pattern first (full-strength brake pulse, then neutral, then the reverse).
// From any other prior state the motor is already stopped, so a single drive
// pulse engages the requested direction directly.
func (h *hawkeye) handleTestMotor(args argsTestMotor) (map[string]any, error) {
	if !h.motorMutex.TryLock() {
		return nil, errors.New("motor command already running")
	}
	defer h.motorMutex.Unlock()
	defer h.motorNeutral()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	angle := powerToAngle(args.Power, MOTOR_REVERSE_LOW, MOTOR_REVERSE_HIGH)

	h.motorLogger.Infof("doing %s motor drive with angle %d (prior=%s)", args.Direction, angle, h.motorLastDriveDirection)

	if args.Direction == string(MOTOR_DRIVE_DIRECTION_REVERSE) && h.motorLastDriveDirection == MOTOR_DRIVE_DIRECTION_FORWARD {
		if err := h.motorServoViam.Move(ctx, uint32(MOTOR_REVERSE_HIGH), nil); err != nil {
			return nil, errors.Wrap(err, "error brake tapping motor using MOTOR_REVERSE_HIGH")
		}
		time.Sleep(MOTOR_REVERSE_BRAKE_DURATION)
		if err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil); err != nil {
			return nil, errors.Wrap(err, "error settling motor in neutral using MOTOR_NEUTRAL")
		}
		time.Sleep(MOTOR_REVERSE_NEUTRAL_DURATION)
	}

	if err := h.motorServoViam.Move(ctx, uint32(angle), nil); err != nil {
		return nil, errors.Wrapf(err, "error doing %s motor drive with angle %d", args.Direction, angle)
	}
	time.Sleep(time.Duration(args.DurationSecs) * time.Second)

	if args.Direction == string(MOTOR_DRIVE_DIRECTION_FORWARD) {
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_FORWARD
	} else {
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_REVERSE
	}

	return map[string]any{"status": "ok"}, nil
}

// powerToAngle linearly maps power in [1, 10] to an angle in [low, high].
// Works whether high > low (forward) or high < low (reverse).
func powerToAngle(power float64, low, high servoDegrees) servoDegrees {
	return low + servoDegrees(math.Round((power-1)/9.0*float64(high-low)))
}

// motorNeutral sends the motor servo back to SERVO_NEUTRAL using a fresh
// context, so cleanup still runs after the request context is cancelled.
func (h *hawkeye) motorNeutral() {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil); err != nil {
		h.motorLogger.Warnf("failed to return motor to neutral: %v", err)
	}
}

// brakePulseNeeded reports whether a brake pulse should fire before moving to
// neutral — only when leaving forward drive. The ESC interprets MOTOR_REVERSE_HIGH
// as a brake signal (not reverse) when transitioning from forward, so a brief pulse
// bleeds momentum without engaging the reverse arming sequence.
func brakePulseNeeded(newAngle servoDegrees, prevDirection driveDirection) bool {
	return newAngle == MOTOR_NEUTRAL && prevDirection == MOTOR_DRIVE_DIRECTION_FORWARD
}
