#!/usr/bin/env python3
"""Turn steering servo left 1s, right 1s, then center. Uses PCA9685 over I2C."""

import time
from smbus2 import SMBus

I2C_BUS = 1
PCA9685_ADDR = 0x40

MODE1 = 0x00
PRESCALE = 0xFE
LED0_ON_L = 0x06

# PiRacer wiring: steering servo on ch0, ESC on ch1 (opposite of donkeycar defaults).
STEERING_CHANNEL = 0
PWM_FREQ_HZ = 60
LEFT_PWM = 460
RIGHT_PWM = 290
NEUTRAL_PWM = (LEFT_PWM + RIGHT_PWM) // 2


def set_pwm_freq(bus, hz):
    prescale = round(25_000_000 / (4096 * hz)) - 1
    old_mode = bus.read_byte_data(PCA9685_ADDR, MODE1)
    bus.write_byte_data(PCA9685_ADDR, MODE1, (old_mode & 0x7F) | 0x10)  # sleep
    bus.write_byte_data(PCA9685_ADDR, PRESCALE, prescale)
    bus.write_byte_data(PCA9685_ADDR, MODE1, old_mode)
    time.sleep(0.005)
    bus.write_byte_data(PCA9685_ADDR, MODE1, old_mode | 0xA0)  # restart + auto-increment


def set_pulse(bus, channel, pulse_12bit):
    base = LED0_ON_L + 4 * channel
    bus.write_byte_data(PCA9685_ADDR, base + 0, 0)
    bus.write_byte_data(PCA9685_ADDR, base + 1, 0)
    bus.write_byte_data(PCA9685_ADDR, base + 2, pulse_12bit & 0xFF)
    bus.write_byte_data(PCA9685_ADDR, base + 3, (pulse_12bit >> 8) & 0x0F)


def main():
    with SMBus(I2C_BUS) as bus:
        bus.write_byte_data(PCA9685_ADDR, MODE1, 0x00)
        set_pwm_freq(bus, PWM_FREQ_HZ)

        print(f"left  (pulse={LEFT_PWM})")
        set_pulse(bus, STEERING_CHANNEL, LEFT_PWM)
        time.sleep(1.0)

        print(f"right (pulse={RIGHT_PWM})")
        set_pulse(bus, STEERING_CHANNEL, RIGHT_PWM)
        time.sleep(1.0)

        print(f"center (pulse={NEUTRAL_PWM})")
        set_pulse(bus, STEERING_CHANNEL, NEUTRAL_PWM)
        time.sleep(0.3)


if __name__ == "__main__":
    main()