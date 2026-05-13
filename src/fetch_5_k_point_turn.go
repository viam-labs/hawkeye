package main

import (
	"context"
	"fmt"
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// fetchKPointTurnLeg is one timed leg: an angle for each servo and how long to
// hold it.
type fetchKPointTurnLeg struct {
	name          string
	motorAngle    servoDegrees
	steeringAngle servoDegrees
	duration      time.Duration
}

// startFetchKPointTurn begins the turn from its first leg, and is the only way in
// that rewinds the leg index — the index surviving the handoffs to the evaluation
// is what carries the maneuver forward. Resuming after a look is a plain
// storeFetchState, not a call to this.
func (h *hawkeye) startFetchKPointTurn() {
	h.resetFetchKPointTurn()
	h.storeFetchState(FETCH_STATE_5_K_POINT_TURN)
}

// resetFetchKPointTurn winds the maneuver back to its first leg.
func (h *hawkeye) resetFetchKPointTurn() {
	h.fetchKPointTurnLegIndex = 0
}

// fetchTickKPointTurn swings the car back around to face the way it came. K is
// FETCH_K_POINT_TURN_DRIVE_LEGS — a constant because how many points the turn
// takes is a property of the room, not of the car.
//
// Alone among the states, this drives the servos directly rather than publishing
// angles: the legs are timed, with nothing for vision to steer by, and the
// sequence has to run start to finish rather than be re-decided each tick. Each
// leg publishes its angles anyway, so a running steering or motor routine agrees
// with the maneuver instead of correcting it away.
func (h *hawkeye) fetchTickKPointTurn() {
	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_K))

	legs := fetchKPointTurnLegs()

	if h.fetchKPointTurnLegIndex >= len(legs) {
		h.storeFetchState(FETCH_STATE_8_DONE)

		h.fetchLogger.Infof(
			"%s: came all the way around without the person ever coming into frame; switching to %q",
			FETCH_STATE_5_K_POINT_TURN,
			FETCH_STATE_8_DONE,
		)
		return
	}

	// Hand over immediately before each reverse leg: that leg cannot begin until
	// the ESC is armed, which costs the better part of a second standing still, so
	// it is the only boundary where the evaluation's look is free.
	//
	// This leaves the car partway through a swing. Both legs of a unit rotate the
	// same way, so partway is just another heading — and looking from more
	// headings finds the person from more of the room.
	for h.fetchKPointTurnLegIndex < len(legs) {
		h.driveKPointTurnLeg(legs[h.fetchKPointTurnLegIndex])
		h.fetchKPointTurnLegIndex++

		if fetchKPointTurnNextLegIsReverse(h.fetchKPointTurnLegIndex) {
			break
		}
	}

	h.neutralizeSteeringAndMotorAngles()
	h.enterFetchEvaluateDeliver()

	h.fetchLogger.Infof(
		"%s: drove leg %d of %d; switching to %q to look for the person for %s",
		FETCH_STATE_5_K_POINT_TURN,
		h.fetchKPointTurnLegIndex,
		len(legs),
		FETCH_STATE_6_EVALUATE_DELIVER,
		FETCH_EVALUATE_DELIVER_DURATION,
	)
}

// fetchKPointTurnNextLegIsReverse reports whether the leg at legIndex reverses,
// which is where the turn hands over and what decides whether the evaluation has
// any arming to do. An index past the end means the turn is finished.
func fetchKPointTurnNextLegIsReverse(legIndex int) bool {
	legs := fetchKPointTurnLegs()
	return legIndex < len(legs) && legs[legIndex].motorAngle == FETCH_K_POINT_TURN_REVERSE_ANGLE
}

