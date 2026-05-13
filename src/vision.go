package main

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/viam-labs/hawkeye/util"
	"go.viam.com/rdk/services/vision"
	"go.viam.com/rdk/vision/objectdetection"
	"go.viam.com/rdk/vision/viscapture"
)

func (h *hawkeye) handleStartVision(args argsStartVision) (map[string]any, error) {
	// Before the routine starts at all, so a path that cannot be made to work
	// fails the start rather than leaving it silently not recording.
	if err := h.startVisionRecording(args.RecordDir); err != nil {
		return nil, err
	}

	err := h.visionRoutine.Start(h.visionLogger, h.visionTick, VISION_TICK_RATE)
	if err != nil {
		h.stopVisionRecording()
		return nil, err
	}

	return map[string]any{"status": "started", "record_dir": args.RecordDir}, nil
}

func (h *hawkeye) handleStopVision(_ argsStopVision) (map[string]any, error) {
	err := h.visionRoutine.Stop()
	if err != nil {
		return nil, err
	}

	h.stopVisionRecording()
	h.visionLastDetection.Store(nil)

	return map[string]any{"status": "stopped"}, nil
}

// startVisionRecording gives the routine somewhere to write annotated frames,
// which is also what tells it to record at all. An empty path means no recording
// and clears whatever the last start selected.
//
// A path that cannot be created is an error, not a warning: recording is opt-in,
// so a run that quietly produced none would only surface when there was nothing
// to scp off.
func (h *hawkeye) startVisionRecording(recordDir string) error {
	if recordDir == "" {
		h.stopVisionRecording()
		return nil
	}

	if err := os.MkdirAll(recordDir, 0o755); err != nil {
		return errors.Wrapf(err, "error creating recording directory %q", recordDir)
	}

	h.visionRecordDir.Store(&recordDir)
	h.visionLogger.Infof("recording to %q; assemble it with: "+
		`ffmpeg -framerate 10 -pattern_type glob -i '%s/*.jpg' -c:v libx264 -pix_fmt yuv420p %s.mp4`,
		recordDir, recordDir, recordDir)

	return nil
}

// stopVisionRecording goes back to fetching detections alone.
func (h *hawkeye) stopVisionRecording() {
	if recordDir := h.visionRecordDir.Swap(nil); recordDir != nil {
		h.visionLogger.Infof("stopped recording to %q", *recordDir)
	}
}

func (h *hawkeye) useVisionBall() {
	h.visionViam.Store(util.Ptr(h.visionBallViam))
	h.visionLogger.Infof("detecting with the ball vision service %q", h.visionBallViam.Name().Name)
}

func (h *hawkeye) useVisionPerson() {
	h.visionViam.Store(util.Ptr(h.visionPersonViam))
	h.visionLogger.Infof("detecting with the person vision service %q", h.visionPersonViam.Name().Name)
}

// loadVisionService returns the detector currently selected. Nothing is selected
// between a Close and the Reconfigure after it — the one case this errors for.
func (h *hawkeye) loadVisionService() (vision.Service, error) {
	visionViam := h.visionViam.Load()
	if visionViam == nil {
		return nil, errors.New("no vision service selected; the module may still be reconfiguring")
	}

	return *visionViam, nil
}

// visionTick fetches detections from the configured camera, reduces them to one
// box, and publishes it on visionLastDetection for the fetch routine.
func (h *hawkeye) visionTick(ctx context.Context) {
	visionViam, err := h.loadVisionService()
	if err != nil {
		h.visionThrottledLogger.Warnf("skipping tick: %v", err)
		return
	}

	// Recording needs the frame the detections came from, which only
	// CaptureAllFromCamera returns. Nothing else about the tick changes,
	// so the footage shows what the routine actually published.
	//
	// Only ticks whose frame will be kept ask for one — 10 a second out of the
	// routine's 40. The other 30 would ship and decode an image to throw it away.
	var (
		recordDir      = h.visionRecordDir.Load()
		recordThisTick = recordDir != nil && time.Since(h.visionRecordLastFrameTime) >= 100*time.Millisecond
		detections     []objectdetection.Detection
		frame          image.Image
	)
	if !recordThisTick {
		detections, err = visionViam.DetectionsFromCamera(ctx, h.cameraName, nil)
	} else {
		h.visionRecordLastFrameTime = time.Now()

		var capture viscapture.VisCapture
		capture, err = visionViam.CaptureAllFromCamera(ctx, h.cameraName,
			viscapture.CaptureOptions{ReturnImage: true, ReturnDetections: true}, nil)
		detections, frame = capture.Detections, capture.Image
	}
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
	if visionViam == h.visionBallViam {
		newDetection = computeDetectionSingle(detections)
	} else {
		newDetection = computeDetectionPair(detections)
	}

	// Publish before recording, so the encode never sits between a detection and
	// the routines waiting on it.
	if newDetection == nil {
		h.visionThrottledLogger.Info("found no detections")
		h.visionLastDetection.Store(nil)
	} else {
		oldDetection := h.visionLastDetection.Swap(newDetection)
		h.visionThrottledLogger.Infof("got new detection %s, replacing %s", newDetection, oldDetection)
	}

	if recordThisTick && frame != nil {
		h.visionRecordFrame(*recordDir, frame, detections)
	}
}

