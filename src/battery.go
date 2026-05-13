package main

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"periph.io/x/conn/v3/physic"
)

type batteryReading struct {
	voltage float64
	percent int
}

func (h *hawkeye) handleStartBattery(_ argsStartBattery) (map[string]any, error) {
	if err := h.batteryRoutine.Start(h.batteryLogger, h.batteryTick, BATTERY_TICK_RATE); err != nil {
		return nil, err
	}

	reading, err := h.readBattery()
	if err != nil {
		h.batteryLogger.Warnf("error getting first battery reading for response: %v", err)
		return map[string]any{"status": "started"}, nil
	}

	return map[string]any{
		"status":  "started",
		"voltage": fmt.Sprintf("%.2fV", reading.voltage),
		"percent": fmt.Sprintf("%d%%", reading.percent),
	}, nil
}

func (h *hawkeye) handleStopBattery(_ argsStopBattery) (map[string]any, error) {
	if err := h.batteryRoutine.Stop(); err != nil {
		return nil, err
	}
	return map[string]any{"status": "stopped"}, nil
}

// batteryTick reads the INA219 once and logs the voltage and percent.
func (h *hawkeye) batteryTick(_ context.Context) {
	reading, err := h.readBattery()
	if err != nil {
		h.batteryLogger.Warnf("error getting battery reading: %v", err)
		return
	}
	h.batteryLogger.Infof("battery: %.2fV (%d%%)", reading.voltage, reading.percent)
}

// handleTestBattery reads the INA219 once, logs it, and publishes it on
// batteryLastReading so a running screen routine can render it. After
// DurationSecs it clears batteryLastReading and returns.
func (h *hawkeye) handleTestBattery(args argsTestBattery) (map[string]any, error) {
	reading, err := h.readBattery()
	if err != nil {
		h.batteryLogger.Warnf("error getting battery reading: %v", err)
		return nil, err
	}
	h.batteryLogger.Infof("battery: %.2fV (%d%%)", reading.voltage, reading.percent)

	h.batteryLastReading.Store(reading)
	defer h.batteryLastReading.Store(nil)
	time.Sleep(time.Duration(args.DurationSecs) * time.Second)

	return map[string]any{
		"status":  "ok",
		"voltage": fmt.Sprintf("%.2fV", reading.voltage),
		"percent": fmt.Sprintf("%d%%", reading.percent),
	}, nil
}

// readBattery returns one reading from the INA219 device. The device is
// initialized once in newHawkeye; if that failed (h.batteryDev == nil),
// this logs a warning and returns an error so the rest of the module keeps
// running without a working power monitor.
func (h *hawkeye) readBattery() (*batteryReading, error) {
	if h.batteryDev == nil {
		return nil, errors.New("INA219 not initialized for battery reading")
	}

	pm, err := h.batteryDev.Sense()
	if err != nil {
		return nil, errors.Wrap(err, "error getting battery reading from INA219")
	}

	voltage := float64(pm.Voltage) / float64(physic.Volt)
	return &batteryReading{
		voltage: voltage,
		percent: batteryVoltageToPercent(voltage),
	}, nil
}

// batteryVoltageToPercent linearly maps a voltage in
// [BATTERY_MIN_VOLTAGE, BATTERY_MAX_VOLTAGE] to a percent in [0, 100].
// Out-of-range voltages clamp to the corresponding endpoint.
func batteryVoltageToPercent(voltage float64) int {
	if voltage <= BATTERY_MIN_VOLTAGE {
		return 0
	}
	if voltage >= BATTERY_MAX_VOLTAGE {
		return 100
	}
	pct := (voltage - BATTERY_MIN_VOLTAGE) / (BATTERY_MAX_VOLTAGE - BATTERY_MIN_VOLTAGE) * 100
	return int(pct + 0.5)
}
