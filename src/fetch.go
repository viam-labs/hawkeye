package main

import (
	"context"
	"image"
	"math"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/multierr"
	"go.viam.com/rdk/vision/objectdetection"
)

// Fetch state machine states.
const (
	fetchStateAcquiring   = "acquiring"   // initial: ML runs to find ball and set lock zone
	fetchStateTracking    = "tracking"    // lock zone set: color detector runs within it
	fetchStateReacquiring = "reacquiring" // color lost ball: ML runs to re-establish lock zone
	fetchStateDone        = "done"        // ball reached FETCH_STOP_AREA: fetch halted itself
)

func (h *hawkeye) handleStartFetch(_ argsStartFetch) (map[string]any, error) {
	h.fetchState = fetchStateAcquiring
	h.fetchLockZone = nil
	h.fetchColorMisses = 0
	h.fetchCorrectionAttempts = 0
	h.fetchAcquireStreakStart = time.Time{}

	if h.visionColorViam == nil {
		h.fetchLogger.Warn("vision_color not configured; fetch will use ML-only tracking (no lock zone)")
	}

	if err := h.visionRoutine.Start(h.fetchLogger, h.fetchVisionTick, VISION_TICK_RATE); err != nil {
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

	return map[string]any{"status": "started", "color_detector": h.visionColorViam != nil}, nil
}

func (h *hawkeye) handleStopFetch(_ argsStopFetch) (map[string]any, error) {
	if h.fetchState == fetchStateDone {
		// Already halted itself via fetchHalt (motor/steering/vision stopped, servos neutral).
		return map[string]any{"status": "stopped"}, nil
	}

	motorErr := h.motorRoutine.Stop()
	steeringErr := h.steeringRoutine.Stop()
	visionErr := h.visionRoutine.Stop()
	if err := multierr.Combine(motorErr, steeringErr, visionErr); err != nil {
		return nil, err
	}

	// Reset servos to neutral now that ticks are stopped.
	h.motorNeutral()
	h.motorLastAngle = MOTOR_NEUTRAL

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil); err != nil {
		h.fetchLogger.Warnf("error resetting steering to neutral after stop: %v", err)
	}

	return map[string]any{"status": "stopped"}, nil
}

// fetchVisionTick is the vision tick used when the fetch routine is running.
// It implements a three-state machine:
//
//	ACQUIRING   → ML detector runs; on success sets lock zone and enters TRACKING.
//	TRACKING    → color detector runs within lock zone; on FETCH_COLOR_MISS_THRESHOLD
//	              consecutive misses switches to REACQUIRING.
//	REACQUIRING → ML detector runs; on success sets new lock zone and returns to TRACKING.
//
// When visionColorViam is not configured, every state runs ML-only (no lock zone).
func (h *hawkeye) fetchVisionTick(ctx context.Context) {
	if h.fetchState == fetchStateDone {
		return
	}

	switch h.fetchState {
	case fetchStateAcquiring, fetchStateReacquiring:
		h.fetchMLAcquire(ctx)
	case fetchStateTracking:
		if h.visionColorViam != nil {
			h.fetchColorTrack(ctx)
		} else {
			h.fetchMLAcquire(ctx)
		}
	}
}

