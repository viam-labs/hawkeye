package main

import (
	"context"
	"fmt"
	"image"
	"time"

	"github.com/pkg/errors"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/vision/objectdetection"
)

type visionDetection struct {
	area    visionPixels
	centerX visionPixels
	bbox    *image.Rectangle // raw bounding box; used by fetch for lock-zone updates
}

func (h *hawkeye) handleStartVision(args argsStartVision) (map[string]any, error) {
	h.visionMode = args.Mode
	err := h.visionRoutine.Start(h.visionLogger, h.visionTick, VISION_TICK_RATE)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "started", "mode": args.Mode}, err
}

func (h *hawkeye) handleStopVision(_ argsStopVision) (map[string]any, error) {
	err := h.visionRoutine.Stop()
	if err != nil {
		return nil, err
	}
	h.visionLastDetection.Store(nil)
	h.visionMode = VISION_MODE_ML
	return map[string]any{"status": "stopped"}, nil
}

// visionActiveService returns the vision service and whether ML label/confidence
// filtering applies for the current mode. Color detector detections are accepted
// without label filtering since the service is pre-configured to see only the ball's color.
// Hybrid ML+color tracking belongs to the fetch routine, not to the standalone vision tick.
func (h *hawkeye) visionActiveService() (svc vision.Service, useMLFilter bool) {
	if h.visionMode == VISION_MODE_COLOR && h.visionColorViam != nil {
		return h.visionColorViam, false
	}
	return h.visionViam, true
}

// visionTick fetches detections from the active vision service, picks the largest,
// and publishes it to visionLastDetection for the steering and motor routines.
func (h *hawkeye) visionTick(ctx context.Context) {
	svc, useMLFilter := h.visionActiveService()

	detections, err := svc.DetectionsFromCamera(ctx, h.cameraName, nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.visionLogger.Info("stopping due to context cancellation")
			return
		}

		h.visionThrottledLogger.Warnf("error getting detection: %v", err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	var newDetection *visionDetection
	if useMLFilter {
		newDetection = computeLargestDetection(detections)
	} else {
		newDetection = computeLargestDetectionAny(detections)
	}

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

// computeLargestDetection returns the largest detection passing the ML label and
// confidence filter. Use for ML mode.
func computeLargestDetection(detections []objectdetection.Detection) *visionDetection {
	return computeLargestFiltered(detections, isBallDetection)
}

// computeLargestDetectionAny returns the largest detection without label/confidence
// filtering. Use for color detector mode where the service is already ball-specific.
func computeLargestDetectionAny(detections []objectdetection.Detection) *visionDetection {
	return computeLargestFiltered(detections, func(_ objectdetection.Detection) bool { return true })
}

func computeLargestFiltered(detections []objectdetection.Detection, accept func(objectdetection.Detection) bool) *visionDetection {
	var best *visionDetection
	for _, d := range detections {
		if !accept(d) {
			continue
		}
		box := d.BoundingBox()
		if box == nil {
			continue
		}

		area := visionPixels(box.Dx() * box.Dy())
		if best == nil || area > best.area {
			if best == nil {
				best = &visionDetection{}
			}
			best.area = area
			best.centerX = visionPixels((box.Min.X + box.Max.X) / 2)
			best.bbox = box
		}
	}
	return best
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

// handleTestVision runs one detection call against the selected vision service and
// returns all raw detections. Use mode="ml" vs mode="color" to spot-check each
// detector without starting the full fetch routine.
func (h *hawkeye) handleTestVision(args argsTestVision) (map[string]any, error) {
	svc := h.visionViam
	if args.Mode == VISION_MODE_COLOR {
		if h.visionColorViam == nil {
			return nil, errors.New("color detector not configured (vision_color missing from robot config)")
		}
		svc = h.visionColorViam
	}

	h.visionLogger.Infof("getting detections from camera %q (mode=%s)", h.cameraName, args.Mode)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	detections, err := svc.DetectionsFromCamera(ctx, h.cameraName, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get detections from camera")
	}
	elapsed := time.Since(start)

	h.visionLogger.Infof("got %d detections in %s", len(detections), elapsed.Round(time.Millisecond))

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
			entry["area"] = box.Dx() * box.Dy()
		}
		results = append(results, entry)
		h.visionLogger.Infof("  [%d] label=%q score=%.3f bbox=%v", i, d.Label(), d.Score(), box)
	}

	return map[string]any{
		"mode":        args.Mode,
		"detections":  results,
		"elapsed_ms":  elapsed.Milliseconds(),
	}, nil
}
