package main

import (
	"context"
	"math"
	"time"

	"github.com/pkg/errors"
	"github.com/viam-labs/hawkeye/util"
)

func (h *hawkeye) handleStartFetch(_ argsStartFetch) (map[string]any, error) {
	h.resetFetchRun()

	h.storeFetchState(FETCH_STATE_0_IDLE)
	h.useVisionBall()

	h.fetchLogger.Infof("starting in %s", FETCH_START_DELAY)
	time.Sleep(FETCH_START_DELAY)

	h.enterFetchSeek()

	err := h.fetchRoutine.Start(h.fetchLogger, h.fetchTick, FETCH_TICK_RATE)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status": "started",
		"state":  string(FETCH_STATE_1_SEEK),
	}, nil
}

func (h *hawkeye) handleStopFetch(_ argsStopFetch) (map[string]any, error) {
	err := h.fetchRoutine.Stop()
	if err != nil {
		return nil, err
	}

	// An arming outlives the tick that started it and drives the motor servo
	// directly, so it must finish before anything below parks the car.
	h.fetchEvaluateDeliverArming.Wait()

	// Those routines may still be running, so park them rather than leaving them
	// at whatever the last tick asked for.
	h.neutralizeSteeringAndMotorAngles()

	// Hand the screen back to whatever runs next.
	h.screenDesiredImage.Store(nil)

	h.fetchState.Store(nil)
	h.resetFetchRun()

	// A stop mid-delivery would otherwise leave vision hunting for shoes.
	h.useVisionBall()

	return map[string]any{"status": "stopped"}, nil
}

// fetchTick is the coordinator: the only routine that turns visionLastDetection
// into servo angles, published on steeringDesiredAngle and motorDesiredAngle for
// their routines to apply. It touches no servo itself, so it stays independently
// startable and testable.
func (h *hawkeye) fetchTick(_ context.Context) {
	var (
		currentState = h.loadFetchState()
		detection    = h.visionLastDetection.Load()
	)

	switch currentState {
	case FETCH_STATE_1_SEEK:
		h.fetchTickSeek(detection)
	case FETCH_STATE_2_EVALUATE_CHASE:
		h.fetchTickEvaluateChase(detection)
	case FETCH_STATE_3_CHASE:
		h.fetchTickChase(detection)
	case FETCH_STATE_4_GRIP:
		h.fetchTickGrip(detection)
	case FETCH_STATE_5_K_POINT_TURN:
		h.fetchTickKPointTurn()
	case FETCH_STATE_6_EVALUATE_DELIVER:
		h.fetchTickEvaluateDeliver(detection)
	case FETCH_STATE_7_DELIVER:
		h.fetchTickDeliver(detection)
	case FETCH_STATE_8_DONE:
		h.fetchTickDone()
	default:
		h.neutralizeSteeringAndMotorAngles()
		h.fetchThrottledLogger.Warnf("nothing to do in state %q yet; holding position", currentState)
	}
}

// neutralizeMotorAngle turns the throttle off.
func (h *hawkeye) neutralizeMotorAngle() {
	h.motorDesiredAngle.Store(util.Ptr(MOTOR_NEUTRAL))
}

// neutralizeSteeringAndMotorAngles parks the car: wheels centered, throttle off.
func (h *hawkeye) neutralizeSteeringAndMotorAngles() {
	h.steeringDesiredAngle.Store(util.Ptr(STEERING_NEUTRAL))
	h.motorDesiredAngle.Store(util.Ptr(MOTOR_NEUTRAL))
}

// loadFetchState treats an unset state as idle, so a routine that has never run
// reads the same as one that has finished.
func (h *hawkeye) loadFetchState() fetchState {
	currentState := h.fetchState.Load()
	if currentState == nil {
		return FETCH_STATE_0_IDLE
	}
	return *currentState
}

// resetFetchRun puts every state's bookkeeping back to "has not started", so a
// run begins from a known slate and leaves nothing for the next to inherit. A
// run boundary ends every cycle, so the per-cycle resets belong here too.
func (h *hawkeye) resetFetchRun() {
	h.resetFetchSeekCycle()
	h.resetFetchGrip()
	h.resetFetchKPointTurn()
	h.resetFetchEvaluateDeliver()
}

func (h *hawkeye) storeFetchState(newState fetchState) {
	previousState := h.loadFetchState()
	h.fetchState.Store(&newState)

	if previousState != newState {
		h.fetchLogger.Infof("state: %s -> %s", previousState, newState)
	}
}

// convertXToSteeringServoAngle maps a pixel-X to a servo angle, pivoting on
// STEERING_NEUTRAL at dead center and clamping outside the vision range.
//
// The curve applies to how far off center the detection is, not to its position
// across the frame, so the two halves stay mirror images of each other.
func convertXToSteeringServoAngle(x visionPixels) servoDegrees {
	if x <= VISION_MIN_X {
		return STEERING_MAX_LEFT
	}
	if x >= VISION_MAX_X {
		return STEERING_MAX_RIGHT
	}

	var (
		centerX   = float64(VISION_MIN_X+VISION_MAX_X) / 2
		halfWidth = float64(VISION_MAX_X-VISION_MIN_X) / 2

		// -1 at the left edge of the frame, 0 dead center, +1 at the right edge.
		offset = (float64(x) - centerX) / halfWidth
	)

	extremeAngle := STEERING_MAX_LEFT
	if offset > 0 {
		extremeAngle = STEERING_MAX_RIGHT
	}

	deflection := math.Pow(math.Abs(offset), STEERING_SENSITIVITY_SOFTNESS)
	angle := float64(STEERING_NEUTRAL) + deflection*float64(extremeAngle-STEERING_NEUTRAL)

	return servoDegrees(math.Round(angle))
}

// convertAreaToMotorServoAngle maps a detection area in [minArea, maxArea) to a
// forward-drive angle in [MOTOR_FORWARD_HIGH, MOTOR_FORWARD_LOW].
//
// Out-of-band areas stop the car: below the min is noise not worth accelerating
// at, and at the max the approach is over. Callers pass their own state's band —
// the chase closes on the ball, the delivery on shoes — and hand off on the same
// max, so the mapping and the state agree on what "close enough" means.
func convertAreaToMotorServoAngle(area, minArea, maxArea visionPixels) servoDegrees {
	if area <= minArea || area >= maxArea {
		return MOTOR_NEUTRAL
	}

	frac := float64(area-minArea) / float64(maxArea-minArea)
	angle := float64(MOTOR_FORWARD_HIGH) + math.Pow(frac, MOTOR_DECELERATION_FACTOR)*float64(MOTOR_FORWARD_LOW-MOTOR_FORWARD_HIGH)
	return servoDegrees(angle + 0.5)
}

func (h *hawkeye) handleTestFetch(args argsTestFetch) (map[string]any, error) {
	return nil, errors.New("not implemented; use the vision and motor tests instead")
}
