# Hawkeye

Viam module (Go) that runs on a Waveshare PiRacer Pro RC car and makes it follow tennis balls. The module registers a single `rdk:service:generic` model (`viam:tennis:hawkeye`) and is driven via `DoCommand`.

## Layout

- [src/](src/) — all business logic. The Go binary's `main` package.
- [util/](util/) — reusable utilities with no project-specific knowledge (`Routine`, `ThrottledLogger`, reflection helpers). Anything imported as `github.com/viam-labs/hawkeye/util` belongs here.
- [3d/](3d/) — printable parts for the gripper mechanism. Not built or imported by Go.
- [Makefile](Makefile) — build targets: `build-local`, `build-module` (tar for registry upload), `build-docker` (cross-compile for `linux/arm64`), `deploy-remote` (scp to `piracer@piracer.local`).
- [meta.json](meta.json) — Viam module manifest; entrypoint is `bin/run`, `setup.sh` is the first-run hook.

## Routines

A **routine** is the unit of distinct behavior in this module. Routines map 1:1 to a hardware component (INA219 power monitor → battery, gripper servo → gripper, drive servo → motor, OLED → screen, steering servo → steering, camera → vision) except `fetch`, which owns no hardware and coordinates the rest. Each routine owns:

- A tick function on `*hawkeye` ([`batteryTick`](src/battery.go), [`fetchTick`](src/fetch.go), [`gripperTick`](src/gripper.go), [`motorTick`](src/motor.go), [`screenTick`](src/screen.go), [`steeringTick`](src/steering.go), [`visionTick`](src/vision.go)) run periodically by [`util.Routine`](util/routine.go).
- `handleStart<Name>`, `handleStop<Name>`, `handleTest<Name>` handlers, wired up in [src/do_command.go](src/do_command.go) under the `start`/`stop`/`test` dispatchers. A `handleTest<Name>` with nothing useful to exercise returns an error pointing at the test that covers it — see `handleTestFetch` and `handleTestSteering`.
- Typed arg structs (`argsStart<Name>`, `argsStop<Name>`, `argsTest<Name>`) in [src/command_args.go](src/command_args.go), each implementing `commandArgs.validateArgs()`.
- A block of `<NAME>_*` constants in [src/constants.go](src/constants.go), opening with `<NAME>_ROUTINE_NAME` and including a `<NAME>_TICK_RATE`. Blocks are ordered alphabetically by routine, so shared domain types (`servoDegrees`, `visionPixels`) sit inside whichever block declares them rather than at the top of the file.

Routines **communicate only through `atomic.Pointer` fields on the `hawkeye` struct** — never by calling each other's tick functions directly. This keeps each routine independently startable, stoppable, and testable, and lets partial combinations run: an absent writer reads as "nothing is asking for anything".

The flow is one-way through `fetch`. Vision publishes `visionLastDetection`; `fetch` is its **only** reader, and turns it into `steeringDesiredAngle`, `motorDesiredAngle`, `gripperDesiredAngle` and `screenDesiredImage`; those routines apply whatever they find on their own next tick and never look at a detection. Battery publishes `batteryLastReading`, which `screen` reads and outranks everything else with. `fetch` touches no servo itself, with one documented exception: the k-point turn drives them directly, since its legs are timed and there is nothing for vision to steer by.

Wherever routines or routine-specific things are listed — struct field groups, constants blocks, arg structs, dispatcher cases, `Close` teardown, doc tables — **order them alphabetically**: battery, fetch, gripper, motor, screen, steering, vision.

Two deliberate exceptions, both inside the fetch routine, where chronology beats the alphabet: the `fetch_<n>_<state>.go` files, and the `FETCH_*` constants within their block — those are grouped by the state that uses them, in state order. Don't alphabetize either.

### Fetch is a state machine

The fetch routine is the exception to one-routine-one-file. [src/fetch.go](src/fetch.go) holds the start/stop/test handlers, the [`fetchTick`](src/fetch.go) dispatch switch, and whatever is shared across states (`neutralizeSteeringAndMotorAngles`, `loadFetchState`/`storeFetchState`, the `convert*ServoAngle` mappings). States are **numbered by the order a fetch enters them**, and the number is carried by both the constant and the file name so the states sort chronologically rather than alphabetically: `FETCH_STATE_0_IDLE` … `FETCH_STATE_8_DONE`, and `fetch_<n>_<state>.go`.

Each `FETCH_STATE_<n>_*` gets its own file — [fetch_1_seek.go](src/fetch_1_seek.go), [fetch_2_evaluate_chase.go](src/fetch_2_evaluate_chase.go), [fetch_3_chase.go](src/fetch_3_chase.go), [fetch_4_grip.go](src/fetch_4_grip.go), [fetch_5_k_point_turn.go](src/fetch_5_k_point_turn.go), [fetch_6_evaluate_deliver.go](src/fetch_6_evaluate_deliver.go), [fetch_7_deliver.go](src/fetch_7_deliver.go), [fetch_8_done.go](src/fetch_8_done.go) — containing that state's `fetchTick<State>` plus the helpers only it uses. State 0 (idle) has no file: it has no tick and falls to the dispatch switch's `default`.

