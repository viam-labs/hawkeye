package main

import (
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// fetchStableWindow is the stability window open on a candidate detection. The
// anchor and its timestamp travel together so neither can be cleared alone; nil
// means no candidate is being timed.
type fetchStableWindow struct {
	anchor *visionDetection
	since  time.Time
}

// enterFetchEvaluateChase starts a fresh look: no candidate, clock from now.
func (h *hawkeye) enterFetchEvaluateChase() {
	h.fetchEvaluateChaseStable = nil
	h.fetchEvaluateChaseStartTime = time.Now()
	h.storeFetchState(FETCH_STATE_2_EVALUATE_CHASE)
}

// resetFetchEvaluateChase clears what this state owns back to "has not started".
func (h *hawkeye) resetFetchEvaluateChase() {
	h.fetchEvaluateChaseStable = nil
	h.fetchEvaluateChaseStartTime = time.Time{}
}

// fetchTickEvaluateChase sizes up whatever stopped the sweep, holding the car
// still. A detection earns a chase by being at least
// VISION_DETECTION_SINGLE_MIN_AREA and holding still for
// FETCH_STABLE_DETECTION_DURATION; anything less is noise not worth lurching at.
//
// The verdict is bounded by FETCH_EVALUATE_CHASE_DURATION. Standing here longer
// is how the car used to get stuck on a false positive that never settled and
// never quite went away, so a spent window hands back to the sweep.
func (h *hawkeye) fetchTickEvaluateChase(detection *visionDetection) {
	h.neutralizeSteeringAndMotorAngles()

	if evaluateChaseDuration := time.Since(h.fetchEvaluateChaseStartTime); evaluateChaseDuration >= FETCH_EVALUATE_CHASE_DURATION {
		h.resetFetchEvaluateChase()
		h.enterFetchSeek()

		h.fetchLogger.Infof(
			"%s: nothing found or held still in %s; back to %q with %s of its budget left",
			FETCH_STATE_2_EVALUATE_CHASE,
			evaluateChaseDuration.Round(time.Millisecond),
			FETCH_STATE_1_SEEK,
			max(FETCH_SEEK_DURATION-h.fetchSeekTotalDriveDuration, 0).Round(time.Millisecond),
		)
		return
	}

	if detection == nil || detection.area < VISION_DETECTION_SINGLE_MIN_AREA {
		h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_VIAM_LOGO))

		if h.fetchEvaluateChaseStable != nil {
			h.fetchLogger.Infof("%s: lost the candidate detection before it held still long enough", FETCH_STATE_2_EVALUATE_CHASE)
			h.fetchEvaluateChaseStable = nil
		}

		h.fetchThrottledLogger.Infof("%s: waiting for a large enough detection", FETCH_STATE_2_EVALUATE_CHASE)
		return
	}

	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_TENNIS_BALL_ROLLING))

	if !h.fetchDetectionIsStable(detection) {
		return
	}

	// Read before the reset drops the window it is measured against.
	heldStillFor := time.Since(h.fetchEvaluateChaseStable.since)

	h.resetFetchSeekCycle()
	h.storeFetchState(FETCH_STATE_3_CHASE)

	h.fetchLogger.Infof(
		"%s: detection [%s] held still for %s; switching to %q",
		FETCH_STATE_2_EVALUATE_CHASE,
		detection,
		heldStillFor.Round(time.Millisecond),
		FETCH_STATE_3_CHASE,
	)
}

// fetchDetectionIsStable advances the stability window and reports whether it has
// closed. Drift is measured against the anchor rather than the previous tick, so
// a ball creeping a little each tick cannot slip through. Callers pass a
// detection they have already found large enough to chase.
func (h *hawkeye) fetchDetectionIsStable(detection *visionDetection) bool {
	if h.fetchEvaluateChaseStable == nil || detection.hasDriftedFrom(h.fetchEvaluateChaseStable.anchor, FETCH_STABLE_DETECTION_MAX_DRIFT) {
		h.fetchEvaluateChaseStable = &fetchStableWindow{anchor: detection, since: time.Now()}

		h.fetchThrottledLogger.Infof(
			"%s: waiting %s for detection [%s] to hold still",
			FETCH_STATE_2_EVALUATE_CHASE,
			FETCH_STABLE_DETECTION_DURATION,
			detection,
		)
		return false
	}

	heldStillFor := time.Since(h.fetchEvaluateChaseStable.since)
	if heldStillFor < FETCH_STABLE_DETECTION_DURATION {
		h.fetchThrottledLogger.Infof(
			"%s: detection has held still for %s of %s",
			FETCH_STATE_2_EVALUATE_CHASE,
			heldStillFor.Round(time.Millisecond),
			FETCH_STABLE_DETECTION_DURATION,
		)
		return false
	}

	return true
}
