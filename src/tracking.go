package main

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/services/vision"
)

// handleTestTracking runs a live vision+steering loop for args.DurationSecs seconds
// and returns per-frame metrics. Use mode="ml" vs mode="color" back-to-back to
// compare detector tracking performance without running the full autonomous routines.
//
// The servo moves for real — point the camera at a tennis ball before running.
// Steering resets to neutral when the test finishes.
func (h *hawkeye) handleTestTracking(args argsTestTracking) (map[string]any, error) {
	logger := h.mainLogger.Sublogger(TRACKING_ROUTINE_NAME)

	svc, useMLFilter := h.trackingSelectService(args.Mode)
	if svc == nil {
		return nil, errors.New("color detector not configured (vision_color missing from robot config)")
	}

	duration := time.Duration(args.DurationSecs * float64(time.Second))
	logger.Infof("starting tracking test: mode=%s duration=%s camera=%q", args.Mode, duration, h.cameraName)

	var (
		frames       int
		errorCount   int
		detectCount  int
		totalDetectionLatency time.Duration
		minDetectionLatency   = time.Duration(1<<63 - 1)
		maxDetectionLatency   time.Duration
		totalTotalLatency     time.Duration
		totalArea             visionPixels
		totalSteeringAngle    int
	)

	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		frameStart := time.Now()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		detections, err := svc.DetectionsFromCamera(ctx, h.cameraName, nil)
		detectionLatency := time.Since(frameStart)
		cancel()

		frames++

		if err != nil {
			logger.Warnf("frame %d: detection error: %v", frames, err)
			errorCount++
			continue
		}

		totalDetectionLatency += detectionLatency
		if detectionLatency < minDetectionLatency {
			minDetectionLatency = detectionLatency
		}
		if detectionLatency > maxDetectionLatency {
			maxDetectionLatency = detectionLatency
		}

		var best *visionDetection
		if useMLFilter {
			best = computeLargestDetection(detections)
		} else {
			best = computeLargestDetectionAny(detections)
		}

		var steeringAngle servoDegrees = STEERING_NEUTRAL
		if best != nil {
			detectCount++
			totalArea += best.area
			steeringAngle = convertXToSteeringServoAngle(best.centerX)
			totalSteeringAngle += int(steeringAngle)
		}

		moveCtx, moveCancel := context.WithTimeout(context.Background(), 1*time.Second)
		_ = h.steeringServoViam.Move(moveCtx, uint32(steeringAngle), nil)
		moveCancel()

		totalLatency := time.Since(frameStart)
		totalTotalLatency += totalLatency

		h.trackingLogFrame(logger, frames, detectionLatency, totalLatency, best, steeringAngle)
	}

	// Reset steering to neutral after test.
	resetCtx, resetCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer resetCancel()
	if err := h.steeringServoViam.Move(resetCtx, uint32(STEERING_NEUTRAL), nil); err != nil {
		logger.Warnf("error resetting steering to neutral after tracking test: %v", err)
	}

	return h.trackingBuildResult(args, frames, errorCount, detectCount,
		totalDetectionLatency, minDetectionLatency, maxDetectionLatency,
		totalTotalLatency, totalArea, totalSteeringAngle), nil
}

// trackingSelectService returns the vision service for the requested mode.
// Returns (nil, false) when color mode is requested but not configured.
func (h *hawkeye) trackingSelectService(mode string) (vision.Service, bool) {
	if mode == VISION_MODE_COLOR {
		return h.visionColorViam, false // nil if not configured — caller checks
	}
	return h.visionViam, true
}

func (h *hawkeye) trackingLogFrame(
	logger logging.Logger,
	frame int,
	detectionLatency, totalLatency time.Duration,
	best *visionDetection,
	steeringAngle servoDegrees,
) {
	if best != nil {
		logger.Infof("frame %d: detection=%s total=%s detected=true area=%d centerX=%d steering=%d",
			frame,
			detectionLatency.Round(time.Millisecond),
			totalLatency.Round(time.Millisecond),
			best.area, best.centerX, steeringAngle)
	} else {
		logger.Infof("frame %d: detection=%s total=%s detected=false steering=neutral",
			frame,
			detectionLatency.Round(time.Millisecond),
			totalLatency.Round(time.Millisecond))
	}
}

func (h *hawkeye) trackingBuildResult(
	args argsTestTracking,
	frames, errorCount, detectCount int,
	totalDetectionLatency, minDetectionLatency, maxDetectionLatency time.Duration,
	totalTotalLatency time.Duration,
	totalArea visionPixels,
	totalSteeringAngle int,
) map[string]any {
	successfulFrames := frames - errorCount

	var (
		avgDetectionLatencyMs float64
		minDetectionLatencyMs int64
		maxDetectionLatencyMs int64
		avgTotalLatencyMs     float64
		avgHz                 float64
		detectionRate         float64
		avgAreaPx             float64
		avgSteeringAngle      float64
	)

	if successfulFrames > 0 {
		avgDetectionLatencyMs = float64(totalDetectionLatency.Milliseconds()) / float64(successfulFrames)
		minDetectionLatencyMs = minDetectionLatency.Milliseconds()
		maxDetectionLatencyMs = maxDetectionLatency.Milliseconds()
		avgTotalLatencyMs = float64(totalTotalLatency.Milliseconds()) / float64(successfulFrames)
		detectionRate = float64(detectCount) / float64(successfulFrames)
		if totalDetectionLatency > 0 {
			avgHz = float64(successfulFrames) / totalDetectionLatency.Seconds()
		}
	}
	if detectCount > 0 {
		avgAreaPx = float64(totalArea) / float64(detectCount)
		avgSteeringAngle = float64(totalSteeringAngle) / float64(detectCount)
	}

	return map[string]any{
		"mode":                      args.Mode,
		"duration_secs":             args.DurationSecs,
		"frames":                    frames,
		"errors":                    errorCount,
		"detection_rate":            detectionRate,
		"avg_detection_latency_ms":  avgDetectionLatencyMs,
		"min_detection_latency_ms":  minDetectionLatencyMs,
		"max_detection_latency_ms":  maxDetectionLatencyMs,
		"avg_total_latency_ms":      avgTotalLatencyMs,
		"avg_hz":                    avgHz,
		"avg_area_px":               avgAreaPx,
		"avg_steering_angle":        avgSteeringAngle,
	}
}
