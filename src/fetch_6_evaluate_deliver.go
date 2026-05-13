package main

import (
	"context"
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// enterFetchEvaluateDeliver opens a look with its clock unset. The clock starts
// on the first tick that finds nobody in frame, so a person already standing
// there ends the turn without arming for a leg the car will never drive.
func (h *hawkeye) enterFetchEvaluateDeliver() {
	h.resetFetchEvaluateDeliver()
	h.storeFetchState(FETCH_STATE_6_EVALUATE_DELIVER)
}

// resetFetchEvaluateDeliver clears what this state owns back to "has not started".
func (h *hawkeye) resetFetchEvaluateDeliver() {
	h.fetchEvaluateDeliverStartTime = time.Time{}
}

// fetchTickEvaluateDeliver holds the car still between legs of the turn, looking
// for the person while the ESC arms for the next leg.
//
// The look is free because it runs over that arming: armMotorForReverse needs the
// better part of a second with the car standing still, which is what a look wants
// anyway. Anyone big enough in frame commits to the delivery; nobody by
// FETCH_EVALUATE_DELIVER_DURATION hands back for another leg. Either way the
// arming must finish first, since it drives the motor servo directly.
func (h *hawkeye) fetchTickEvaluateDeliver(detection *visionDetection) {
	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_EYE))

	// Before the arming starts, so a person already in frame ends the turn
	// without arming for a leg the car will never drive.
	if detection != nil && detection.area >= VISION_DETECTION_PAIR_COMBINED_MIN_AREA {
		h.fetchLogger.Infof(
			"%s: the person came into frame %s; carrying the ball over",
			FETCH_STATE_6_EVALUATE_DELIVER,
			detection,
		)

		h.endFetchEvaluateDeliver(FETCH_STATE_7_DELIVER)
		return
	}

	// The clock and the arming start together — the point of this state.
	if h.fetchEvaluateDeliverStartTime.IsZero() {
		h.fetchEvaluateDeliverStartTime = time.Now()
		h.startFetchEvaluateDeliverArming()
	}

	lookDuration := time.Since(h.fetchEvaluateDeliverStartTime)
	if lookDuration < FETCH_EVALUATE_DELIVER_DURATION {
		h.fetchThrottledLogger.Infof(
			"%s: looking for the person, %s of %s",
			FETCH_STATE_6_EVALUATE_DELIVER,
			lookDuration.Round(time.Millisecond),
			FETCH_EVALUATE_DELIVER_DURATION,
		)
		return
	}

	h.fetchLogger.Infof(
		"%s: nobody in frame after %s; back to %q for another leg",
		FETCH_STATE_6_EVALUATE_DELIVER,
		lookDuration.Round(time.Millisecond),
		FETCH_STATE_5_K_POINT_TURN,
	)

	h.endFetchEvaluateDeliver(FETCH_STATE_5_K_POINT_TURN)
}

// startFetchEvaluateDeliverArming walks the ESC through the brake tap the next
// reverse leg needs, on its own goroutine so the look happens over it.
//
// Nothing is armed when the next leg is not a reverse one: that is a finished
// turn's last evaluation, giving the person one more chance to appear.
func (h *hawkeye) startFetchEvaluateDeliverArming() {
	if !fetchKPointTurnNextLegIsReverse(h.fetchKPointTurnLegIndex) {
		return
	}

	h.fetchEvaluateDeliverArming.Add(1)
	go func() {
		defer h.fetchEvaluateDeliverArming.Done()

		h.fetchLogger.Infof(
			"%s: arming the ESC for the turn's next reverse leg",
			FETCH_STATE_6_EVALUATE_DELIVER,
		)

		// The arming owns the motor until done, the same way a leg does.
		if err := h.armMotorForReverse(context.Background()); err != nil {
			h.fetchLogger.Warnf("%s: error arming for reverse: %v", FETCH_STATE_6_EVALUATE_DELIVER, err)
		}
	}()
}

// endFetchEvaluateDeliver hands the car on once this state's arming has finished
// with the motor servo, so whatever drives next does not race a brake tap that is
// still mid-pattern. Returns immediately when there was no arming to do.
func (h *hawkeye) endFetchEvaluateDeliver(nextState fetchState) {
	h.fetchEvaluateDeliverArming.Wait()

	h.resetFetchEvaluateDeliver()
	h.storeFetchState(nextState)
}
