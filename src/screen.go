package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/pkg/errors"

	"periph.io/x/devices/v3/ssd1306/image1bit"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func (h *hawkeye) handleStartScreen(_ argsStartScreen) (map[string]any, error) {
	err := h.screenRoutine.Start(h.screenLogger, h.screenTick, SCREEN_TICK_RATE)
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": "started"}, nil
}

func (h *hawkeye) handleStopScreen(_ argsStopScreen) (map[string]any, error) {
	err := h.screenRoutine.Stop()
	if err != nil {
		return nil, err
	}

	imageToRender := image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))
	h.makeText(imageToRender, "see ya!")
	err = h.renderImage(imageToRender)
	if err != nil {
		h.screenLogger.Warnf("error rendering goodbye message on screen: %v", err)
	}

	time.Sleep(1 * time.Second)

	imageToRender = image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))
	h.makeViamLogo(imageToRender)
	err = h.renderImage(imageToRender)
	if err != nil {
		h.screenLogger.Warnf("error resetting to screen to viam logo: %v", err)
	}

	return map[string]any{"status": "stopped"}, nil
}

// screenTick re-renders the OLED using fallthrough checks on the atomic pointers in hawkeye,
// falling back to the Viam logo when no pointers are set. Each branch only populates imgToRender;
// the rendering on the screen happens with renderImage.
func (h *hawkeye) screenTick(_ context.Context) {
	var (
		batteryLastReading  = h.batteryLastReading.Load()
		visionLastDetection = h.visionLastDetection.Load()
		imgToRender         = image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))
		imgName             string
	)

	if batteryLastReading != nil {
		h.makeText(imgToRender, fmt.Sprintf("Battery: %.2fV %d%%", batteryLastReading.voltage, batteryLastReading.percent))
		imgName = "battery reading"
	} else if visionLastDetection != nil {
		rotation := h.screenBallIndex * 45
		position := h.screenBallIndex + 1
		h.makeTennisBall(imgToRender, rotation, position)

		h.screenBallIndex += h.screenBallDirection
		if h.screenBallIndex == 0 || h.screenBallIndex == 3 {
			h.screenBallDirection *= -1
		}
		imgName = "ball animation"
	} else {
		h.makeViamLogo(imgToRender)
		imgName = "viam logo"
	}

	if err := h.renderImage(imgToRender); err != nil {
		h.screenThrottledLogger.Warnf("error rendering %s to screen: %v", imgName, err)
		return
	}

	h.screenThrottledLogger.Infof("rendered %q on screen", imgName)
}

func (h *hawkeye) makeViamLogo(img *image1bit.VerticalLSB) {
	// Viam wordmark drawn as thick line segments (capsule strokes). V and A
	// are pure chevrons (A has no crossbar, matching the Viam style); M is
	// vertical outer legs joined by an inner V; I is a solid vertical bar.
	// Letter heights span y=4..28; horizontal layout is sized to give ~2px
	// gaps between letters and symmetric ~20px side margins.
	const strokeHalf = 2.5

	type seg struct{ x1, y1, x2, y2 float64 }
	segments := []seg{
		// V: chevron pointing down
		{22, 4, 31, 28},
		{31, 28, 40, 4},
		// A: chevron pointing up, no crossbar
		{56, 28, 65, 4},
		{65, 4, 74, 28},
		// M: vertical legs joined by an inner V
		{81, 28, 81, 4},
		{81, 4, 93, 28},
		{93, 28, 105, 4},
		{105, 4, 105, 28},
	}

	const (
		iLeft, iRight = 45, 51
		iTop, iBottom = 2, 30
	)

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			if x >= iLeft && x <= iRight && y >= iTop && y <= iBottom {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			fx, fy := float64(x), float64(y)
			for _, s := range segments {
				if distToSegment(fx, fy, s.x1, s.y1, s.x2, s.y2) <= strokeHalf {
					img.SetBit(x, y, image1bit.On)
					break
				}
			}
		}
	}
}

