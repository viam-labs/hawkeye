# Fetch Routine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `fetch` meta-routine that starts/stops vision+steering+motor as one unit, with a brake pulse in `motorTick` to bleed forward momentum on stop.

**Architecture:** `fetch` is a virtual meta-routine with no tick and no struct fields — `handleStartFetch`/`handleStopFetch` in `src/fetch.go` call the sub-routines' `Start`/`Stop` directly. `motorTick` gains a one-shot brake pulse (ESC `MOTOR_REVERSE_HIGH` for `MOTOR_BRAKE_PULSE_DURATION`) on the first forward→neutral transition. Dispatch wired in `do_command.go`.

**Tech Stack:** Go, `github.com/pkg/errors`, `go.uber.org/multierr`, Viam RDK servo API (`go.viam.com/rdk/components/servo`).

## Global Constraints

- All identifiers follow the naming convention in CLAUDE.md: struct fields start with routine name, constants are `SCREAMING_SNAKE`, handlers are `handle{Start,Stop}<RoutineName>`.
- New constant block for `FETCH_*` goes in `src/constants.go` alongside existing blocks.
- `MOTOR_BRAKE_PULSE_DURATION` is empirical — start at 150ms, tune on hardware.
- `fetch` has no tick function and no `hawkeye` struct fields.
- No `handleTestFetch` — each sub-routine has its own test handler.
- Use `github.com/pkg/errors` for new errors; `go.uber.org/multierr` for combining.
- Build command: `go build ./src/` from repo root to verify compilation.
- Test command: `go test ./src/` from repo root.

---

### Task 1: Add FETCH_ROUTINE_NAME and MOTOR_BRAKE_PULSE_DURATION constants

**Files:**
- Modify: `src/constants.go`

**Interfaces:**
- Produces: `FETCH_ROUTINE_NAME string`, `MOTOR_BRAKE_PULSE_DURATION time.Duration` — used by Tasks 3, 4, 5.

- [ ] **Step 1: Add the fetch constant block after the gripper block**

In `src/constants.go`, append after the `GRIPPER_*` block (line 124):

```go
const (
	FETCH_ROUTINE_NAME = "fetch"
)
```

- [ ] **Step 2: Add MOTOR_BRAKE_PULSE_DURATION to the motor block**

In `src/constants.go`, add inside the `MOTOR_*` block after `TEST_MOTOR_DRIVE_DURATION`:

```go
	// Duration of the ESC brake pulse applied when transitioning from forward to neutral.
	// A brief MOTOR_REVERSE_HIGH pulse bleeds momentum without entering reverse mode.
	// Empirical — tune on the bench; start at 150ms.
	MOTOR_BRAKE_PULSE_DURATION = 150 * time.Millisecond
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./src/
```

Expected: no output (clean build).

- [ ] **Step 4: Commit**

```bash
git add src/constants.go
git commit -m "feat(fetch): add FETCH_ROUTINE_NAME and MOTOR_BRAKE_PULSE_DURATION constants"
```

---

### Task 2: Add argsStartFetch and argsStopFetch

**Files:**
- Modify: `src/command_args.go`

**Interfaces:**
- Produces: `argsStartFetch`, `argsStopFetch` — consumed by Task 4 (`handleStartFetch`, `handleStopFetch`) and Task 5 (dispatcher).

- [ ] **Step 1: Add arg structs at the end of the Fetch section**

In `src/command_args.go`, append a `// Fetch` section after the `// Battery` block (after line 165):

```go
// Fetch

type argsStartFetch struct{}

func (*argsStartFetch) validateArgs() error { return nil }

type argsStopFetch struct{}

func (*argsStopFetch) validateArgs() error { return nil }
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./src/
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add src/command_args.go
git commit -m "feat(fetch): add argsStartFetch and argsStopFetch"
```

---

### Task 3: Brake pulse in motorTick

**Files:**
- Modify: `src/motor.go`
- Test: `src/motor_test.go` (new)

**Interfaces:**
- Consumes: `MOTOR_NEUTRAL`, `MOTOR_REVERSE_HIGH`, `MOTOR_BRAKE_PULSE_DURATION`, `MOTOR_DRIVE_DIRECTION_FORWARD` from `src/constants.go` (Task 1).
- Produces: `brakePulseNeeded(newAngle servoDegrees, prevDirection driveDirection) bool` — exported for tests, used by `motorTick`.

