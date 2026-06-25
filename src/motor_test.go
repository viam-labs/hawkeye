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