func distToSegment(px, py, x1, y1, x2, y2 float64) float64 {
	dx := x2 - x1
	dy := y2 - y1
	lenSq := dx*dx + dy*dy
	if lenSq == 0 {
		return math.Hypot(px-x1, py-y1)
	}
	t := ((px-x1)*dx + (py-y1)*dy) / lenSq
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	cx := x1 + t*dx
	cy := y1 + t*dy
	return math.Hypot(px-cx, py-cy)
}

func (h *hawkeye) makeTennisBall(img *image1bit.VerticalLSB, rotationDeg, position int) {
	// Ball outline plus two arc seams. Each seam is the portion of a smaller
	// circle inside the ball that meets the outline at two endpoints. The
	// two seam centers sit symmetrically across the ball center on a "seam
	// axis"; rotationDeg tilts that axis clockwise from vertical. Each
	// seam's endpoints land at ±45° from the axis on the ball outline.
	// position picks a horizontal slot: 0 = centered, 1-4 = evenly spaced
	// left to right (balls touch the screen edges at positions 1 and 4).
	const (
		ballCY = SCREEN_HEIGHT / 2
		ballR  = SCREEN_HEIGHT/2 - 1

		seamR            = 11.20
		seamCenterOffset = 14.20
	)

	ballCX := SCREEN_WIDTH / 2
	switch position {
	case 1:
		ballCX = 15
	case 2:
		ballCX = 48
	case 3:
		ballCX = 80
	case 4:
		ballCX = 112
	}

	theta := float64(rotationDeg) * math.Pi / 180.0
	dirX := math.Sin(theta)
	dirY := -math.Cos(theta)

	seamCX1 := float64(ballCX) + seamCenterOffset*dirX
	seamCY1 := float64(ballCY) + seamCenterOffset*dirY
	seamCX2 := float64(ballCX) - seamCenterOffset*dirX
	seamCY2 := float64(ballCY) - seamCenterOffset*dirY

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			fx, fy := float64(x), float64(y)
			ballDx := fx - float64(ballCX)
			ballDy := fy - float64(ballCY)
			distFromCenter := math.Sqrt(ballDx*ballDx + ballDy*ballDy)

			if math.Abs(distFromCenter-ballR) < 0.7 {
				img.SetBit(x, y, image1bit.On)
				continue
			}
			if distFromCenter >= ballR {
				continue
			}

			d1x := fx - seamCX1
			d1y := fy - seamCY1
			if math.Abs(math.Sqrt(d1x*d1x+d1y*d1y)-seamR) < 0.7 {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			d2x := fx - seamCX2
			d2y := fy - seamCY2
			if math.Abs(math.Sqrt(d2x*d2x+d2y*d2y)-seamR) < 0.7 {
				img.SetBit(x, y, image1bit.On)
			}
		}
	}
}

func (h *hawkeye) handleTestScreen(args argsTestScreen) (map[string]any, error) {
	img := image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))

	switch args.Msg {
	case "viam":
		h.makeViamLogo(img)
	case "tennis":
		h.makeTennisBall(img, args.Rotation, args.Position)
	default:
		h.makeText(img, args.Msg)
	}

	if err := h.renderImage(img); err != nil {
		return nil, errors.Wrapf(err, "error writing %q to screen", args.Msg)
	}

	h.screenLogger.Infof("drew %q on screen", args.Msg)

	return map[string]any{"status": "ok"}, nil
}

func (h *hawkeye) makeText(img *image1bit.VerticalLSB, text string) {
	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(2, 14),
	}
	drawer.DrawString(text)
}

// renderImage render img on the cached SSD1306 OLED. The device is initialized
// once in newHawkeye; if that failed (h.screenDev == nil), this returns an
// error so callers can decide whether to log, throttle-log, or surface it.
func (h *hawkeye) renderImage(img *image1bit.VerticalLSB) error {
	if h.screenDev == nil {
		return errors.New("SSD1306 not initialized")
	}
	return h.screenDev.Draw(h.screenDev.Bounds(), img, image.Point{})
}