// fetchMLAcquire runs the ML detector to find the ball and establish the lock zone.
// Requires FETCH_ACQUIRE_DEBOUNCE_DURATION of continuous detection (via
// fetchDebounceAcquire) before committing, to filter out one-off false
// positives. Transitions to fetchStateTracking on success; writes nil to
// visionLastDetection while searching so the motor stays stopped.
func (h *hawkeye) fetchMLAcquire(ctx context.Context) {
	start := time.Now()
	detections, err := h.visionViam.DetectionsFromCamera(ctx, h.cameraName, nil)
	latency := time.Since(start)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		h.fetchLogger.Warnf("[ML][%s] error (latency=%s): %v", h.fetchState, latency.Round(time.Millisecond), err)
		return
	}

	// Computes the largest bounding box
	best := computeLargestDetection(detections)
	if !h.fetchDebounceAcquire(best) {
		if best == nil {
			if h.fetchState == fetchStateAcquiring {
				h.fetchSearchForBall(ctx)
				return
			}
			h.fetchLogger.Infof("[ML][%s] NO BALL DETECTED! — motor stopped, retrying", h.fetchState)
			h.visionLastDetection.Store(nil)
			return
		}
		h.fetchLogger.Infof("[ML][%s] ball detected, debouncing (seen for %s, need %s)",
			h.fetchState, time.Since(h.fetchAcquireStreakStart).Round(time.Millisecond), FETCH_ACQUIRE_DEBOUNCE_DURATION)
		return
	}

	h.fetchLockOn(best, "ML")
}

// fetchLockOn transitions fetch into fetchStateTracking after ML finds the
// ball — whether from a normal fetchMLAcquire check or fetchSearchForBall's
// search-drive — setting the lock zone, resetting per-approach counters, and
// publishing the detection. source tags the log line with which path found
// the ball.
func (h *hawkeye) fetchLockOn(best *visionDetection, source string) {
	lockZone := h.fetchExpandBbox(best.bbox)
	h.fetchLockZone = lockZone
	h.fetchColorMisses = 0
	h.fetchCorrectionAttempts = 0
	h.fetchState = fetchStateTracking

	h.fetchLogger.Infof("[%s] LOCKED ON BALL!: area=%d centerX=%d lockZone=%v (state: →tracking)",
		source, best.area, best.centerX, lockZone)

	h.visionLastDetection.Store(best)
}

// fetchSearchForBall drives a forward left-right sweep while repeatedly
// polling the ML detector, for use when fetch starts and the first ML check
// finds nothing. Runs synchronously inside fetchVisionTick's call stack
// (blocks the vision routine's own goroutine for up to
// FETCH_SEARCH_MAX_DURATION) — same blocking-servo-sequence pattern as
// fetchCorrectAndRetry. Gives up and halts fetch entirely if nothing is found
// before the deadline.
func (h *hawkeye) fetchSearchForBall(ctx context.Context) {
	h.fetchLogger.Infof("[search] no ball on initial ML check — sweeping to search, up to %s", FETCH_SEARCH_MAX_DURATION)

	if err := h.motorRoutine.Stop(); err != nil {
		h.fetchLogger.Warnf("[search] error stopping motor routine: %v", err)
	}
	if err := h.steeringRoutine.Stop(); err != nil {
		h.fetchLogger.Warnf("[search] error stopping steering routine: %v", err)
	}

	start := time.Now()
	deadline := start.Add(FETCH_SEARCH_MAX_DURATION)

	var lastSweepAngle servoDegrees
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			h.fetchLogger.Info("[search] stopping due to context cancellation")
			h.fetchSettleServos()
			return
		}

		angle := fetchSearchSweepAngle(time.Since(start))
		if angle != lastSweepAngle {
			h.fetchLogger.Infof("[search] sweeping %s (angle=%d)", fetchSearchSweepDirectionLabel(angle), angle)
			lastSweepAngle = angle
		}
		if err := h.steeringServoViam.Move(ctx, uint32(angle), nil); err != nil {
			h.fetchLogger.Warnf("[search] error moving steering to angle %d: %v", angle, err)
		}
		if err := h.motorServoViam.Move(ctx, uint32(FETCH_SEARCH_MOTOR_SPEED), nil); err != nil {
			h.fetchLogger.Warnf("[search] error driving forward: %v", err)
		}
		h.steeringLastAngle = angle
		h.motorLastAngle = FETCH_SEARCH_MOTOR_SPEED
		h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_FORWARD

		detections, err := h.visionViam.DetectionsFromCamera(ctx, h.cameraName, nil)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				h.fetchLogger.Warnf("[search] error getting ML detection: %v", err)
			}
			continue
		}

		best := computeLargestDetection(detections)
		if !h.fetchDebounceAcquire(best) {
			continue
		}

		h.fetchLockOn(best, "search")

		if err := h.motorRoutine.Start(h.motorLogger, h.motorTick, MOTOR_TICK_RATE); err != nil {
			h.fetchLogger.Warnf("[search] error restarting motor routine: %v", err)
		}
		if err := h.steeringRoutine.Start(h.steeringLogger, h.steeringTick, STEERING_TICK_RATE); err != nil {
			h.fetchLogger.Warnf("[search] error restarting steering routine: %v", err)
		}
		return
	}

	h.fetchLogger.Infof("[search] gave up after %s with no ball found — halting fetch", FETCH_SEARCH_MAX_DURATION)
	h.fetchState = fetchStateDone
	h.fetchHalt()
}