// visionRecordFrame writes one frame with the detector's boxes and the fetch's
// state drawn on it, named for the time taken so the directory replays in order.
// Recording is a debug aid, so a frame that fails to write is logged and dropped
// rather than failing the tick.
func (h *hawkeye) visionRecordFrame(recordDir string, frame image.Image, detections []objectdetection.Detection) {
	// A JPEG decodes to an *image.YCbCr, which cannot be drawn on.
	bounds := frame.Bounds()
	canvas := image.NewNRGBA(bounds)
	draw.Draw(canvas, bounds, frame, bounds.Min, draw.Src)

	for _, d := range detections {
		if box := d.BoundingBox(); box != nil {
			drawVisionDetectionBox(canvas, *box)
		}
	}

	// Burning the state in makes the footage a trace of the state machine.
	// OverlayText fixes position, color and size and copies the frame again. That
	// is the whole of the RDK's text support, and the copy is cheap at 10 fps.
	annotated := objectdetection.OverlayText(canvas, string(h.loadFetchState()))

	path := filepath.Join(recordDir, time.Now().Format("15-04-05.000")+".jpg")
	file, err := os.Create(path)
	if err != nil {
		h.visionThrottledLogger.Warnf("error creating frame %q: %v", path, err)
		return
	}
	defer file.Close()

	if err := jpeg.Encode(file, annotated, &jpeg.Options{Quality: 80}); err != nil {
		h.visionThrottledLogger.Warnf("error encoding frame %q: %v", path, err)
	}
}

// drawVisionDetectionBox outlines one raw detection in red. No label: a color
// detector only reports the color it matched, which is the same for every box in
// the frame and covers up the thing being looked at.
func drawVisionDetectionBox(canvas *image.NRGBA, box image.Rectangle) {
	box = box.Intersect(canvas.Bounds())
	if box.Empty() {
		return
	}

	const thickness = 2
	var (
		red   = image.NewUniform(color.NRGBA{R: 255, A: 255})
		edges = []image.Rectangle{
			image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+thickness), // top
			image.Rect(box.Min.X, box.Max.Y-thickness, box.Max.X, box.Max.Y), // bottom
			image.Rect(box.Min.X, box.Min.Y, box.Min.X+thickness, box.Max.Y), // left
			image.Rect(box.Max.X-thickness, box.Min.Y, box.Max.X, box.Max.Y), // right
		}
	)

	for _, edge := range edges {
		draw.Draw(canvas, edge.Intersect(box), red, image.Point{}, draw.Src)
	}
}

// computeDetectionSingle reduces the frame to one object — the tennis ball —
// by stitching split boxes together, dropping what sits too high to be on the
// floor, and returning the largest of what is left.
func computeDetectionSingle(preProcessedDetections []objectdetection.Detection) *visionDetection {
	postProcessedDetections := dropDetectionsTooHighInFrame(
		mergeNearbyDetections(preProcessedDetections, VISION_DETECTION_SINGLE_MERGE_MAX_GAP))

	var bestDetection *visionDetection
	for i := range postProcessedDetections {
		if bestDetection == nil || postProcessedDetections[i].area > bestDetection.area {
			bestDetection = &postProcessedDetections[i]
		}
	}

	return bestDetection
}

// mergeNearbyDetections collapses detections within maxGap of each other into the
// one box containing them. The model may split a ball along its seam or a shoe
// along its laces into two or three boxes, each too small to be worth anything.
//
// Both pipelines start here, and maxGap is the caller's to choose since it depends
// on what is being looked at — see VISION_DETECTION_SINGLE_MERGE_MAX_GAP and
// VISION_DETECTION_PAIR_MERGE_MAX_GAP.
func mergeNearbyDetections(detections []objectdetection.Detection, maxGap visionPixels) []visionDetection {
	mergedDetections := make([]visionDetection, 0, len(detections))
	for _, d := range detections {
		box := d.BoundingBox()
		if box == nil {
			continue
		}

		mergedDetections = append(mergedDetections, newVisionDetection(
			visionPixels(box.Min.X), visionPixels(box.Min.Y),
			visionPixels(box.Max.X), visionPixels(box.Max.Y),
		))
	}

	// Merging a pair can bring a third within range, so sweep until a pass finds
	// nothing left to merge.
	for mergedAPair := true; mergedAPair; {
		mergedAPair = false

		for i := 0; i < len(mergedDetections) && !mergedAPair; i++ {
			for j := i + 1; j < len(mergedDetections); j++ {
				if !mergedDetections[i].isNear(&mergedDetections[j], maxGap) {
					continue
				}

				mergedDetections[i] = mergedDetections[i].mergedWith(&mergedDetections[j])
				mergedDetections = append(mergedDetections[:j], mergedDetections[j+1:]...)
				mergedAPair = true

				break
			}
		}
	}

	return mergedDetections
}

