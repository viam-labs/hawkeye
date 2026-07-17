package main

import (
	"testing"
	"time"
)

func TestFetchCenterOffset(t *testing.T) {
	tests := []struct {
		name    string
		centerX visionPixels
		want    float64
	}{
		{"exact center", 320, 0},
		{"right of center", 350, 30},
		{"left of center", 290, -30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchCenterOffset(tt.centerX); got != tt.want {
				t.Errorf("fetchCenterOffset(%d) = %v, want %v", tt.centerX, got, tt.want)
			}
		})
	}
}

func TestFetchIsCentered(t *testing.T) {
	tests := []struct {
		name    string
		centerX visionPixels
		want    bool
	}{
		{"exact center", 320, true},
		{"at right tolerance boundary", 350, true},
		{"just past right tolerance", 351, false},
		{"at left tolerance boundary", 290, true},
		{"just past left tolerance", 289, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchIsCentered(tt.centerX); got != tt.want {
				t.Errorf("fetchIsCentered(%d) = %v, want %v", tt.centerX, got, tt.want)
			}
		})
	}
}

func TestFetchMirroredSteeringAngle(t *testing.T) {
	tests := []struct {
		name    string
		centerX visionPixels
		want    servoDegrees
	}{
		{"exact center holds neutral", 320, STEERING_NEUTRAL},
		{"right of center steers toward max left", 400, 105},
		{"far right of center clamps to max left", 640, STEERING_MAX_LEFT},
		{"left of center steers toward max right", 240, 97},
		{"far left of center clamps to max right", 0, STEERING_MAX_RIGHT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchMirroredSteeringAngle(tt.centerX); got != tt.want {
				t.Errorf("fetchMirroredSteeringAngle(%d) = %d, want %d", tt.centerX, got, tt.want)
			}
		})
	}
}

func TestFetchStopArea(t *testing.T) {
	tests := []struct {
		name               string
		correctionAttempts int
		want               visionPixels
	}{
		{"no corrections yet: normal threshold", 0, FETCH_STOP_AREA},
		{"one correction attempt: retry threshold", 1, FETCH_STOP_AREA_RETRY},
		{"multiple correction attempts: retry threshold", 2, FETCH_STOP_AREA_RETRY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchStopArea(tt.correctionAttempts); got != tt.want {
				t.Errorf("fetchStopArea(%d) = %d, want %d", tt.correctionAttempts, got, tt.want)
			}
		})
	}
}

func TestFetchIsAcquireConfirmed(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		streakStart time.Time
		now         time.Time
		want        bool
	}{
		{"no streak yet", time.Time{}, base, false},
		{"just started, well short of debounce", base, base.Add(1 * time.Millisecond), false},
		{"just before debounce duration", base, base.Add(FETCH_ACQUIRE_DEBOUNCE_DURATION - 1), false},
		{"exactly at debounce duration", base, base.Add(FETCH_ACQUIRE_DEBOUNCE_DURATION), true},
		{"past debounce duration", base, base.Add(FETCH_ACQUIRE_DEBOUNCE_DURATION + 1*time.Second), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchIsAcquireConfirmed(tt.streakStart, tt.now); got != tt.want {
				t.Errorf("fetchIsAcquireConfirmed(%v, %v) = %v, want %v", tt.streakStart, tt.now, got, tt.want)
			}
		})
	}
}

func TestFetchSearchSweepAngle(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		want    servoDegrees
	}{
		{"start of first interval: left", 0, FETCH_SEARCH_STEERING_ANGLE_LEFT},
		{"mid first interval: left", FETCH_SEARCH_SWEEP_INTERVAL / 2, FETCH_SEARCH_STEERING_ANGLE_LEFT},
		{"just before switch: left", FETCH_SEARCH_SWEEP_INTERVAL - 1, FETCH_SEARCH_STEERING_ANGLE_LEFT},
		{"start of second interval: right", FETCH_SEARCH_SWEEP_INTERVAL, FETCH_SEARCH_STEERING_ANGLE_RIGHT},
		{"mid second interval: right", FETCH_SEARCH_SWEEP_INTERVAL + FETCH_SEARCH_SWEEP_INTERVAL/2, FETCH_SEARCH_STEERING_ANGLE_RIGHT},
		{"start of third interval: left again", 2 * FETCH_SEARCH_SWEEP_INTERVAL, FETCH_SEARCH_STEERING_ANGLE_LEFT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchSearchSweepAngle(tt.elapsed); got != tt.want {
				t.Errorf("fetchSearchSweepAngle(%v) = %d, want %d", tt.elapsed, got, tt.want)
			}
		})
	}
}

func TestFetchSearchSweepDirectionLabel(t *testing.T) {
	tests := []struct {
		name  string
		angle servoDegrees
		want  string
	}{
		{"left angle labeled left", FETCH_SEARCH_STEERING_ANGLE_LEFT, "SWEEPING LEFT!"},
		{"right angle labeled right", FETCH_SEARCH_STEERING_ANGLE_RIGHT, "SWEEPING RIGHT!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchSearchSweepDirectionLabel(tt.angle); got != tt.want {
				t.Errorf("fetchSearchSweepDirectionLabel(%d) = %q, want %q", tt.angle, got, tt.want)
			}
		})
	}
}

func TestFetchShouldHalt(t *testing.T) {
	tests := []struct {
		name     string
		centerX  visionPixels
		attempts int
		want     bool
	}{
		{"centered, no attempts yet", 320, 0, true},
		{"off-center, attempts remain", 400, 0, false},
		{"off-center, one attempt short of cap", 400, FETCH_CORRECTION_MAX_ATTEMPTS - 1, false},
		{"off-center, attempts exhausted", 400, FETCH_CORRECTION_MAX_ATTEMPTS, true},
		{"centered, attempts already used", 320, FETCH_CORRECTION_MAX_ATTEMPTS + 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fetchShouldHalt(tt.centerX, tt.attempts); got != tt.want {
				t.Errorf("fetchShouldHalt(%d, %d) = %v, want %v", tt.centerX, tt.attempts, got, tt.want)
			}
		})
	}
}