// fetchSettleServos resets both servos to neutral using a fresh context, for
// use when a fetch sub-maneuver (search or correction) is cut short by
// context cancellation and can't rely on the (already-canceling) tick ctx.
func (h *hawkeye) fetchSettleServos() {
	h.motorNeutral()
	h.motorLastAngle = MOTOR_NEUTRAL

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil); err != nil {
		h.fetchLogger.Warnf("error resetting steering to neutral: %v", err)
	}
	h.steeringLastAngle = STEERING_NEUTRAL
}

// fetchColorTrack runs the color detector and filters results to the current lock zone.
// Updates the lock zone on each successful detection to follow the ball.
// Stops the motor when ball area reaches the current stop-area threshold
// (fetchStopArea — later than normal once a correction attempt has happened).
// Switches to fetchStateReacquiring after FETCH_COLOR_MISS_THRESHOLD consecutive misses.
func (h *hawkeye) fetchColorTrack(ctx context.Context) {
	start := time.Now()
	detections, err := h.visionColorViam.DetectionsFromCamera(ctx, h.cameraName, nil)
	latency := time.Since(start)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		h.fetchLogger.Warnf("[color] error (latency=%s): %v", latency.Round(time.Millisecond), err)
		return
	}

	// Locks on if the largest blob is inside the zone
	best := computeLargestDetectionAny(detections)
	if best != nil && h.fetchLockZone != nil && best.bbox != nil && !best.bbox.Overlaps(*h.fetchLockZone) {
		h.fetchLogger.Infof("[color] largest blob (area=%d centerX=%d) outside lock zone — filtering to zone",
			best.area, best.centerX)
		best = h.fetchFindBestInLockZone(detections)
	}

	if best == nil {
		h.fetchColorMisses++
		h.fetchThrottledLogger.Infof("[color] miss %d/%d lockZone=%v — motor stopped",
			h.fetchColorMisses, FETCH_COLOR_MISS_THRESHOLD, h.fetchLockZone)
		h.visionLastDetection.Store(nil)

		if h.fetchColorMisses >= FETCH_COLOR_MISS_THRESHOLD {
			h.fetchLogger.Infof("[color] %d consecutive misses — switching to ML re-acquisition", h.fetchColorMisses)
			h.fetchState = fetchStateReacquiring
			h.fetchLockZone = nil
		}
		return
	}

	// Ball found. Update lock zone to follow ball.
	newZone := h.fetchExpandBbox(best.bbox)
	h.fetchLogger.Infof("[color] BALL BEING TRACKED!: area=%d centerX=%d oldZone=%v newZone=%v misses→0",
		best.area, best.centerX, h.fetchLockZone, newZone)
	h.fetchLockZone = newZone
	h.fetchColorMisses = 0

	// Halt or correct when ball is close enough. Fetch is a one-shot operation:
	// halt only once centered (or once correction attempts are exhausted);
	// otherwise back up and steer toward center before re-approaching.
	stopArea := fetchStopArea(h.fetchCorrectionAttempts)
	if best.area >= stopArea {
		h.visionLastDetection.Store(nil)

		if fetchShouldHalt(best.centerX, h.fetchCorrectionAttempts) {
			h.fetchLogger.Infof("[color] BALL CLOSE ENOUGH, HALTING!: area=%d >= stopArea=%d centerX=%d — halting fetch",
				best.area, stopArea, best.centerX)
			h.fetchState = fetchStateDone
			h.fetchHalt()
			return
		}

		h.fetchCorrectAndRetry(ctx, best)
		return
	}

	h.visionLastDetection.Store(best)
}

