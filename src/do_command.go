package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-viper/mapstructure/v2"
	"github.com/pkg/errors"
	"go.uber.org/multierr"
)

// DoCommand runs one or more hawkeye routines.
//
//	{
//	  "command":  "start" | "stop",
//	  "routines": {
//	    "vision":   { <args> },
//	    "steering": { <args> },
//	    "motor":    { <args> },
//	  }
//	}
//
// "test" requires exactly one routine:
//
//	{
//	  "command":  "test",
//	  "routines": { "vision": { <args> } }
//	}
func (h *hawkeye) DoCommand(_ context.Context, command map[string]any) (map[string]any, error) {
	var input struct {
		Command         string         `mapstructure:"command"`
		RoutinesAndArgs map[string]any `mapstructure:"routines"`
	}
	if err := mapstructure.Decode(command, &input); err != nil {
		return nil, errors.Wrapf(err, "error decoding command input")
	}

	switch input.Command {
	case "start":
		return doDispatch(input.RoutinesAndArgs, h.dispatchStart)
	case "stop":
		return doDispatch(input.RoutinesAndArgs, h.dispatchStop)
	case "test":
		if len(input.RoutinesAndArgs) != 1 {
			return nil, fmt.Errorf(`"test" requires exactly one routine, but got %d`, len(input.RoutinesAndArgs))
		}
		return doDispatch(input.RoutinesAndArgs, h.dispatchTest)
	default:
		return nil, errors.Errorf("unknown command %q", input.Command)
	}
}

// doDispatch iterates routines in a deterministic (sorted key) order,
// invokes dispatchFunc for each, and aggregates per-routine results
// and errors. Best-effort: every routine is attempted even if an earlier
// one failed. Returns a result map keyed by routine name and a joined error
// containing all per-routine failures.
func doDispatch(
	routineNameToArgs map[string]any,
	dispatchFunc func(routineName string, commandArgs any) (map[string]any, error),
) (map[string]any, error) {
	var (
		routineNames    []string
		dispatchResults = make(map[string]any)
		dispatchErrs    error
	)

	for routineName := range routineNameToArgs {
		routineNames = append(routineNames, routineName)
	}
	sort.Strings(routineNames)

	for _, routineName := range routineNames {
		result, err := dispatchFunc(routineName, routineNameToArgs[routineName])
		if err != nil {
			dispatchErrs = multierr.Combine(dispatchErrs, err)
			continue
		}
		dispatchResults[routineName] = result
	}

	return dispatchResults, dispatchErrs
}

func (h *hawkeye) dispatchStart(routineName string, commandArgs any) (map[string]any, error) {
	switch routineName {
	case VISION_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartVision)
	case STEERING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartSteering)
	case MOTOR_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartMotor)
	case SCREEN_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartScreen)
	case BATTERY_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartBattery)
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartFetch)
	default:
		return nil, errors.Errorf("unknown routine %q for start", routineName)
	}
}

func (h *hawkeye) dispatchStop(routineName string, commandArgs any) (map[string]any, error) {
	switch routineName {
	case VISION_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopVision)
	case STEERING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopSteering)
	case MOTOR_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopMotor)
	case SCREEN_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopScreen)
	case BATTERY_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopBattery)
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopFetch)
	default:
		return nil, errors.Errorf("unknown routine %q for stop", routineName)
	}
}

func (h *hawkeye) dispatchTest(routineName string, commandArgs any) (map[string]any, error) {
	switch routineName {
	case VISION_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestVision)
	case STEERING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestSteering)
	case MOTOR_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestMotor)
	case SCREEN_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestScreen)
	case BATTERY_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestBattery)
	case TRACKING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestTracking)
	case BRAKING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestBraking)
	default:
		return nil, errors.Errorf("unknown routine %q for test", routineName)
	}
}

// runHandler decodes a single command's args into a typed struct, validates it,
// and calls the start/stop/test handler. A is inferred from the handler's
// argument type; PA is then inferred as *A via the commandArgs constraint.
// validateArgs runs on &typedArgs so defaults it sets persist through to the
// handler.
func runHandler[A any, PA commandArgs[A]](
	args any,
	handler func(A) (map[string]any, error),
) (map[string]any, error) {
	var typedArgs A
	if err := mapstructure.Decode(args, &typedArgs); err != nil {
		return nil, err
	}
	if err := PA(&typedArgs).validateArgs(); err != nil {
		return nil, err
	}
	return handler(typedArgs)
}
