package main

import (
	"image"
	"testing"

	"go.viam.com/rdk/vision/objectdetection"
)

// det builds a detection with the given bounding box, score, and label.
func det(x0, y0, x1, y1 int, score float64, label string) objectdetection.Detection {
	return objectdetection.NewDetectionWithoutImgBounds(image.Rect(x0, y0, x1, y1), score, label)
}

func TestComputeLargestDetection(t *testing.T) {
	tests := []struct {
		name        string
		detections  []objectdetection.Detection
		wantNil     bool
		wantArea    visionPixels
		wantCenterX visionPixels
	}{
		{
			name:       "no detections returns nil",
			detections: nil,
			wantNil:    true,
		},
		{
			name:       "empty slice returns nil",
			detections: []objectdetection.Detection{},
			wantNil:    true,
		},
		{
			name:        "single detection returns its area and center",
			detections:  []objectdetection.Detection{det(100, 50, 300, 250, 0.9, "tennis ball")},
			wantArea:    40000, // 200 * 200
			wantCenterX: 200,   // (100 + 300) / 2
		},
		{
			name: "picks the largest detection by area",
			detections: []objectdetection.Detection{
				det(0, 0, 10, 10, 0.9, "tennis ball"),      // area 100
				det(100, 50, 300, 250, 0.9, "tennis ball"), // area 40000
				det(0, 0, 50, 50, 0.9, "tennis ball"),      // area 2500
			},
			wantArea:    40000,
			wantCenterX: 200,
		},
		{
			name: "filters non-ball labels, picks largest ball detection",
			detections: []objectdetection.Detection{
				det(0, 0, 200, 200, 0.9, "chair"),            // large, non-ball label → filtered
				det(300, 300, 350, 350, 0.99, "tennis ball"), // small, ball label → wins
			},
			wantArea:    2500, // 50 * 50
			wantCenterX: 325,  // (300 + 350) / 2
		},
		{
			name: "accepts numeric label 0 (Viam app bug workaround)",
			detections: []objectdetection.Detection{
				det(100, 50, 300, 250, 0.9, "0"),
			},
			wantArea:    40000,
			wantCenterX: 200,
		},
		{
			name: "filters low-confidence ball detection",
			detections: []objectdetection.Detection{
				det(0, 0, 200, 200, 0.3, "tennis ball"),
			},
			wantNil: true,
		},
		{
			name: "picks largest ball above confidence threshold, ignores low-confidence ball",
			detections: []objectdetection.Detection{
				det(0, 0, 10, 10, 0.9, "tennis ball"),      // area 100
				det(100, 50, 300, 250, 0.8, "tennis ball"), // area 40000 → wins
				det(0, 0, 400, 400, 0.3, "tennis ball"),    // area 160000, low confidence → filtered
			},
			wantArea:    40000,
			wantCenterX: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeLargestDetection(tt.detections)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatalf("expected a detection (area=%d centerX=%d), got nil", tt.wantArea, tt.wantCenterX)
			}
			if got.area != tt.wantArea {
				t.Errorf("area = %d, want %d", got.area, tt.wantArea)
			}
			if got.centerX != tt.wantCenterX {
				t.Errorf("centerX = %d, want %d", got.centerX, tt.wantCenterX)
			}
		})
	}
}

// nilBoxDetection is a Detection with no bounding box, used to verify
// computeLargestDetection skips detections that have none.
type nilBoxDetection struct{}

func (nilBoxDetection) BoundingBox() *image.Rectangle    { return nil }
func (nilBoxDetection) NormalizedBoundingBox() []float64 { return nil }
func (nilBoxDetection) Score() float64                   { return 1.0 }
func (nilBoxDetection) Label() string                    { return "no-box" }

func TestComputeLargestDetection_SkipsNilBoundingBox(t *testing.T) {
	detections := []objectdetection.Detection{
		nilBoxDetection{},
		det(100, 50, 300, 250, 0.9, "tennis ball"),
		nilBoxDetection{},
	}

	got := computeLargestDetection(detections)
	if got == nil {
		t.Fatal("expected the detection with a bounding box, got nil")
	}
	if got.area != 40000 || got.centerX != 200 {
		t.Errorf("got area=%d centerX=%d, want area=40000 centerX=200", got.area, got.centerX)
	}
}

func TestComputeLargestDetection_AllNilBoundingBoxes(t *testing.T) {
	got := computeLargestDetection([]objectdetection.Detection{nilBoxDetection{}, nilBoxDetection{}})
	if got != nil {
		t.Fatalf("expected nil when no detection has a bounding box, got %+v", got)
	}
}

func TestIsBallDetection(t *testing.T) {
	tests := []struct {
		name string
		d    objectdetection.Detection
		want bool
	}{
		{"tennis ball label above threshold", det(0, 0, 1, 1, 0.9, "tennis ball"), true},
		{"numeric label 0 above threshold", det(0, 0, 1, 1, 0.9, "0"), true},
		{"tennis ball label at threshold", det(0, 0, 1, 1, 0.5, "tennis ball"), true},
		{"tennis ball label below threshold", det(0, 0, 1, 1, 0.49, "tennis ball"), false},
		{"wrong label high score", det(0, 0, 1, 1, 0.99, "chair"), false},
		{"wrong label low score", det(0, 0, 1, 1, 0.1, "chair"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBallDetection(tt.d); got != tt.want {
				t.Errorf("isBallDetection() = %v, want %v", got, tt.want)
			}
		})
	}
}