// fetchCorrectAndRetry backs the car up while steering toward center, then
// resumes forward tracking so the next approach re-checks the stop condition.
// Runs synchronously inside fetchColorTrack's call stack (blocks the vision
// routine's own goroutine for up to FETCH_CORRECTION_MAX_DURATION) — same
// blocking-servo-sequence pattern as handleTestMotor.
func (h *hawkeye) fetchCorrectAndRetry(ctx context.Context, best *visionDetection) {
	h.fetchCorrectionAttempts++
	h.fetchLogger.Infof("[correct] attempt %d/%d: area=%d centerX=%d offset=%.1f — reversing to re-center",
		h.fetchCorrectionAttempts, FETCH_CORRECTION_MAX_ATTEMPTS, best.area, best.centerX, fetchCenterOffset(best.centerX))

	if err := h.motorRoutine.Stop(); err != nil {
		h.fetchLogger.Warnf("[correct] error stopping motor routine: %v", err)
	}
	if err := h.steeringRoutine.Stop(); err != nil {
		h.fetchLogger.Warnf("[correct] error stopping steering routine: %v", err)
	}

	if h.motorLastDriveDirection == MOTOR_DRIVE_DIRECTION_FORWARD {
		if err := h.motorArmReverse(ctx); err != nil {
			h.fetchLogger.Warnf("[correct] error arming reverse: %v", err)
		}
	}

	lastAngle := fetchMirroredSteeringAngle(best.centerX)
	lastArea := best.area
	centered := false
	deadline := time.Now().Add(FETCH_CORRECTION_MAX_DURATION)

	canceled := false
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			h.fetchLogger.Info("[correct] stopping due to context cancellation")
			canceled = true
			break
		}

		if detections, err := h.visionColorViam.DetectionsFromCamera(ctx, h.cameraName, nil); err == nil {
			if found := computeLargestDetectionAny(detections); found != nil {
				lastAngle = fetchMirroredSteeringAngle(found.centerX)
				lastArea = found.area
				centered = fetchIsCentered(found.centerX)
			}
		}

		if err := h.steeringServoViam.Move(ctx, uint32(lastAngle), nil); err != nil {
			h.fetchLogger.Warnf("[correct] error moving steering to angle %d: %v", lastAngle, err)
		}
		if err := h.motorServoViam.Move(ctx, uint32(MOTOR_REVERSE_RETRY), nil); err != nil {
			h.fetchLogger.Warnf("[correct] error driving reverse: %v", err)
		}

		if centered {
			break
		}
		time.Sleep(FETCH_CORRECTION_POLL_INTERVAL)
	}

	h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_REVERSE
	h.motorNeutral()
	h.motorLastDriveDirection = MOTOR_DRIVE_DIRECTION_NEUTRAL
	h.motorLastAngle = MOTOR_NEUTRAL
	if canceled {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil); err != nil {
			h.fetchLogger.Warnf("[correct] error resetting steering to neutral after cancellation: %v", err)
		}
		h.steeringLastAngle = STEERING_NEUTRAL
	}

	h.fetchLogger.Infof("[correct] attempt %d/%d finished: centered=%v area=%d",
		h.fetchCorrectionAttempts, FETCH_CORRECTION_MAX_ATTEMPTS, centered, lastArea)

	if ctx.Err() != nil {
		return // fetch is shutting down — don't restart motor/steering routines
	}

	if err := h.motorRoutine.Start(h.motorLogger, h.motorTick, MOTOR_TICK_RATE); err != nil {
		h.fetchLogger.Warnf("[correct] error restarting motor routine: %v", err)
	}
	if err := h.steeringRoutine.Start(h.steeringLogger, h.steeringTick, STEERING_TICK_RATE); err != nil {
		h.fetchLogger.Warnf("[correct] error restarting steering routine: %v", err)
	}
}

