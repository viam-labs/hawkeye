# Hunt Routine Design

**Date:** 2026-06-25
**Status:** Approved

## Summary

Add a `hunt` meta-routine that starts and stops the vision, steering, and motor routines as a single coordinated unit. The robot will detect a tennis ball with the ML vision model, steer toward it, decelerate as the ball grows larger in frame, and halt when the ball is close enough (area exceeds `VISION_MAX_DETECTION_AREA`). No new tick logic is needed — the existing routines already implement this behavior.

## Architecture

`hunt` is a virtual meta-routine: it has start/stop handlers but no tick function and no struct fields on `hawkeye`. It lives in `src/hunt.go` alongside the existing per-component files and follows the same naming conventions.

```
start hunt
  └─► handleStartVision   — ML detections → visionLastDetection (atomic)
  └─► handleStartSteering — reads centerX → steering servo angle
  └─► handleStartMotor    — reads area → deceleration curve → neutral at VISION_MAX_DETECTION_AREA

stop hunt (reverse order: stop driving before stop sensing)
  └─► handleStopMotor
  └─► handleStopSteering
  └─► handleStopVision
```

Each sub-routine ticks independently. Shared state flows through `visionLastDetection` exactly as it does today.

## Behavior

- Ball detected far away: motor drives forward at `MOTOR_FORWARD_HIGH`, steering centers on ball.
- Ball grows in frame: `convertAreaToMotorServoAngleAndSetLastDriveDirection` applies the power curve (`MOTOR_DECELERATION_FACTOR`) — motor angle walks from `MOTOR_FORWARD_HIGH` toward `MOTOR_FORWARD_LOW`.
- Ball fills frame (`area > VISION_MAX_DETECTION_AREA`): motor returns to `MOTOR_NEUTRAL`. Robot stops.
- Ball lost mid-hunt: motor and steering both fall back to neutral on their next tick (existing behavior).
- Vision model: ML all the way. Mac handles inference; close range means larger ball = higher model confidence. No color-detector fallback.

## DoCommand Shape

```json
{"command": "start", "routines": {"hunt": {}}}
{"command": "stop",  "routines": {"hunt": {}}}
```

No `argsStartHunt` fields and no `argsStopHunt` fields for now (empty validated structs). No `handleTestHunt` — each sub-routine has its own test handler.

## Error Handling

`handleStartHunt` starts sub-routines in order: vision → steering → motor. If any start fails, it stops already-started sub-routines and returns a combined error via `multierr`. Mirrors the rollback pattern in `Reconfigure`.

`handleStopHunt` stops in reverse order: motor → steering → vision. All stop errors are combined and returned.

## Files Changed

| File | Change |
|---|---|
| `src/hunt.go` (new) | `handleStartHunt(argsStartHunt)`, `handleStopHunt(argsStopHunt)` |
| `src/constants.go` | `HUNT_ROUTINE_NAME = "hunt"` |
| `src/command_args.go` | `argsStartHunt{}`, `argsStopHunt{}` (empty structs, `validateArgs` returns nil) |
| `src/do_command.go` | Wire `"hunt"` case into start/stop dispatchers |

## Future Work

- **Halt confirmation:** expose a `huntHalted` state (e.g. atomic bool) so the future scooper command can gate on "robot is stopped in front of ball."
- **Ball-lost recovery:** optionally spin in place to re-acquire the ball instead of falling back to neutral and waiting.
- **Scooper integration:** `grab` command already exists (`src/gripper.go`); hunt + grab will form the full collect sequence.
