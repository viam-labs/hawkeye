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
//	    "motor":    { <args> },
//	    "steering": { <args> },
//	    "vision":   { <args> },
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

// doDispatch invokes dispatchFunc for each routine in sorted-key order and
// aggregates the results. Best-effort: every routine is attempted even if an
// earlier one failed, and all failures come back joined.
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
	case BATTERY_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartBattery)
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartFetch)
	case GRIPPER_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartGripper)
	case MOTOR_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartMotor)
	case SCREEN_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartScreen)
	case STEERING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartSteering)
	case VISION_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStartVision)
	default:
		return nil, errors.Errorf("unknown routine %q for start", routineName)
	}
}

func (h *hawkeye) dispatchStop(routineName string, commandArgs any) (map[string]any, error) {
	switch routineName {
	case BATTERY_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopBattery)
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopFetch)
	case GRIPPER_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopGripper)
	case MOTOR_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopMotor)
	case SCREEN_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopScreen)
	case STEERING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopSteering)
	case VISION_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleStopVision)
	default:
		return nil, errors.Errorf("unknown routine %q for stop", routineName)
	}
}

func (h *hawkeye) dispatchTest(routineName string, commandArgs any) (map[string]any, error) {
	switch routineName {
	case BATTERY_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestBattery)
	case FETCH_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestFetch)
	case GRIPPER_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestGripper)
	case MOTOR_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestMotor)
	case SCREEN_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestScreen)
	case STEERING_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestSteering)
	case VISION_ROUTINE_NAME:
		return runHandler(commandArgs, h.handleTestVision)
	default:
		return nil, errors.Errorf("unknown routine %q for test", routineName)
	}
}

// runHandler decodes one command's args into a typed struct, validates it, and
// calls the handler. A comes from the handler's argument type and PA from the
// commandArgs constraint. validateArgs runs on a pointer so the defaults it sets
// reach the handler.
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
