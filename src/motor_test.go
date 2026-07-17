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
func TestMotorArmReverseSequenceByPower(t *testing.T) {
	tests := []struct {
		name          string
		power         float64
		prevDirection driveDirection
		wantAngle     servoDegrees
		wantArm       bool
	}{
		{
			name:          "max power reverse after forward: arms first, then full reverse",
			power:         10,
			prevDirection: MOTOR_DRIVE_DIRECTION_FORWARD,
			wantAngle:     MOTOR_REVERSE_HIGH,
			wantArm:       true,
		},
		{
			name:          "normal power reverse after forward: still arms first",
			power:         5,
			prevDirection: MOTOR_DRIVE_DIRECTION_FORWARD,
			wantAngle:     powerToAngle(5, MOTOR_REVERSE_LOW, MOTOR_REVERSE_HIGH),
			wantArm:       true,
		},
		{
			name:          "max power reverse after neutral: no arming needed",
			power:         10,
			prevDirection: MOTOR_DRIVE_DIRECTION_NEUTRAL,
			wantAngle:     MOTOR_REVERSE_HIGH,
			wantArm:       false,
		},
		{
			name:          "normal power reverse after reverse: no arming needed",
			power:         5,
			prevDirection: MOTOR_DRIVE_DIRECTION_REVERSE,
			wantAngle:     powerToAngle(5, MOTOR_REVERSE_LOW, MOTOR_REVERSE_HIGH),
			wantArm:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAngle := powerToAngle(tt.power, MOTOR_REVERSE_LOW, MOTOR_REVERSE_HIGH)
			if gotAngle != tt.wantAngle {
				t.Errorf("powerToAngle(%v, REVERSE_LOW, REVERSE_HIGH) = %d, want %d", tt.power, gotAngle, tt.wantAngle)
			}

			gotArm := motorNeedsArmReverse(string(MOTOR_DRIVE_DIRECTION_REVERSE), tt.prevDirection)
			if gotArm != tt.wantArm {
				t.Errorf("motorNeedsArmReverse(reverse, %q) = %v, want %v", tt.prevDirection, gotArm, tt.wantArm)
			}
		})
	}
}

func TestMotorNeedsArmReverse(t *testing.T) {
	tests := []struct {
		name               string
		requestedDirection string
		prevDirection      driveDirection
		want               bool
	}{
		{"reverse request after forward needs arming", string(MOTOR_DRIVE_DIRECTION_REVERSE), MOTOR_DRIVE_DIRECTION_FORWARD, true},
		{"reverse request after neutral needs no arming", string(MOTOR_DRIVE_DIRECTION_REVERSE), MOTOR_DRIVE_DIRECTION_NEUTRAL, false},
		{"reverse request after reverse needs no arming", string(MOTOR_DRIVE_DIRECTION_REVERSE), MOTOR_DRIVE_DIRECTION_REVERSE, false},
		{"forward request after forward never needs arming", string(MOTOR_DRIVE_DIRECTION_FORWARD), MOTOR_DRIVE_DIRECTION_FORWARD, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := motorNeedsArmReverse(tt.requestedDirection, tt.prevDirection); got != tt.want {
				t.Errorf("motorNeedsArmReverse(%q, %q) = %v, want %v",
					tt.requestedDirection, tt.prevDirection, got, tt.want)
			}
		})
	}
}