- [ ] **Step 1: Write the failing test for brakePulseNeeded**

Create `src/motor_test.go`:

```go
package main

import "testing"

func TestBrakePulseNeeded(t *testing.T) {
	tests := []struct {
		name          string
		newAngle      servoDegrees
		prevDirection driveDirection
		want          bool
	}{
		{
			name:          "forward to neutral triggers pulse",
			newAngle:      MOTOR_NEUTRAL,
			prevDirection: MOTOR_DRIVE_DIRECTION_FORWARD,
			want:          true,
		},
		{
			name:          "neutral to neutral no pulse",
			newAngle:      MOTOR_NEUTRAL,
			prevDirection: MOTOR_DRIVE_DIRECTION_NEUTRAL,
			want:          false,
		},
		{
			name:          "reverse to neutral no pulse",
			newAngle:      MOTOR_NEUTRAL,
			prevDirection: MOTOR_DRIVE_DIRECTION_REVERSE,
			want:          false,
		},
		{
			name:          "forward to non-neutral no pulse",
			newAngle:      MOTOR_FORWARD_LOW,
			prevDirection: MOTOR_DRIVE_DIRECTION_FORWARD,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := brakePulseNeeded(tt.newAngle, tt.prevDirection); got != tt.want {
				t.Errorf("brakePulseNeeded(%d, %q) = %v, want %v",
					tt.newAngle, tt.prevDirection, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./src/ -run TestBrakePulseNeeded -v
```

Expected: compile error — `undefined: brakePulseNeeded`.

- [ ] **Step 3: Add brakePulseNeeded function to motor.go**

Add this function at the bottom of `src/motor.go` (after `motorNeutral`):

```go
// brakePulseNeeded reports whether a brake pulse should fire before moving to
// neutral — only when leaving forward drive. The ESC interprets MOTOR_REVERSE_HIGH
// as a brake signal (not reverse) when transitioning from forward, so a brief pulse
// bleeds momentum without engaging the reverse arming sequence.
func brakePulseNeeded(newAngle servoDegrees, prevDirection driveDirection) bool {
	return newAngle == MOTOR_NEUTRAL && prevDirection == MOTOR_DRIVE_DIRECTION_FORWARD
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./src/ -run TestBrakePulseNeeded -v
```

Expected:
```
--- PASS: TestBrakePulseNeeded/forward_to_neutral_triggers_pulse
--- PASS: TestBrakePulseNeeded/neutral_to_neutral_no_pulse
--- PASS: TestBrakePulseNeeded/reverse_to_neutral_no_pulse
--- PASS: TestBrakePulseNeeded/forward_to_non-neutral_no_pulse
PASS
```

- [ ] **Step 5: Wire brakePulseNeeded into motorTick**

Replace the current `motorTick` function in `src/motor.go` (lines 29–67) with:

```go
// motorTick reads visionLastDetection and drives the motor servo to an angle derived
// from the detection's area, resetting to neutral when no detection is present.
// When transitioning from forward to neutral, a brief brake pulse is applied first
// to bleed forward momentum before coasting to a stop.
func (h *hawkeye) motorTick(ctx context.Context) {
	lastDetection := h.visionLastDetection.Load()
	if lastDetection == nil {
		if h.motorLastAngle == MOTOR_NEUTRAL {
			h.motorThrottledLogger.Info("found no vision detection to move steering servo; remaining at neutral")
			return
		}

		h.motorThrottledLogger.Infof("found no vision detection to move steering servo; resetting to neutral angle %d", MOTOR_NEUTRAL)
		err := h.motorServoViam.Move(ctx, uint32(MOTOR_NEUTRAL), nil)
		if err != nil {
			h.motorThrottledLogger.Warnf("error resetting motor servo to neutral angle %d: %v", MOTOR_NEUTRAL, err)
		}

		return
	}

	prevDirection := h.motorLastDriveDirection
	motorAngle := h.convertAreaToMotorServoAngleAndSetLastDriveDirection(lastDetection.area)

	if motorAngle == h.motorLastAngle {
		h.motorThrottledLogger.Infof("no change in motor servo angle %d; skipping move", motorAngle)
		return
	}

	if brakePulseNeeded(motorAngle, prevDirection) {
		h.motorThrottledLogger.Info("applying brake pulse before neutral")
		if err := h.motorServoViam.Move(ctx, uint32(MOTOR_REVERSE_HIGH), nil); err != nil {
			if errors.Is(err, context.Canceled) {
				h.motorLogger.Info("stopping due to context cancellation")
				return
			}
			h.motorThrottledLogger.Warnf("error applying brake pulse: %v", err)
		} else {
			time.Sleep(MOTOR_BRAKE_PULSE_DURATION)
		}
	}

	err := h.motorServoViam.Move(ctx, uint32(motorAngle), nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			h.motorLogger.Info("stopping due to context cancellation")
			return
		}

		h.motorThrottledLogger.Warnf("error powering motor servo with angle %d: %v", motorAngle, err)
		time.Sleep(10 * time.Millisecond)
		return
	}

	h.motorLastAngle = motorAngle
	h.motorThrottledLogger.Infof("powered motor servo with angle %d", motorAngle)
}
```

