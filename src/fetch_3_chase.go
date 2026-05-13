package main

import (
	"time"

	"github.com/viam-labs/hawkeye/util"
)

// fetchTickChase drives at the detection until it fills
// VISION_DETECTION_SINGLE_MAX_AREA of the frame, then brakes and hands off to
// the grip. Only the motor is touched at that handoff — leaving the wheels where
// they are keeps the car pointed at the ball — and the jaws open with the brake
// so they are ready when the creep arrives.
//
// A detection that goes missing parks the car and waits indefinitely: it has
// already driven to where the ball was, so there is no better place to stand.
// The ball needs no stability check here; this chase is already locked on.
func (h *hawkeye) fetchTickChase(detection *visionDetection) {
	if detection == nil || detection.area < VISION_DETECTION_SINGLE_MIN_AREA {
		h.neutralizeSteeringAndMotorAngles()
		h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_LOCK_LOST))

		h.fetchThrottledLogger.Infof("%s: waiting for the detection to come back", FETCH_STATE_3_CHASE)
		return
	}

	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_LOCK_FOUND))

	if detection.area >= VISION_DETECTION_SINGLE_MAX_AREA {
		h.fetchLogger.Infof(
			"%s: detection [%s] reached the closing area of %d; braking for %s and opening the gripper before %q",
			FETCH_STATE_3_CHASE,
			detection,
			VISION_DETECTION_SINGLE_MAX_AREA,
			FETCH_CHASE_BRAKE_DURATION,
			FETCH_STATE_4_GRIP,
		)

		h.gripperDesiredAngle.Store(util.Ptr(GRIPPER_ANGLE_OPEN))
		h.motorDesiredAngle.Store(util.Ptr(MOTOR_REVERSE_HIGH))
		time.Sleep(FETCH_CHASE_BRAKE_DURATION)
		h.enterFetchGrip()

		return
	}

	var (
		steeringAngle = convertXToSteeringServoAngle(detection.centerX)
		motorAngle    = convertAreaToMotorServoAngle(detection.area, VISION_DETECTION_SINGLE_MIN_AREA, VISION_DETECTION_SINGLE_MAX_AREA)
	)

	h.steeringDesiredAngle.Store(&steeringAngle)
	h.motorDesiredAngle.Store(&motorAngle)

	h.fetchThrottledLogger.Infof(
		"%s: detection [%s] requires steering angle %d and motor angle %d",
		FETCH_STATE_3_CHASE,
		detection,
		steeringAngle,
		motorAngle,
	)
}
