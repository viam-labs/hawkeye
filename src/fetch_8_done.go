package main

import "github.com/viam-labs/hawkeye/util"

// fetchTickDone parks everything until the next fetch starts.
func (h *hawkeye) fetchTickDone() {
	h.neutralizeSteeringAndMotorAngles()
	h.gripperDesiredAngle.Store(util.Ptr(GRIPPER_ANGLE_NEUTRAL))
	h.screenDesiredImage.Store(util.Ptr(SCREEN_IMAGE_VIAM_LOGO))
	h.fetchThrottledLogger.Infof("%s: holding position", FETCH_STATE_8_DONE)
}
