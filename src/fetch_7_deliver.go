package main

import (
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// fetchTickDeliver drives the ball at the person exactly the way the chase drove
// at the ball, closing in as the box of their shoes grows.
// VISION_DETECTION_PAIR_COMBINED_MAX_AREA means the car is at their feet.
//
// Finding the person is not this state's job — the turn and the evaluation trade
// the car back and forth doing that — so this drives from its first tick.
func (h *hawkeye) fetchTickDeliver(detection *visionDetection) {
	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_PERSON))

	isDetectionValid := (detection != nil && detection.area >= VISION_DETECTION_PAIR_COMBINED_MIN_AREA)

	// No shoes to aim at means no idea where the person is.
	if !isDetectionValid {
		// h.neutralizeSteeringAndMotorAngles()
		x := MOTOR_FORWARD_LOW + 1
		h.motorDesiredAngle.Store(&x)
		h.fetchThrottledLogger.Infof("%s: no person to carry the ball to; holding position", FETCH_STATE_7_DELIVER)
		return
	}

	if detection.area >= VISION_DETECTION_PAIR_COMBINED_MAX_AREA {
		h.fetchLogger.Infof(
			"%s: person %s reached the delivery area of %d; setting the ball down",
			FETCH_STATE_7_DELIVER,
			detection,
			VISION_DETECTION_PAIR_COMBINED_MAX_AREA,
		)

		h.deliverBall()
		h.storeFetchState(FETCH_STATE_8_DONE)

		h.fetchLogger.Infof("%s: ball delivered; switching to %q", FETCH_STATE_7_DELIVER, FETCH_STATE_8_DONE)
		return
	}

	var (
		steeringAngle = convertXToSteeringServoAngle(detection.centerX)
		motorAngle    = convertAreaToMotorServoAngle(detection.area,
			VISION_DETECTION_PAIR_COMBINED_MIN_AREA, VISION_DETECTION_PAIR_COMBINED_MAX_AREA)
	)

	h.steeringDesiredAngle.Store(&steeringAngle)
	h.motorDesiredAngle.Store(&motorAngle)

	h.fetchThrottledLogger.Infof(
		"%s: shoes [%s] require steering angle %d and motor angle %d",
		FETCH_STATE_7_DELIVER,
		detection,
		steeringAngle,
		motorAngle,
	)
}

// deliverBall brakes, opens the jaws, and backs away so the car comes out from
// around the ball rather than shunting it along. Runs start to finish inside the
// one tick: every step is timed and there is nothing left for vision to steer by.
func (h *hawkeye) deliverBall() {
	h.steeringDesiredAngle.Store(util.Ptr(STEERING_NEUTRAL))

	h.fetchLogger.Infof(
		"%s: braking in front of person for %s",
		FETCH_STATE_7_DELIVER,
		FETCH_DELIVER_BRAKE_DURATION,
	)

	h.motorDesiredAngle.Store(util.Ptr(MOTOR_REVERSE_HIGH))
	time.Sleep(FETCH_DELIVER_BRAKE_DURATION)

	h.releaseBall()
}

// releaseBall is deliverBall without the brake that gets there. Split out so a
// test can exercise it on a car that is already standing still.
func (h *hawkeye) releaseBall() {
	h.steeringDesiredAngle.Store(util.Ptr(STEERING_NEUTRAL))

	h.fetchLogger.Infof("%s: opening the gripper to release the ball", FETCH_STATE_7_DELIVER)
	h.gripperDesiredAngle.Store(util.Ptr(GRIPPER_ANGLE_OPEN))

	h.steeringDesiredAngle.Store(util.Ptr(STEERING_NEUTRAL + 3))
	h.motorDesiredAngle.Store(util.Ptr(MOTOR_NEUTRAL))
	time.Sleep(MOTOR_REVERSE_NEUTRAL_DURATION)

	h.fetchLogger.Infof("%s: backing away from the ball for %s",
		FETCH_STATE_7_DELIVER, FETCH_DELIVER_REVERSE_DURATION)
	h.motorDesiredAngle.Store(util.Ptr(MOTOR_REVERSE_HIGH))
	time.Sleep(FETCH_DELIVER_REVERSE_DURATION)

	// Jaws shut on the way out, where every other state expects to find them.
	h.fetchLogger.Infof("%s: closing the gripper", FETCH_STATE_7_DELIVER)
	h.gripperDesiredAngle.Store(util.Ptr(GRIPPER_ANGLE_CLOSED))

	// Stopped, not still reversing: done parks the car only on its next tick.
	h.motorDesiredAngle.Store(util.Ptr(MOTOR_NEUTRAL))
}
