#!/usr/bin/env python3
"""Drive forward for 1 second via ESC on PCA9685 channel 1, then stop.

The car will physically roll. Put it on a block (wheels off the ground) or
give it clear floor space before running.
"""

import time
from smbus2 import SMBus

I2C_BUS = 1
PCA9685_ADDR = 0x40
MODE1 = 0x00
PRESCALE = 0xFE
LED0_ON_L = 0x06

# PiRacer wiring: ESC on ch1, steering servo on ch0.
THROTTLE_CHANNEL = 1
PWM_FREQ_HZ = 60

# From donkeycar cfg_basic defaults, neutral=370, full-forward=500,
# full-reverse=220. We use gentler values so the car creeps instead of
# launching in either direction.
NEUTRAL_PWM = 370
FORWARD_PWM = 410   # +40 from neutral → ~31% of the 130-tick forward range
REVERSE_PWM = 330   # -40 from neutral → ~27% of the 150-tick reverse range

ARM_SECONDS = 1.0    # hold neutral so ESC accepts throttle commands
DRIVE_SECONDS = 1.0


def set_pwm_freq(bus, hz):
    prescale = round(25_000_000 / (4096 * hz)) - 1
    old_mode = bus.read_byte_data(PCA9685_ADDR, MODE1)
    bus.write_byte_data(PCA9685_ADDR, MODE1, (old_mode & 0x7F) | 0x10)
    bus.write_byte_data(PCA9685_ADDR, PRESCALE, prescale)
    bus.write_byte_data(PCA9685_ADDR, MODE1, old_mode)
    time.sleep(0.005)
    bus.write_byte_data(PCA9685_ADDR, MODE1, old_mode | 0xA0)


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

        print(f"arming (neutral pulse={NEUTRAL_PWM}) for {ARM_SECONDS}s")
        set_pulse(bus, THROTTLE_CHANNEL, NEUTRAL_PWM)
        time.sleep(ARM_SECONDS)

        print(f"forward (pulse={FORWARD_PWM}) for {DRIVE_SECONDS}s")
        set_pulse(bus, THROTTLE_CHANNEL, FORWARD_PWM)
        time.sleep(DRIVE_SECONDS)

        print(f"neutral (pulse={NEUTRAL_PWM})")
        set_pulse(bus, THROTTLE_CHANNEL, NEUTRAL_PWM)
        time.sleep(0.3)

        # FBR (forward-brake-reverse) ESCs need a "double tap" to reverse:
        # the first below-neutral (reverse) pulse after forward is interpreted as
        # brake, and only the second (after another neutral) is accepted as reverse.
        print(f"brake pulse (pulse={REVERSE_PWM})")
        set_pulse(bus, THROTTLE_CHANNEL, REVERSE_PWM)
        time.sleep(0.1)

        print(f"neutral (pulse={NEUTRAL_PWM})")
        set_pulse(bus, THROTTLE_CHANNEL, NEUTRAL_PWM)
        time.sleep(0.3)

        print(f"reverse (pulse={REVERSE_PWM}) for {DRIVE_SECONDS}s")
        set_pulse(bus, THROTTLE_CHANNEL, REVERSE_PWM)
        time.sleep(DRIVE_SECONDS)

        print(f"stop (pulse={NEUTRAL_PWM})")
        set_pulse(bus, THROTTLE_CHANNEL, NEUTRAL_PWM)
        time.sleep(0.3)


if __name__ == "__main__":
    main()