The number is positional bookkeeping only. It appears in the constant and the file name, and **nowhere else** — not in the `fetchState` string value (which stays `"seek"`, `"evaluate_chase"`, …, and is what reaches `DoCommand` results, logs, and the recorded-frame overlay), and not in the tick, `enterFetch<State>`, or `resetFetch<State>` function names.

State names are plain verbs, not gerunds (`seek`, `chase`, `grip`, `deliver` — never `seeking`, `chasing`), and the constant name, the `fetchState` string value, the file name, and the tick function all spell the same verb.

Log lines that name a state take it from the constant, never a literal: `Infof("%s: ...", FETCH_STATE_1_SEEK, ...)`, not `Infof("seek: ...")`. Renaming a state then can't leave its log prefix behind — which is how `"recalling: ..."` once outlived `FETCH_STATE_RECALLING`.

When adding a state: add the `FETCH_STATE_<n>_*` constant, a `fetch_<n>_<state>.go` with its tick, and a case in the dispatch switch. A state inserted mid-sequence renumbers everything after it, constants and file names together — the numbers are contiguous positions, not stable ids. Never name the file after a GOOS/GOARCH (`fetch_9_windows.go` would silently become a build-constrained file).

#### Fetch state bookkeeping

Each state's mutable fields live behind two helpers in that state's own file, so a state's entry conditions are readable without grepping its predecessors:

- `enterFetch<State>()` — initializes that state's per-entry fields and calls `storeFetchState`. Transitions call this instead of poking the next state's fields. A state with no per-entry fields has no helper, and a plain `storeFetchState` is the signal that there is nothing to set up.
- `resetFetch<State>()` — clears the same fields back to "has not started". [`resetFetchRun`](src/fetch.go) in fetch.go calls every one of these, and is the single reset that `handleStartFetch`, `handleStopFetch` and `Close` all share.

Two fields deliberately **survive** re-entry, and conflating them with the per-entry ones breaks the machine:

- `fetchSeekTotalDriveDuration` — the seek ↔ evaluate_chase drive budget. Carrying it across re-entries is what makes that cycle terminate. Dropped only by `resetFetchSeekCycle`, on leaving the cycle for good.
- `fetchKPointTurnLegIndex` — turn ↔ evaluate_deliver progress. Rewound only by `startFetchKPointTurn`; resuming after a look is a plain `storeFetchState`.

So: name a per-cycle reset after its cycle (`resetFetchSeekCycle`, `startFetchKPointTurn`), never fold it into an `enterFetch<State>`.

## Naming convention (load-bearing)

Every routine-specific identifier in [src/](src/) **must start with or contain the routine name** (`battery`, `fetch`, `gripper`, `motor`, `screen`, `steering`, `vision`). This is what lets you grep a routine and see all of its surface area at once.

- `hawkeye` struct fields: **must start** with the routine name — `batteryLastReading`, `motorLastDriveDirection`, `steeringServoViam`, `steeringLastAngle`, `visionViam`, `visionRoutine`, `visionLogger`, `visionThrottledLogger`, `visionLastDetection`, etc. See the `hawkeye` struct in [src/init.go](src/init.go), grouped one routine per block.
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

## Recording

Recording annotated frames belongs to the **vision** routine, not fetch: `argsStartVision.Record` is an absolute path that both selects the directory and turns recording on at all, and an empty path means don't record. It therefore starts and stops with the vision routine and outlives any single fetch. Frames carry the fetch's current state as an overlay, read from `fetchState` — safe only because that field is an atomic; most of the fetch's bookkeeping is not.

## Conventions

- Tick functions log spammy per-tick output through the routine's `ThrottledLogger`; one-shot lifecycle events go through the plain `<routine>Logger`.
- `context.Canceled` inside a tick is a clean shutdown signal — log and return, don't wrap.
- Use `github.com/pkg/errors` (`errors.Wrap`/`Wrapf`/`Errorf`) for new errors; combine multiple with `go.uber.org/multierr`.
- Decode `DoCommand` inputs with `mapstructure`; let `runHandler` handle decode + `validateArgs` so handlers receive a typed, validated struct.
- `Reconfigure` calls `Close` first, then re-resolves dependencies — keep it idempotent and don't leak running routines across reconfigures.
- Constants are empirical (servo angles, tick rates, drive timings). Don't tune them blindly — they're calibrated to this specific hardware.

### Comments

Comments are kept deliberately short. The bar is **what a reader cannot recover from the names and the code**:

- Every function gets a docstring covering its essence in a sentence or two. Don't restate the signature or narrate the body.
- Inline comments only where a block isn't self-documenting. No comment that names what the next line plainly does.
- Empirical and hardware rationale **stays** — why an exponent is 0.33, why the ESC needs a brake tap, why a filter runs after stitching rather than before. That is the part nobody can re-derive without the car.
- Prefer one comment covering a block of related constants over one per constant.
- Log messages are operational output, not documentation. Don't shorten a log line to save space, and don't add a comment that merely repeats one.

## Build & deploy

- Local laptop dev loop: `make deploy-remote` cross-compiles in Docker for `linux/arm64`, stops `viam-agent` on the PiRacer, scps the binary to `/home/piracer/bin/run`, and restarts. See the comment block in [Makefile](Makefile) for the matching robot-config snippet.
- Registry path: `viam module build start` (cloud) or `viam module upload --version <v> --platform linux/arm64 .` (local-built).
- Module is ARM64-only; do not add x86-only deps.
