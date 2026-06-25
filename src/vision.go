package main

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/vision/objectdetection"
)

type visionDetection struct {
	area    visionPixels
	centerX visionPixels
}

func (h *hawkeye) handleStartVision(_ argsStartVision) (map[string]any, error) {
	err := h.visionRoutine.Start(h.visionLogger, h.visionTick, VISION_TICK_RATE)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "started"}, err
}

func (h *hawkeye) handleStopVision(_ argsStopVision) (map[string]any, error) {
	err := h.visionRoutine.Stop()
	if err != nil {
		return nil, err
	}
	h.visionLastDetection.Store(nil)
	return map[string]any{"status": "stopped"}, nil
}

// visionTick fetches detections from the configured camera, picks the largest,
// and publishes it to visionLastDetection for the steering and motor routines.
func (h *hawkeye) visionTick(ctx context.Context) {
	detections, err := h.visionViam.DetectionsFromCamera(ctx, h.cameraName, nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.visionLogger.Info("stopping due to context cancellation")
			return
		}

		h.visionThrottledLogger.Warnf("error getting detection: %v", err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	newDetection := computeLargestDetection(detections)
	if newDetection == nil {
		h.visionThrottledLogger.Info("found no detections")
		h.visionLastDetection.Store(nil)
		return
	}

	oldDetection := h.visionLastDetection.Swap(newDetection)

	var msg string
	if oldDetection == nil {
		msg = "old area=<nil> centerX=<nil>"
	} else {
		msg = fmt.Sprintf("old area=%d old centerX=%d", oldDetection.area, oldDetection.centerX)
	}
	h.visionThrottledLogger.Infof("got new detection (%s | new area=%d centerX=%d)",
		msg, newDetection.area, newDetection.centerX)
}

func computeLargestDetection(detections []objectdetection.Detection) *visionDetection {
	var bestDetection *visionDetection
	for _, d := range detections {
		if !isBallDetection(d) {
			continue
		}
		box := d.BoundingBox()
		if box == nil {
			continue
		}

		if bestDetection == nil {
			bestDetection = &visionDetection{}
		}

		area := visionPixels(box.Dx() * box.Dy())
		if area > bestDetection.area {
			bestDetection.area = area
			bestDetection.centerX = visionPixels((box.Min.X + box.Max.X) / 2)
		}
	}

	return bestDetection
}

// isBallDetection reports whether a detection should be treated as a tennis ball.
// Accepts label "0" (Viam app bug: numeric index instead of class name) and
// "tennis ball". Rejects anything below VISION_MIN_CONFIDENCE.
func isBallDetection(d objectdetection.Detection) bool {
	label := d.Label()
	if label != VISION_BALL_LABEL_NUMERIC && label != VISION_BALL_LABEL_NAME {
		return false
	}
	return d.Score() >= VISION_MIN_CONFIDENCE
}

func (h *hawkeye) handleTestVision(_ argsTestVision) (map[string]any, error) {
	h.visionLogger.Infof("getting detections from camera %q", h.cameraName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	detections, err := h.visionViam.DetectionsFromCamera(ctx, h.cameraName, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get detections from camera")
	}
	end := time.Now()

	h.visionLogger.Infof("got %d detections from camera %q (elapsed time: %s)",
		len(detections), h.cameraName, end.Sub(start))

	results := make([]map[string]any, 0, len(detections))
	for i, d := range detections {
		box := d.BoundingBox()
		entry := map[string]any{
			"class":      d.Label(),
			"confidence": d.Score(),
		}
		if box != nil {
			entry["x_min"] = box.Min.X
			entry["y_min"] = box.Min.Y
			entry["x_max"] = box.Max.X
			entry["y_max"] = box.Max.Y
		}
		results = append(results, entry)
		h.visionLogger.Infof("  [%d] %s score=%.3f box=%v", i, d.Label(), d.Score(), box)
	}

	return map[string]any{"detections": results, "elapsedTime": fmt.Sprintf("%s", end.Sub(start))}, nil
}
