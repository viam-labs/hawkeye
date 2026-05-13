package main

import (
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// enterFetchGrip opens the creep with no settling window: the ball has to come
// within the jaws on this state's own watch before one starts.
func (h *hawkeye) enterFetchGrip() {
	h.fetchGripInReachSince = time.Time{}
	h.storeFetchState(FETCH_STATE_4_GRIP)
}

// resetFetchGrip clears what this state owns back to "has not started".
func (h *hawkeye) resetFetchGrip() {
	h.fetchGripInReachSince = time.Time{}
}

// fetchTickGrip creeps the last stretch onto the ball at MOTOR_GRIP, still
// steering, until isWithinGripperReach holds for FETCH_GRIPPER_REACH_DURATION —
// then closes the jaws and hands off to the turn.
//
// Creeping rather than aiming the chase's coast at the ball: how far that coast
// carries depends on approach speed, surface and battery charge, so it cannot be
// landed accurately. The first ticks here bleed it off.
func (h *hawkeye) fetchTickGrip(detection *visionDetection) {
	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_CLAW))

	// Never creep blind, or the car drives off after a ball it cannot see.
	if detection == nil || detection.area < VISION_DETECTION_SINGLE_MIN_AREA {
		h.neutralizeSteeringAndMotorAngles()
		h.fetchGripInReachSince = time.Time{}

		h.fetchThrottledLogger.Infof("%s: no detection to creep toward; holding position", FETCH_STATE_4_GRIP)
		return
	}

	switch {
	// Out ahead of the jaws or off to one side: reset the settling window.
	case !detection.isWithinGripperReach():
		if !h.fetchGripInReachSince.IsZero() {
			h.fetchGripInReachSince = time.Time{}
			h.fetchLogger.Infof(
				"%s: detection [%s] slipped back out of the gripper's reach; continuing to creep up",
				FETCH_STATE_4_GRIP,
				detection,
			)
		}
		h.creepTowardDetection(detection)

	// First tick in reach: open the settling window.
	case h.fetchGripInReachSince.IsZero():
		h.fetchGripInReachSince = time.Now()
		h.fetchLogger.Infof(
			"%s: detection [%s] came within reach; creeping %s further to settle it into the jaws",
			FETCH_STATE_4_GRIP,
			detection,
			FETCH_GRIPPER_REACH_DURATION,
		)
		h.creepTowardDetection(detection)

	// In reach: the window has either run out or not yet.
	default:
		inReachFor := time.Since(h.fetchGripInReachSince)
		if inReachFor < FETCH_GRIPPER_REACH_DURATION {
			h.creepTowardDetection(detection)
			return
		}

		h.neutralizeSteeringAndMotorAngles()
		h.gripperDesiredAngle.Store(util.Ptr(GRIPPER_ANGLE_CLOSED))
		h.resetFetchGrip()

		// The turn starts from its first leg and looks for the person after each.
		h.useVisionPerson()
		h.startFetchKPointTurn()

		h.fetchLogger.Infof(
			"%s: detection stayed within reach for %s; closing the gripper and switching to %q",
			FETCH_STATE_4_GRIP,
			inReachFor.Round(time.Millisecond),
			FETCH_STATE_5_K_POINT_TURN,
		)
	}
}

// creepTowardDetection rolls the car forward at MOTOR_GRIP, steering by the
// detection since the ball can sit off to one side. Every tick of the grip that
// does not end with the jaws closing ends here.
func (h *hawkeye) creepTowardDetection(detection *visionDetection) {
	steeringAngle := convertXToSteeringServoAngle(detection.centerX)
	h.steeringDesiredAngle.Store(&steeringAngle)
	h.motorDesiredAngle.Store(util.Ptr(MOTOR_GRIP))

	h.fetchThrottledLogger.Infof(
		"%s: creeping at motor angle %d with steering angle %d using detection %s",
		FETCH_STATE_4_GRIP,
		MOTOR_GRIP,
		steeringAngle,
		detection,
	)
}