// dropDetectionsTooHighInFrame removes detections centered above
// VISION_DETECTION_MIN_CENTER_Y. Both pipelines run it after the stitching, so an
// object straddling the line is judged by where the whole of it sits: filtering
// first would drop an object's upper boxes and drag the survivor's center down —
// moving the very thing being measured.
func dropDetectionsTooHighInFrame(detections []visionDetection) []visionDetection {
	keptDetections := make([]visionDetection, 0, len(detections))
	for _, d := range detections {
		if d.centerY >= VISION_DETECTION_MIN_CENTER_Y {
			keptDetections = append(keptDetections, d)
		}
	}

	return keptDetections
}

// computeDetectionPair reduces the frame to a person's two shoes, returned as the
// one box around both — the same shape computeDetectionSingle gives, so the fetch
// steers by it either way.
//
// Stitching stays tight (VISION_DETECTION_PAIR_MERGE_MAX_GAP) so a shoe split
// along its laces rejoins while the two shoes stay apart as the pair being looked
// for. Dropping what sits too high happens before pairing, so a real shoe cannot
// pair with something that is not one. Largest qualifying pair wins: with two
// people in frame, the nearer one gets the ball.
func computeDetectionPair(preProcessedDetections []objectdetection.Detection) *visionDetection {
	postProcessedDetections := dropDetectionsTooHighInFrame(
		mergeNearbyDetections(preProcessedDetections, VISION_DETECTION_PAIR_MERGE_MAX_GAP))

	var bestDetection *visionDetection
	for i := range postProcessedDetections {
		for j := i + 1; j < len(postProcessedDetections); j++ {
			if !postProcessedDetections[i].isDetectionPairWith(&postProcessedDetections[j]) {
				continue
			}

			mergedDetectionPair := postProcessedDetections[i].mergedWith(&postProcessedDetections[j])
			if bestDetection == nil || mergedDetectionPair.area > bestDetection.area {
				bestDetection = &mergedDetectionPair
			}
		}
	}

	return bestDetection
}

// isDetectionPairWith reports whether d and other are similar in size, level, and
// close enough to be one person's two shoes rather than two people. See the
// VISION_DETECTION_PAIR_* constants.
func (d *visionDetection) isDetectionPairWith(other *visionDetection) bool {
	var (
		smallerArea = min(d.area, other.area)
		largerArea  = max(d.area, other.area)
	)

	if largerArea <= 0 || float64(smallerArea)/float64(largerArea) < VISION_DETECTION_PAIR_MIN_AREA_RATIO {
		return false
	}

	var (
		gapX = max(d.centerX-other.centerX, other.centerX-d.centerX)
		gapY = max(d.centerY-other.centerY, other.centerY-d.centerY)
	)

	return gapX <= VISION_DETECTION_PAIR_MAX_CENTER_X_GAP && gapY <= VISION_DETECTION_PAIR_MAX_CENTER_Y_GAP
}

// handleTestVision runs one frame through the pipeline the caller asks for and
// reports the detection that comes out — the same box the fetch would get.
//
// It reads that kind's detector directly rather than the selected one, so testing
// a pair mid-fetch does not steal the running routine's detector.
func (h *hawkeye) handleTestVision(args argsTestVision) (map[string]any, error) {
	var (
		kind       = visionDetectionKind(args.Detect)
		visionViam = h.visionBallViam
	)
	if kind == VISION_DETECTION_PAIR {
		visionViam = h.visionPersonViam
	}

	h.visionLogger.Infof("getting %s detections from camera %q using vision service %q",
		kind, h.cameraName, visionViam.Name().Name)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	detections, err := visionViam.DetectionsFromCamera(ctx, h.cameraName, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get detections from camera")
	}

	// Raw boxes go to the log only; what comes back is the one they combine into.
	for i, d := range detections {
		h.visionLogger.Infof("  [%d] %s score=%.3f box=%v", i, d.Label(), d.Score(), d.BoundingBox())
	}

	var detection *visionDetection
	if kind == VISION_DETECTION_SINGLE {
		detection = computeDetectionSingle(detections)
	} else {
		detection = computeDetectionPair(detections)
	}
	elapsed := time.Since(start).Round(time.Millisecond)

	h.visionLogger.Infof("combined %d raw detections into %s (elapsed time: %s)",
		len(detections), detection, elapsed)

	return map[string]any{
		"status":         "ok",
		"detect":         args.Detect,
		"vision_service": visionViam.Name().Name,
		"raw_detections": len(detections),
		"detection":      detection.String(),
		"elapsed_time":   elapsed.String(),
	}, nil
}
