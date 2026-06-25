package main

import "go.uber.org/multierr"

func (h *hawkeye) handleStartFetch(_ argsStartFetch) (map[string]any, error) {
	if err := h.visionRoutine.Start(h.visionLogger, h.visionTick, VISION_TICK_RATE); err != nil {
		return nil, err
	}
	if err := h.steeringRoutine.Start(h.steeringLogger, h.steeringTick, STEERING_TICK_RATE); err != nil {
		_ = h.visionRoutine.Stop()
		return nil, err
	}
	if err := h.motorRoutine.Start(h.motorLogger, h.motorTick, MOTOR_TICK_RATE); err != nil {
		_ = h.steeringRoutine.Stop()
		_ = h.visionRoutine.Stop()
		return nil, err
	}
	return map[string]any{"status": "started"}, nil
}

func (h *hawkeye) handleStopFetch(_ argsStopFetch) (map[string]any, error) {
	motorErr := h.motorRoutine.Stop()
	steeringErr := h.steeringRoutine.Stop()
	visionErr := h.visionRoutine.Stop()
	if err := multierr.Combine(motorErr, steeringErr, visionErr); err != nil {
		return nil, err
	}
	return map[string]any{"status": "stopped"}, nil
}