// fetchHalt stops the motor, steering, and vision routines and resets both
// servos to neutral, so fetch behaves as a one-shot "go get the ball" op
// instead of idling forever once FETCH_STOP_AREA is reached.
//
// fetchHalt runs inside fetchVisionTick, which runs on the vision routine's
// own goroutine, so a synchronous visionRoutine.Stop() here would deadlock
// waiting on itself — that Stop is dispatched async instead.
func (h *hawkeye) fetchHalt() {
	h.fetchLogger.Info("[fetch] halting: motor, steering, and vision stopping")

	// Stop() errors are tolerated (and the neutral-reset still runs) since
	// callers like fetchSearchForBall's give-up path may have already
	// stopped these routines themselves before calling fetchHalt.
	if err := h.motorRoutine.Stop(); err != nil {
		h.fetchLogger.Warnf("error stopping motor routine after fetch completion: %v", err)
	}
	h.motorBrakeThenNeutral()

	if err := h.steeringRoutine.Stop(); err != nil {
		h.fetchLogger.Warnf("error stopping steering routine after fetch completion: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.steeringServoViam.Move(ctx, uint32(STEERING_NEUTRAL), nil); err != nil {
		h.fetchLogger.Warnf("error resetting steering to neutral after fetch completion: %v", err)
	}

	go func() {
		if err := h.visionRoutine.Stop(); err != nil {
			h.fetchLogger.Warnf("error stopping vision routine after fetch completion: %v", err)
		}
	}()
}

// fetchFindBestInLockZone returns the largest detection whose bounding box overlaps
// the current lock zone. No label/confidence filter — the color detector is already
// ball-specific by configuration. Returns nil if no overlapping detection exists.
func (h *hawkeye) fetchFindBestInLockZone(detections []objectdetection.Detection) *visionDetection {
	var best *visionDetection
	for _, d := range detections {
		box := d.BoundingBox()
		if box == nil {
			continue
		}
		if h.fetchLockZone != nil && !box.Overlaps(*h.fetchLockZone) {
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

// fetchExpandBbox adds FETCH_LOCK_ZONE_MARGIN_PX to each side of the bounding box
// and clamps to the camera frame. Returns nil if bbox is nil.
func (h *hawkeye) fetchExpandBbox(bbox *image.Rectangle) *image.Rectangle {
	if bbox == nil {
		return nil
	}
	m := FETCH_LOCK_ZONE_MARGIN_PX
	expanded := image.Rectangle{
		Min: image.Point{
			X: max(bbox.Min.X-m, int(VISION_MIN_X)),
			Y: max(bbox.Min.Y-m, 0),
		},
		Max: image.Point{
			X: min(bbox.Max.X+m, int(VISION_MAX_X)),
			Y: min(bbox.Max.Y+m, VISION_MAX_Y),
		},
	}
	return &expanded
}

// fetchCenterOffset returns centerX's signed pixel offset from the camera
// frame's horizontal center. Positive means the ball is right of center.
func fetchCenterOffset(centerX visionPixels) float64 {
	return float64(centerX) - VISION_FRAME_CENTER_X
}

// fetchIsCentered reports whether centerX is within FETCH_CENTER_TOLERANCE_PX
// of frame center — i.e. "directly in front" of the camera.
func fetchIsCentered(centerX visionPixels) bool {
	return math.Abs(fetchCenterOffset(centerX)) <= float64(FETCH_CENTER_TOLERANCE_PX)
}

// fetchShouldHalt reports whether fetch should halt now rather than attempt
// another reverse-and-retry correction: either the ball is already centered,
// or the correction attempt budget is exhausted.
func fetchShouldHalt(centerX visionPixels, attempts int) bool {
	return fetchIsCentered(centerX) || attempts >= FETCH_CORRECTION_MAX_ATTEMPTS
}

// fetchSearchSweepAngle returns which way fetchSearchForBall should steer at
// elapsed time into the search: alternates left/right every
// FETCH_SEARCH_SWEEP_INTERVAL so the sweep covers a wider field of view than
// circling one direction would.
func fetchSearchSweepAngle(elapsed time.Duration) servoDegrees {
	if (elapsed/FETCH_SEARCH_SWEEP_INTERVAL)%2 == 0 {
		return FETCH_SEARCH_STEERING_ANGLE_LEFT
	}
	return FETCH_SEARCH_STEERING_ANGLE_RIGHT
}

// fetchSearchSweepDirectionLabel returns a human-readable label for angle,
// for logging which way fetchSearchForBall is currently sweeping.
func fetchSearchSweepDirectionLabel(angle servoDegrees) string {
	if angle == FETCH_SEARCH_STEERING_ANGLE_LEFT {
		return "SWEEPING LEFT!"
	}
	return "SWEEPING RIGHT!"
}

// fetchStopArea returns the detection-area threshold at which to consider the
// ball close enough to halt/correct. Once at least one correction attempt
// has happened for the current lock, the resumed leg has less coast to close
// the final gap, so it uses FETCH_STOP_AREA_RETRY (later/closer) instead of
// the normal FETCH_STOP_AREA.
func fetchStopArea(correctionAttempts int) visionPixels {
	if correctionAttempts > 0 {
		return FETCH_STOP_AREA_RETRY
	}
	return FETCH_STOP_AREA
}

// fetchIsAcquireConfirmed reports whether a continuous ML detection streak
// that began at streakStart has lasted long enough (as of now) to treat as a
// confirmed lock-on rather than a one-off false positive. A zero streakStart
// means no streak is in progress.
func fetchIsAcquireConfirmed(streakStart, now time.Time) bool {
	if streakStart.IsZero() {
		return false
	}
	return now.Sub(streakStart) >= FETCH_ACQUIRE_DEBOUNCE_DURATION
}

// fetchDebounceAcquire tracks h.fetchAcquireStreakStart across ticks and
// reports whether best should be treated as a confirmed lock-on. best == nil
// (detection dropped) resets the streak. Shared by fetchMLAcquire and
// fetchSearchForBall so either path contributes to — and benefits from — the
// same continuous-detection requirement.
func (h *hawkeye) fetchDebounceAcquire(best *visionDetection) bool {
	if best == nil {
		h.fetchAcquireStreakStart = time.Time{}
		return false
	}

	now := time.Now()
	if h.fetchAcquireStreakStart.IsZero() {
		h.fetchAcquireStreakStart = now
	}
	if !fetchIsAcquireConfirmed(h.fetchAcquireStreakStart, now) {
		return false
	}

	h.fetchAcquireStreakStart = time.Time{}
	return true
}

// fetchMirroredSteeringAngle computes the steering angle to command while
// reversing during a correction attempt. Uses the same proportional gain as
// steeringTick's forward-driving PD controller, but mirrored around
// STEERING_NEUTRAL: reversing flips which rotational direction a given wheel
// angle swings the nose (same turning-radius geometry, flipped velocity
// sign), so a positive pixel offset needs the opposite sign of correction
// compared to forward driving. Clamped to the servo's physical range.
func fetchMirroredSteeringAngle(centerX visionPixels) servoDegrees {
	output := STEERING_KP * fetchCenterOffset(centerX)
	raw := float64(STEERING_NEUTRAL) + output
	clamped := math.Max(float64(STEERING_MAX_RIGHT), math.Min(float64(STEERING_MAX_LEFT), raw))
	return servoDegrees(clamped + 0.5)
}
