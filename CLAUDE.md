# Hawkeye

Viam module (Go) that runs on a Waveshare PiRacer Pro RC car and makes it follow tennis balls. The module registers a single `rdk:service:generic` model (`viam:tennis:hawkeye`) and is driven via `DoCommand`.

## Layout

- [src/](src/) — all business logic. The Go binary's `main` package.
- [util/](util/) — reusable utilities with no project-specific knowledge (`Routine`, `ThrottledLogger`, reflection helpers). Anything imported as `github.com/viam-labs/hawkeye/util` belongs here.
- [scripts/](scripts/) — Python scripts copied onto the PiRacer host for low-level hardware testing without Viam in the loop. Not built or imported by Go.
- [Makefile](Makefile) — build targets: `build-local`, `build-module` (tar for registry upload), `build-docker` (cross-compile for `linux/arm64`), `deploy-remote` (scp to `piracer@piracer.local`).
- [meta.json](meta.json) — Viam module manifest; entrypoint is `bin/run`, `setup.sh` is the first-run hook.

## Routines

A **routine** is the unit of distinct behavior in this module. Routines usually map 1:1 to a hardware component (camera → vision, steering servo → steering, drive servo → motor, OLED → screen, INA219 power monitor → battery). Each routine owns:

- A tick function on `*hawkeye` (e.g. [`visionTick`](src/vision.go), [`steeringTick`](src/steering.go), [`motorTick`](src/motor.go), [`screenTick`](src/screen.go), [`batteryTick`](src/battery.go)) run periodically by [`util.Routine`](util/routine.go).
- `handleStart<Name>`, `handleStop<Name>`, `handleTest<Name>` handlers, wired up in [src/do_command.go](src/do_command.go) under the `start`/`stop`/`test` dispatchers.
- Typed arg structs (`argsStart<Name>`, `argsStop<Name>`, `argsTest<Name>`) in [src/command_args.go](src/command_args.go), each implementing `commandArgs.validateArgs()`.
- A block of `<NAME>_*` constants in [src/constants.go](src/constants.go), starting with `<NAME>_ROUTINE_NAME` and `<NAME>_TICK_RATE`.

Routines **communicate only through shared state on the `hawkeye` struct** — never by calling each other's tick functions directly. The vision routine writes `visionLastDetection` (an `atomic.Pointer`); the steering and motor routines read it on their own tick. This keeps each routine independently startable, stoppable, and testable.

## Naming convention (load-bearing)

Every routine-specific identifier in [src/](src/) **must start with or contain the routine name** (`vision`, `steering`, `motor`, `screen`, `battery`). This is what lets you grep a routine and see all of its surface area at once.

- `hawkeye` struct fields: **must start** with the routine name — `visionViam`, `visionRoutine`, `visionLogger`, `visionThrottledLogger`, `visionLastDetection`, `steeringServoViam`, `steeringLastAngle`, `motorMutex`, `motorLastDriveDirection`, `batteryLastReading`, etc. See [src/init.go:39](src/init.go#L39).
- Handlers: `handleStart<Routine>`, `handleStop<Routine>`, `handleTest<Routine>`.
- Tick functions: `<routine>Tick` (e.g. `visionTick`).
- Arg structs: `argsStart<Routine>`, `argsStop<Routine>`, `argsTest<Routine>`.
- Constants: `<ROUTINE>_<THING>` in SCREAMING_SNAKE (`VISION_TICK_RATE`, `STEERING_MAX_LEFT`, `MOTOR_NEUTRAL`, `SCREEN_WIDTH`).
- Domain types are shared across routines and named after the unit, not the routine: `visionPixels`, `servoDegrees`, `driveDirection`.

When adding a new routine, follow this same pattern end to end (struct fields, handlers, args, constants, dispatcher cases in [src/do_command.go](src/do_command.go)).

## DoCommand shape

```
{ "command": "start" | "stop" | "test", "routines": { "<name>": { <args> } } }
```

`start`/`stop` can address multiple routines in one call (best-effort, deterministic order). `test` requires exactly one routine and is for ad-hoc hardware exercises — handlers run synchronously and may block.

## Conventions

- Tick functions log spammy per-tick output through the routine's `ThrottledLogger`; one-shot lifecycle events go through the plain `<routine>Logger`.
- `context.Canceled` inside a tick is a clean shutdown signal — log and return, don't wrap.
- Use `github.com/pkg/errors` (`errors.Wrap`/`Wrapf`/`Errorf`) for new errors; combine multiple with `go.uber.org/multierr`.
- Decode `DoCommand` inputs with `mapstructure`; let `runHandler` handle decode + `validateArgs` so handlers receive a typed, validated struct.
- `Reconfigure` calls `Close` first, then re-resolves dependencies — keep it idempotent and don't leak running routines across reconfigures.
- Constants are empirical (servo angles, tick rates, drive timings). Don't tune them blindly — they're calibrated to this specific hardware.

## Build & deploy

- Local laptop dev loop: `make deploy-remote` cross-compiles in Docker for `linux/arm64`, stops `viam-agent` on the PiRacer, scps the binary to `/home/piracer/bin/run`, and restarts. See the comment block in [Makefile](Makefile) for the matching robot-config snippet.
- Registry path: `viam module build start` (cloud) or `viam module upload --version <v> --platform linux/arm64 .` (local-built).
- Module is ARM64-only; do not add x86-only deps.
