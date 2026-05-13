package main

import (
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// enterFetchSeek opens a driving stint, leaving the budget alone: it carries
// across every re-entry from a failed evaluation, which is what makes the cycle
// terminate. Ending the cycle is resetFetchSeekCycle's job.
func (h *hawkeye) enterFetchSeek() {
	h.fetchSeekStintStartTime = time.Now()
	h.fetchSeekCoastStartTime = time.Time{}
	h.storeFetchState(FETCH_STATE_1_SEEK)
}

// resetFetchSeek clears what the seek itself owns, budget included.
func (h *hawkeye) resetFetchSeek() {
	h.fetchSeekTotalDriveDuration = 0
	h.fetchSeekStintStartTime = time.Time{}
	h.fetchSeekCoastStartTime = time.Time{}
}

// resetFetchSeekCycle ends the seek/evaluate_chase cycle and drops its budget.
// Only for leaving the cycle for good — a ball worth chasing, or a budget spent
// — never for the handoffs within it.
func (h *hawkeye) resetFetchSeekCycle() {
	h.resetFetchSeek()
	h.resetFetchEvaluateChase()
}

// fetchTickSeek sweeps the car forward looking for a ball, wheels swapping lock
// to lock so it covers an arc, for up to FETCH_SEEK_DURATION of driving.
//
// A big enough detection only stops the car; judging it is the evaluation's job,
// since at seek speed a distant ball and a scrap of noise look alike. A failed
// evaluation hands the car back with the rest of the budget, so a false positive
// costs seconds rather than the fetch. Running the budget out ends it.
func (h *hawkeye) fetchTickSeek(detection *visionDetection) {
	if !h.fetchSeekCoastStartTime.IsZero() {
		h.fetchTickSeekStopped(detection)
		return
	}

	var (
		stintDuration      = time.Since(h.fetchSeekStintStartTime)
		totalDriveDuration = h.fetchSeekTotalDriveDuration + stintDuration
	)

	if totalDriveDuration >= FETCH_SEEK_DURATION {
		h.neutralizeSteeringAndMotorAngles()
		h.resetFetchSeekCycle()
		h.storeFetchState(FETCH_STATE_8_DONE)

		h.fetchLogger.Infof(
			"%s: spent the whole %s budget without finding a ball worth chasing; switching to %q",
			FETCH_STATE_1_SEEK,
			FETCH_SEEK_DURATION,
			FETCH_STATE_8_DONE,
		)
		return
	}

	// Every stint drives FETCH_SEEK_MIN_DRIVE_DURATION before it will stop for
	// anything: what the evaluation just rejected is usually still in frame, and
	// stopping for it again immediately would spend no budget at all.
	isDetectionValid := (detection != nil && detection.area >= VISION_DETECTION_SINGLE_MIN_AREA*2)
	if stintDuration >= FETCH_SEEK_MIN_DRIVE_DURATION && isDetectionValid {
		// Bank the stint — the sweep may well be back for the rest of the budget.
		h.fetchSeekTotalDriveDuration = totalDriveDuration
		h.fetchSeekStintStartTime = time.Time{}
		h.fetchSeekCoastStartTime = time.Now()
		h.fetchTickSeekStopped(detection)

		h.fetchLogger.Infof(
			"%s: something in frame %s after %s of this stint and %s of the %s budget; stopping to evaluate",
			FETCH_STATE_1_SEEK,
			detection,
			stintDuration.Round(time.Millisecond),
			totalDriveDuration.Round(time.Millisecond),
			FETCH_SEEK_DURATION,
		)
		return
	}

	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_EYE))

	// Full left on even wags, full right on odd, so the sweep covers an arc
	// rather than a line.
	steeringAngle := STEERING_MAX_LEFT
	if int(totalDriveDuration/FETCH_SEEK_WAG_INTERVAL)%2 == 1 {
		steeringAngle = STEERING_MAX_RIGHT
	}

	h.steeringDesiredAngle.Store(&steeringAngle)
	h.motorDesiredAngle.Store(util.Ptr(FETCH_SEEK_MOTOR_ANGLE))

	h.fetchThrottledLogger.Infof(
		"%s: sweeping at motor angle %d with steering angle %d; %s of the %s budget spent",
		FETCH_STATE_1_SEEK,
		FETCH_SEEK_MOTOR_ANGLE,
		steeringAngle,
		totalDriveDuration.Round(time.Millisecond),
		FETCH_SEEK_DURATION,
	)
}

// fetchTickSeekStopped coasts the car to a halt on what the sweep spotted, then
// hands it to the evaluation. Only the throttle comes off; nothing here is in
// enough of a hurry to drive the motor against its own momentum.
//
// The wheels keep steering onto the detection the whole way down, so the chase
// starts lined up. The coast has to finish first because the evaluation's
// stability test means nothing until the car has settled — a detection slides
// across the frame on its own under a car that is still rolling.
//
// Coasting is free of the budget, which measures driving only.
func (h *hawkeye) fetchTickSeekStopped(detection *visionDetection) {
	h.neutralizeMotorAngle()

	coastedFor := time.Since(h.fetchSeekCoastStartTime)
	if coastedFor >= FETCH_SEEK_COAST_DURATION {
		h.fetchSeekCoastStartTime = time.Time{}
		h.enterFetchEvaluateChase()

		h.fetchLogger.Infof(
			"%s: pulled up on what stopped the sweep; switching to %q for up to %s, with %s of the seek budget left",
			FETCH_STATE_1_SEEK,
			FETCH_STATE_2_EVALUATE_CHASE,
			FETCH_EVALUATE_CHASE_DURATION,
			max(FETCH_SEEK_DURATION-h.fetchSeekTotalDriveDuration, 0).Round(time.Millisecond),
		)
		return
	}

	if detection == nil {
		h.fetchThrottledLogger.Infof(
			"%s: coasting to a halt, %s of %s; nothing in frame to steer onto",
			FETCH_STATE_1_SEEK,
			coastedFor.Round(time.Millisecond),
			FETCH_SEEK_COAST_DURATION,
		)
		return
	}

	steeringAngle := convertXToSteeringServoAngle(detection.centerX)
	h.steeringDesiredAngle.Store(&steeringAngle)

	h.fetchThrottledLogger.Infof(
		"%s: coasting to a halt, %s of %s; steering onto the detection [%s] at angle %d",
		FETCH_STATE_1_SEEK,
		coastedFor.Round(time.Millisecond),
		FETCH_SEEK_COAST_DURATION,
		detection,
		steeringAngle,
	)
}
