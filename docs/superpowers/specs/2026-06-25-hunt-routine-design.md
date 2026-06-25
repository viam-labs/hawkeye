# Fetch Routine Design

**Date:** 2026-06-25
**Status:** Approved

## Summary

Add a `fetch` meta-routine that starts and stops the vision, steering, and motor routines as a single coordinated unit. The robot will detect a tennis ball with the ML vision model, steer toward it, decelerate as the ball grows larger in frame, and halt when the ball is close enough (area exceeds `VISION_MAX_DETECTION_AREA`). No new tick logic is needed — the existing routines already implement approach and deceleration. A brake pulse on the motor stop transition is added to counteract forward momentum.

## Architecture

`fetch` is a virtual meta-routine: it has start/stop handlers but no tick function and no struct fields on `hawkeye`. It lives in `src/fetch.go` alongside the existing per-component files and follows the same naming conventions.

```
start fetch
  └─► handleStartVision   — ML detections → visionLastDetection (atomic)
  └─► handleStartSteering — reads centerX → steering servo angle
  └─► handleStartMotor    — reads area → deceleration curve → brake pulse → neutral at VISION_MAX_DETECTION_AREA

stop fetch (reverse order: stop driving before stop sensing)
  └─► handleStopMotor
  └─► handleStopSteering
  └─► handleStopVision
```

Each sub-routine ticks independently. Shared state flows through `visionLastDetection` exactly as it does today.

## Behavior

- Ball detected far away: motor drives forward at `MOTOR_FORWARD_HIGH`, steering centers on ball.
- Ball grows in frame: `convertAreaToMotorServoAngleAndSetLastDriveDirection` applies the power curve (`MOTOR_DECELERATION_FACTOR`) — motor angle walks from `MOTOR_FORWARD_HIGH` toward `MOTOR_FORWARD_LOW`.
- Ball fills frame (`area > VISION_MAX_DETECTION_AREA`): motor applies a brake pulse (`MOTOR_REVERSE_HIGH` for `MOTOR_BRAKE_PULSE_DURATION`) to bleed forward momentum, then returns to `MOTOR_NEUTRAL`. Brake pulse fires only once per forward→stop transition (guarded by `motorLastDriveDirection`).
- Ball lost mid-fetch: motor and steering both fall back to neutral on their next tick (existing behavior). No brake pulse — robot was not in forward drive when ball was lost.
- Vision model: ML all the way. Mac handles inference; close range means larger ball = higher model confidence. No color-detector fallback.
- Fallback: if brake pulse causes overshoot in the wrong direction or feels unnatural, remove the pulse and tune `VISION_MAX_DETECTION_AREA` higher to stop earlier via momentum carry alone.

## DoCommand Shape

```json
{"command": "start", "routines": {"fetch": {}}}
{"command": "stop",  "routines": {"fetch": {}}}
```

No `argsStartFetch` fields and no `argsStopFetch` fields for now (empty validated structs). No `handleTestFetch` — each sub-routine has its own test handler.

## Error Handling

`handleStartFetch` starts sub-routines in order: vision → steering → motor. If any start fails, it stops already-started sub-routines and returns a combined error via `multierr`. Mirrors the rollback pattern in `Reconfigure`.

`handleStopFetch` stops in reverse order: motor → steering → vision. All stop errors are combined and returned.

## Brake Pulse Implementation

In `motorTick`, save `motorLastDriveDirection` before calling `convertAreaToMotorServoAngleAndSetLastDriveDirection` (which mutates it). If the resulting angle is `MOTOR_NEUTRAL` and the saved direction was `MOTOR_DRIVE_DIRECTION_FORWARD`, pulse `MOTOR_REVERSE_HIGH` for `MOTOR_BRAKE_PULSE_DURATION` before sending `MOTOR_NEUTRAL`. This reuses the same ESC brake signal that the existing `handleTestMotor` brake-tap uses.

```
prevDirection := h.motorLastDriveDirection
motorAngle := h.convertAreaToMotorServoAngleAndSetLastDriveDirection(lastDetection.area)

if motorAngle == MOTOR_NEUTRAL && prevDirection == MOTOR_DRIVE_DIRECTION_FORWARD {
    // brake pulse: MOTOR_REVERSE_HIGH → sleep MOTOR_BRAKE_PULSE_DURATION → MOTOR_NEUTRAL
}
```

`MOTOR_BRAKE_PULSE_DURATION` is empirical — start at 150ms and tune on the bench.

## Files Changed

| File | Change |
|---|---|
| `src/fetch.go` (new) | `handleStartFetch(argsStartFetch)`, `handleStopFetch(argsStopFetch)` |
| `src/constants.go` | `FETCH_ROUTINE_NAME = "fetch"`, `MOTOR_BRAKE_PULSE_DURATION = 150ms` |
| `src/command_args.go` | `argsStartFetch{}`, `argsStopFetch{}` (empty structs, `validateArgs` returns nil) |
| `src/do_command.go` | Wire `"fetch"` case into start/stop dispatchers |
| `src/motor.go` | Add brake pulse logic in `motorTick` (save prevDirection, pulse on forward→neutral transition) |

## Future Work

- **Halt confirmation:** expose a `fetchHalted` state (e.g. atomic bool) so the future scooper command can gate on "robot is stopped in front of ball."
- **Ball-lost recovery:** optionally spin in place to re-acquire the ball instead of falling back to neutral and waiting.
- **Scooper integration:** `grab` command already exists (`src/gripper.go`); fetch + grab will form the full collect sequence.
