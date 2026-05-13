package main

import (
	"fmt"
	"math"
)

type visionDetection struct {
	// The bounding box, kept so the fetch can tell whether a detection holds still.
	minX visionPixels
	minY visionPixels
	maxX visionPixels
	maxY visionPixels

	// Derived once in newVisionDetection. A detection is built, never edited, so
	// these cannot fall out of step with the bounds above.
	area    visionPixels
	width   visionPixels
	height  visionPixels
	centerX visionPixels
	centerY visionPixels
}

// newVisionDetection builds a detection from a bounding box.
func newVisionDetection(minX, minY, maxX, maxY visionPixels) visionDetection {
	var (
		width  = maxX - minX
		height = maxY - minY
	)
	return visionDetection{
		minX:    minX,
		minY:    minY,
		maxX:    maxX,
		maxY:    maxY,
		area:    width * height,
		width:   width,
		height:  height,
		centerX: (minX + maxX) / 2,
		centerY: (minY + maxY) / 2,
	}
}

// String renders the whole detection, so every log mentioning one carries all of
// it rather than whichever fields the call site cared about. Nil formats as
// "<none>", which is how waiting states report that they are still waiting.
//
// Points are (x,y) measured from the frame's top-left corner downward, so top is
// the smaller y.
func (d *visionDetection) String() string {
	if d == nil {
		return "<none>"
	}

	return fmt.Sprintf(
		"<area=%d size=%dx%d center=(%d,%d) topLeft=(%d,%d) topRight=(%d,%d) bottomLeft=(%d,%d) bottomRight=(%d,%d)>",
		d.area,
		d.width, d.height,
		d.centerX, d.centerY,
		d.minX, d.minY,
		d.maxX, d.minY,
		d.minX, d.maxY,
		d.maxX, d.maxY,
	)
}

// isNear reports whether d and other overlap or come within maxGap pixels. Both
// axes must be in range: boxes that line up vertically but sit at opposite ends
// of the frame are not the same object.
func (d *visionDetection) isNear(other *visionDetection, maxGap visionPixels) bool {
	var (
		gapX = max(d.minX-other.maxX, other.minX-d.maxX)
		gapY = max(d.minY-other.maxY, other.minY-d.maxY)
	)

	return gapX <= maxGap && gapY <= maxGap
}

// mergedWith returns the smallest detection containing both d and other.
func (d *visionDetection) mergedWith(other *visionDetection) visionDetection {
	return newVisionDetection(
		min(d.minX, other.minX),
		min(d.minY, other.minY),
		max(d.maxX, other.maxX),
		max(d.maxY, other.maxY),
	)
}

// isWithinGripperReach reports whether the jaws can close on d: low enough in the
// frame to be at the bumper, and centered enough to be taken hold of rather than
// shouldered aside. Size plays no part — see VISION_GRIPPER_Y_MIN_THRESHOLD.
func (d *visionDetection) isWithinGripperReach() bool {
	return d.minY >= VISION_GRIPPER_Y_MIN_THRESHOLD &&
		d.centerX >= VISION_GRIPPER_X_MIN_THRESHOLD &&
		d.centerX <= VISION_GRIPPER_X_MAX_THRESHOLD
}

// hasDriftedFrom reports whether any of d's edges have moved more than maxDrift
// from anchor's, as a fraction of the anchor's own width and height. Measuring
// against the box rather than the frame keeps this scale-invariant: a ball across
// the room drifts fewer pixels than one at the bumper for the same real movement.
func (d *visionDetection) hasDriftedFrom(anchor *visionDetection, maxDrift float64) bool {
	var (
		maxXDrift = maxDrift * float64(anchor.width)
		maxYDrift = maxDrift * float64(anchor.height)
	)

	return math.Abs(float64(d.minX-anchor.minX)) > maxXDrift ||
		math.Abs(float64(d.maxX-anchor.maxX)) > maxXDrift ||
		math.Abs(float64(d.minY-anchor.minY)) > maxYDrift ||
		math.Abs(float64(d.maxY-anchor.maxY)) > maxYDrift
}