- [ ] **Step 6: Run all tests and verify build**

```bash
go test ./src/ -v
go build ./src/
```

Expected: all existing tests pass, clean build.

- [ ] **Step 7: Commit**

```bash
git add src/motor.go src/motor_test.go
git commit -m "feat(fetch): add brake pulse on forward-to-neutral transition in motorTick"
```

---

### Task 4: handleStartFetch and handleStopFetch

**Files:**
- Create: `src/fetch.go`

**Interfaces:**
- Consumes: `argsStartFetch`, `argsStopFetch` (Task 2); `FETCH_ROUTINE_NAME` (Task 1); `h.visionRoutine`, `h.steeringRoutine`, `h.motorRoutine` and their loggers/ticks/rates (already on `hawkeye` struct, no changes needed); `multierr`.
- Produces: `(h *hawkeye) handleStartFetch(argsStartFetch) (map[string]any, error)`, `(h *hawkeye) handleStopFetch(argsStopFetch) (map[string]any, error)` — consumed by Task 5 dispatcher.

- [ ] **Step 1: Create src/fetch.go**

```go
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
```

- [ ] **Step 2: Verify compilation**

```bash
go build ./src/
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add src/fetch.go
git commit -m "feat(fetch): add handleStartFetch and handleStopFetch"
```

---

### Task 5: Wire fetch into do_command.go dispatchers

**Files:**
- Modify: `src/do_command.go`

**Interfaces:**
- Consumes: `FETCH_ROUTINE_NAME` (Task 1); `handleStartFetch`, `handleStopFetch` (Task 4); `argsStartFetch`, `argsStopFetch` (Task 2, consumed implicitly via `runHandler` type inference).

- [ ] **Step 1: Add fetch case to dispatchStart**

In `src/do_command.go`, add to `dispatchStart` after the `BATTERY_ROUTINE_NAME` case (before the `default`):

```go
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartFetch)
```

- [ ] **Step 2: Add fetch case to dispatchStop**

In `src/do_command.go`, add to `dispatchStop` after the `BATTERY_ROUTINE_NAME` case (before the `default`):

```go
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopFetch)
```

- [ ] **Step 3: Verify compilation and all tests pass**

```bash
go build ./src/
go test ./src/ -v
```

Expected: clean build, all tests pass.

- [ ] **Step 4: Commit**

```bash
git add src/do_command.go
git commit -m "feat(fetch): wire fetch into start/stop dispatchers"
```

---

## Hardware Validation (on PiRacer)

After deploying with `make deploy-remote`:

1. **Start fetch** — place tennis ball ~1m in front of robot:
   ```json
   {"command": "start", "routines": {"fetch": {}}}
   ```
   Expected: robot steers toward ball, drives forward, decelerates as ball fills frame, halts with brake pulse.

2. **Brake pulse check** — watch for brief lurch backward at stop. If overshoot in wrong direction, increase `MOTOR_BRAKE_PULSE_DURATION`; if not stopping cleanly, decrease. Adjust `VISION_MAX_DETECTION_AREA` as secondary tuning knob.

3. **Ball lost mid-chase** — remove ball while robot is approaching:
   Expected: motor and steering return to neutral on next tick (no brake pulse — not in forward drive when detection dropped).

4. **Stop fetch**:
   ```json
   {"command": "stop", "routines": {"fetch": {}}}
   ```
   Expected: `{"fetch": {"status": "stopped"}}`.

5. **Double-start guard** — start fetch while already running:
   Expected: error `"vision is already running"`.