// fetchKPointTurnLegs lays the maneuver out: FETCH_K_POINT_TURN_DRIVE_LEGS legs
// alternating forward on full left lock with reverse on full right lock, each
// pair separated by a coast to shed the previous leg's momentum.
//
// Alternating is the trick — every swap of lock and direction adds rotation while
// giving back most of the ground the last leg covered, so the car comes around in
// a corridor a single forward-and-back sweep would not fit. An odd leg count
// opens and closes on a forward leg, leaving the car pointing the way it came.
func fetchKPointTurnLegs() []fetchKPointTurnLeg {
	legs := make([]fetchKPointTurnLeg, 0, 2*FETCH_K_POINT_TURN_DRIVE_LEGS)

	for i := range FETCH_K_POINT_TURN_DRIVE_LEGS {
		if i > 0 {
			// Only the throttle comes off — the wheels hold the last leg's lock.
			// Straightening here would spend the coast rolling forward in a line,
			// giving back the tightness the lock-to-lock swap buys.
			legs = append(legs, fetchKPointTurnLeg{
				name:          fmt.Sprintf("coasting into leg %d", i+1),
				motorAngle:    MOTOR_NEUTRAL,
				steeringAngle: legs[len(legs)-1].steeringAngle,
				duration:      FETCH_K_POINT_TURN_COAST_DURATION,
			})
		}

		if i%2 == 0 {
			legs = append(legs, fetchKPointTurnLeg{
				name:          fmt.Sprintf("swinging out on leg %d of %d", i+1, FETCH_K_POINT_TURN_DRIVE_LEGS),
				motorAngle:    FETCH_K_POINT_TURN_FORWARD_ANGLE,
				steeringAngle: STEERING_MAX_LEFT,
				duration:      FETCH_K_POINT_TURN_FORWARD_DURATION,
			})
		} else {
			legs = append(legs, fetchKPointTurnLeg{
				name:          fmt.Sprintf("backing around on leg %d of %d", i+1, FETCH_K_POINT_TURN_DRIVE_LEGS),
				motorAngle:    FETCH_K_POINT_TURN_REVERSE_ANGLE,
				steeringAngle: STEERING_MAX_RIGHT,
				duration:      FETCH_K_POINT_TURN_REVERSE_DURATION,
			})
		}
	}

	return legs
}

// driveKPointTurnLeg drives one leg to completion. There is no cancellation path:
// abandoning a leg partway strands the car mid-maneuver with the ball in its
// jaws, and legs are short.
//
// A reverse leg expects the ESC to be armed already — after a forward drive it
// wants a full brake tap, not just a coast to neutral. The evaluation between
// legs does that; callers driving legs back to back must arm for themselves.
func (h *hawkeye) driveKPointTurnLeg(leg fetchKPointTurnLeg) {
	// Nothing cancels this one: the leg owns the car until it is done.
	ctx := context.Background()

	h.fetchLogger.Infof(
		"%s: %s for %s (motor angle %d, steering angle %d)",
		FETCH_STATE_5_K_POINT_TURN,
		leg.name,
		leg.duration,
		leg.motorAngle,
		leg.steeringAngle,
	)

	h.steeringDesiredAngle.Store(util.Ptr(leg.steeringAngle))
	h.motorDesiredAngle.Store(util.Ptr(leg.motorAngle))

	// A servo that will not move costs this leg its distance, but stopping
	// mid-turn strands the car — so log, carry on, and let the timings play out.
	if err := h.steeringServoViam.Move(ctx, uint32(leg.steeringAngle), nil); err != nil {
		h.fetchLogger.Warnf(
			"%s: error moving steering servo to angle %d while %s: %v",
			FETCH_STATE_5_K_POINT_TURN,
			leg.steeringAngle,
			leg.name,
			err,
		)
	}
	if err := h.motorServoViam.Move(ctx, uint32(leg.motorAngle), nil); err != nil {
		h.fetchLogger.Warnf(
			"%s: error moving motor servo to angle %d while %s: %v",
			FETCH_STATE_5_K_POINT_TURN,
			leg.motorAngle,
			leg.name,
			err,
		)
	}

	time.Sleep(leg.duration)
}
