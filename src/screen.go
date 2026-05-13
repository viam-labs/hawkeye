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

// screenTick re-renders the OLED. A battery reading outranks everything else;
// otherwise the screen shows screenDesiredImage, falling back to the Viam logo.
func (h *hawkeye) screenTick(_ context.Context) {
	var (
		batteryLastReading = h.batteryLastReading.Load()
		imgToRender        = image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))
		imgName            string
	)

	if batteryLastReading != nil {
		h.makeText(imgToRender, fmt.Sprintf("Battery: %.2fV %d%%", batteryLastReading.voltage, batteryLastReading.percent))
		imgName = "battery reading"
	} else {
		desiredImage := SCREEN_IMAGE_VIAM_LOGO
		if requestedImage := h.screenDesiredImage.Load(); requestedImage != nil {
			desiredImage = *requestedImage
		}
		imgName = string(desiredImage)

		switch desiredImage {
		case SCREEN_IMAGE_TENNIS_BALL_ROLLING:
			h.makeNextTennisBallFrame(imgToRender)
		case SCREEN_IMAGE_LOCK_FOUND:
			h.makeLockFound(imgToRender)
		case SCREEN_IMAGE_LOCK_LOST:
			h.makeLockLost(imgToRender)
		case SCREEN_IMAGE_CLAW:
			h.makeClaw(imgToRender)
		case SCREEN_IMAGE_CLAW_WITH_BALL:
			h.makeClawWithBall(imgToRender)
		case SCREEN_IMAGE_PERSON:
			h.makePerson(imgToRender)
		case SCREEN_IMAGE_EYE:
			h.makeEye(imgToRender)
		case SCREEN_IMAGE_K:
			h.makeK(imgToRender)
		case SCREEN_IMAGE_VIAM_LOGO:
			h.makeViamLogo(imgToRender)
		default:
			h.screenThrottledLogger.Warnf("unknown desired image %q; falling back to %s", desiredImage, SCREEN_IMAGE_VIAM_LOGO)
			h.makeViamLogo(imgToRender)
			imgName = string(SCREEN_IMAGE_VIAM_LOGO)
		}
	}

	if err := h.renderImage(imgToRender); err != nil {
		h.screenThrottledLogger.Warnf("error rendering %s to screen: %v", imgName, err)
		return
	}

	h.screenThrottledLogger.Infof("rendered %q on screen", imgName)
}

// makeNextTennisBallFrame draws the next frame of the rolling-ball animation,
// advancing the ball one slot and bouncing it back at either end.
func (h *hawkeye) makeNextTennisBallFrame(img *image1bit.VerticalLSB) {
	var (
		rotation = h.screenBallIndex * 45
		position = h.screenBallIndex + 1
	)
	h.makeTennisBall(img, rotation, position)

	h.screenBallIndex += h.screenBallDirection
	if h.screenBallIndex == 0 || h.screenBallIndex == 3 {
		h.screenBallDirection *= -1
	}
}

func (h *hawkeye) makeViamLogo(img *image1bit.VerticalLSB) {
	// Capsule strokes. V and A are pure chevrons (A has no crossbar, matching the
	// Viam style); M is vertical outer legs joined by an inner V; I is a bar.
	// Letters span y=4..28, laid out for ~2px gaps and ~20px side margins.
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
	// Outline plus two arc seams, each the portion of a smaller circle inside the
	// ball that meets the outline at two endpoints. The seam centers sit
	// symmetrically across the ball center on an axis that rotationDeg tilts
	// clockwise from vertical; endpoints land at ±45° from it.
	// position: 0 = centered, 1-4 = evenly spaced left to right, touching the
	// screen edges at 1 and 4.
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

// makeLockFound draws the padlock centered: the fetch routine is chasing a
// detection it can currently see.
func (h *hawkeye) makeLockFound(img *image1bit.VerticalLSB) {
	h.makeLock(img, SCREEN_WIDTH/2)
}

// makeLockLost draws the padlock left of center followed by a question mark: the
// chase is still on but its detection has gone missing. The two centers are
// picked so the pair sits centered as a group.
func (h *hawkeye) makeLockLost(img *image1bit.VerticalLSB) {
	const (
		lockCX         = 52
		questionMarkCX = 80
	)

	h.makeLock(img, lockCX)
	h.makeQuestionMark(img, questionMarkCX)
}

func (h *hawkeye) makeLock(img *image1bit.VerticalLSB, lockCX int) {
	// A solid rounded body with a keyhole cut out, under a shackle drawn as the
	// upper half of a ring centered on the body's top edge. The shackle is
	// narrower than the body so its ends meet it rather than overhang.
	const (
		bodyTop       = 13
		bodyBottom    = 29
		bodyHalfWidth = 11
		bodyRadius    = 3.0

		shackleOuterRadius = 8.0
		shackleInnerRadius = 5.0

		keyholeCY         = 19
		keyholeRadius     = 2.5
		keyholeStemHalfW  = 0
		keyholeStemBottom = 24
	)

	fLockCX := float64(lockCX)

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			fx, fy := float64(x), float64(y)

			// Shackle: the half of the ring that sits above the body.
			if y < bodyTop {
				distFromShackleCenter := math.Hypot(fx-fLockCX, fy-bodyTop)
				if distFromShackleCenter >= shackleInnerRadius && distFromShackleCenter <= shackleOuterRadius {
					img.SetBit(x, y, image1bit.On)
				}
				continue
			}

			// Body: a rounded rectangle, tested by measuring the distance to the
			// nearest point of the rectangle its corner circles are centered on.
			if y > bodyBottom || x < lockCX-bodyHalfWidth || x > lockCX+bodyHalfWidth {
				continue
			}

			var (
				cornerCX = min(max(fx, fLockCX-bodyHalfWidth+bodyRadius), fLockCX+bodyHalfWidth-bodyRadius)
				cornerCY = min(max(fy, bodyTop+bodyRadius), bodyBottom-bodyRadius)
			)
			if math.Hypot(fx-cornerCX, fy-cornerCY) > bodyRadius {
				continue
			}

			// Keyhole: a circle and the stem below it, both cut back out of the body.
			if math.Hypot(fx-fLockCX, fy-keyholeCY) <= keyholeRadius {
				continue
			}
			if x >= lockCX-keyholeStemHalfW && x <= lockCX+keyholeStemHalfW &&
				y >= keyholeCY && y <= keyholeStemBottom {
				continue
			}

			img.SetBit(x, y, image1bit.On)
		}
	}
}

func (h *hawkeye) makeQuestionMark(img *image1bit.VerticalLSB, questionMarkCX int) {
	// A ring with its lower-left quadrant left out, so it ends pointing straight
	// down where the stem picks up and carries on to the dot.
	const (
		hookCY         = 12
		hookRadius     = 6.0
		hookStrokeHalf = 1.7
		stemTop        = 18.0
		stemBottom     = 21.0
		stemStrokeHalf = 1.5
		dotCY          = 27.0
		dotRadius      = 2.0
	)

	fQuestionMarkCX := float64(questionMarkCX)

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			fx, fy := float64(x), float64(y)

			isInHookQuadrant := fy <= hookCY || fx >= fQuestionMarkCX
			if isInHookQuadrant &&
				math.Abs(math.Hypot(fx-fQuestionMarkCX, fy-hookCY)-hookRadius) <= hookStrokeHalf {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			if distToSegment(fx, fy, fQuestionMarkCX, stemTop, fQuestionMarkCX, stemBottom) <= stemStrokeHalf {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			if math.Hypot(fx-fQuestionMarkCX, fy-dotCY) <= dotRadius {
				img.SetBit(x, y, image1bit.On)
			}
		}
	}
}

// makeClaw draws the claw empty: closed on the ball, still sitting where it
// grabbed it.
func (h *hawkeye) makeClaw(img *image1bit.VerticalLSB) {
	h.drawClaw(img, false)
}

// makeClawWithBall draws the same claw carrying the ball between its arms.
func (h *hawkeye) makeClawWithBall(img *image1bit.VerticalLSB) {
	h.drawClaw(img, true)
}

func (h *hawkeye) drawClaw(img *image1bit.VerticalLSB, withBall bool) {
	// A claw seen head on, reaching up: a stem, the body block it carries, and two
	// arms angling out to an elbow and back in to the fingertips, with a pivot
	// punched out of the body. Strokes stay thin to read at 32px. A carried ball
	// sits in the opening between the arms, touching neither them nor the body.
	const (
		clawCX = SCREEN_WIDTH / 2

		stemStrokeHalf = 2.5
		bodyStrokeHalf = 4.0
		armStrokeHalf  = 2.0

		pivotCY     = 22.0
		pivotRadius = 2.0

		ballCY     = 10.0
		ballRadius = 6.0
	)

	type seg struct {
		x1, y1, x2, y2 float64
		strokeHalf     float64
	}
	segments := []seg{
		// Stem, and the body block the arms stand on.
		{clawCX, SCREEN_HEIGHT - 1, clawCX, SCREEN_HEIGHT - 2, stemStrokeHalf},
		{clawCX - 8, 22, clawCX + 8, 22, bodyStrokeHalf},
		// Left arm: out and up to the elbow, then back in to the fingertip.
		{clawCX - 6, 18, clawCX - 20, 10, armStrokeHalf},
		{clawCX - 20, 10, clawCX - 10, 3, armStrokeHalf},
		// Right arm, mirrored.
		{clawCX + 6, 18, clawCX + 20, 10, armStrokeHalf},
		{clawCX + 20, 10, clawCX + 10, 3, armStrokeHalf},
	}

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			fx, fy := float64(x), float64(y)

			// The pivot is a hole punched back out of the body.
			if math.Hypot(fx-clawCX, fy-pivotCY) <= pivotRadius {
				continue
			}

			if withBall && math.Hypot(fx-clawCX, fy-ballCY) <= ballRadius {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			for _, s := range segments {
				if distToSegment(fx, fy, s.x1, s.y1, s.x2, s.y2) <= s.strokeHalf {
					img.SetBit(x, y, image1bit.On)
					break
				}
			}
		}
	}
}

// makePerson draws a stick figure, for the delivery: the car has the ball and is
// driving it back to whoever threw it.
func (h *hawkeye) makePerson(img *image1bit.VerticalLSB) {
	// A filled head over five capsule strokes: spine, two arms angled down, two
	// legs.
	const (
		strokeHalf = 1.4

		personCX   = SCREEN_WIDTH / 2
		headCY     = 6.0
		headRadius = 4.0

		neckY = 11.0
		hipY  = 19.0
		footY = 28.0

		// Where the arms leave the spine, how far out they reach, and how far they
		// hang below where they started.
		armY     = 13.0
		armReach = 9.0
		armDrop  = 4.0

		legReach = 8.0
	)

	type seg struct{ x1, y1, x2, y2 float64 }
	segments := []seg{
		{personCX, neckY, personCX, hipY},
		{personCX, armY, personCX - armReach, armY + armDrop},
		{personCX, armY, personCX + armReach, armY + armDrop},
		{personCX, hipY, personCX - legReach, footY},
		{personCX, hipY, personCX + legReach, footY},
	}

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			fx, fy := float64(x), float64(y)

			if math.Hypot(fx-personCX, fy-headCY) <= headRadius {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			for _, s := range segments {
				if distToSegment(fx, fy, s.x1, s.y1, s.x2, s.y2) <= strokeHalf {
					img.SetBit(x, y, image1bit.On)
					break
				}
			}
		}
	}
}

// makeEye draws an open eye, for the sweep that opens a fetch: the car is out
// looking for something rather than acting on anything it has found.
func (h *hawkeye) makeEye(img *image1bit.VerticalLSB) {
	// An ellipse with an iris ring and filled pupil inside, wider than tall by
	// about the ratio a real eye is.
	const (
		strokeHalf = 1.1

		eyeCX      = SCREEN_WIDTH / 2
		eyeCY      = SCREEN_HEIGHT / 2
		eyeRadiusX = 34.0
		eyeRadiusY = 13.0

		irisRadius  = 9.0
		pupilRadius = 4.0
	)

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
			var (
				dx = float64(x) - eyeCX
				dy = float64(y) - eyeCY

				// 1 exactly on the ellipse, below it inside, above it outside.
				ellipse = (dx/eyeRadiusX)*(dx/eyeRadiusX) + (dy/eyeRadiusY)*(dy/eyeRadiusY)
			)

			// Dividing by the gradient turns this into a pixel distance, which keeps
			// the outline even instead of thinning where the curve runs steepest.
			gradient := 2 * math.Hypot(dx/(eyeRadiusX*eyeRadiusX), dy/(eyeRadiusY*eyeRadiusY))
			if math.Abs(ellipse-1)/gradient <= strokeHalf {
				img.SetBit(x, y, image1bit.On)
				continue
			}

			// The iris and pupil are clipped to the inside of the eye, so a stray
			// pixel of either can never sit outside the outline that contains them.
			if ellipse >= 1 {
				continue
			}

			distFromCenter := math.Hypot(dx, dy)
			if math.Abs(distFromCenter-irisRadius) <= strokeHalf || distFromCenter <= pupilRadius {
				img.SetBit(x, y, image1bit.On)
			}
		}
	}
}

// makeK draws a capital K, for the K-point turn the car is in the middle of —
// whatever K happens to be set to.
func (h *hawkeye) makeK(img *image1bit.VerticalLSB) {
	// Three capsule strokes: a vertical spine with two arms branching off its
	// middle to the upper and lower right. The spine sits left of center by half
	// the arms' reach, so the glyph comes out centered rather than the spine.
	const (
		strokeHalf = 2.5

		spineX = SCREEN_WIDTH/2 - 10.0
		armX   = SCREEN_WIDTH/2 + 10.0

		top        = 4.0
		bottom     = 27.0
		junctionCY = (top + bottom) / 2
	)

	type seg struct{ x1, y1, x2, y2 float64 }
	segments := []seg{
		{spineX, top, spineX, bottom},
		{spineX, junctionCY, armX, top},
		{spineX, junctionCY, armX, bottom},
	}

	for x := 0; x < SCREEN_WIDTH; x++ {
		for y := 0; y < SCREEN_HEIGHT; y++ {
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

func (h *hawkeye) handleTestScreen(args argsTestScreen) (map[string]any, error) {
	// Every other image is a single frame, and so is the tennis ball unless the
	// caller asks for the animation the ball is really made of.
	if args.Msg == string(SCREEN_IMAGE_TENNIS_BALL_ROLLING) && args.Animate {
		return h.handleTestScreenTennisBallAnimation()
	}

	img := image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))

	switch args.Msg {
	case "viam-logo":
		h.makeViamLogo(img)
	case "eye":
		h.makeEye(img)
	case "tennis-ball-rolling":
		h.makeTennisBall(img, args.Rotation, args.Position)
	case "lock-found":
		h.makeLockFound(img)
	case "lock-lost":
		h.makeLockLost(img)
	case "claw":
		h.makeClaw(img)
	case "claw-with-ball":
		h.makeClawWithBall(img)
	case "k":
		h.makeK(img)
	case "person":
		h.makePerson(img)
	default:
		h.makeText(img, args.Msg)
	}

	if err := h.renderImage(img); err != nil {
		return nil, errors.Wrapf(err, "error writing %q to screen", args.Msg)
	}

	h.screenLogger.Infof("drew %q on screen", args.Msg)

	return map[string]any{"status": "ok"}, nil
}

// handleTestScreenTennisBallAnimation rolls the ball across the screen for
// SCREEN_TEST_ANIMATION_DURATION at the rate the screen routine would, then
// leaves the last frame up. Otherwise the animation is only reachable by running
// a fetch as far as the evaluation.
//
// It animates from its own index rather than screenBallIndex, which belongs to
// the routine's goroutine; the math is the same, so only the starting position
// differs. Stop the screen routine first, or it draws over this between frames.
func (h *hawkeye) handleTestScreenTennisBallAnimation() (map[string]any, error) {
	h.screenLogger.Infof("rolling the tennis ball across the screen for %s", SCREEN_TEST_ANIMATION_DURATION)

	var (
		startedAt     = time.Now()
		ballIndex     = 0
		ballDirection = 1
		frames        = 0
	)

	for time.Since(startedAt) < SCREEN_TEST_ANIMATION_DURATION {
		img := image1bit.NewVerticalLSB(image.Rect(0, 0, SCREEN_WIDTH, SCREEN_HEIGHT))
		h.makeTennisBall(img, ballIndex*45, ballIndex+1)

		if err := h.renderImage(img); err != nil {
			return nil, errors.Wrap(err, "error rolling the tennis ball across the screen")
		}
		frames++

		// Bounced back at either end, the same way makeNextTennisBallFrame does.
		ballIndex += ballDirection
		if ballIndex == 0 || ballIndex == 3 {
			ballDirection *= -1
		}

		time.Sleep(SCREEN_TICK_RATE)
	}

	elapsed := time.Since(startedAt).Round(time.Millisecond)
	h.screenLogger.Infof("rolled the tennis ball for %d frames over %s", frames, elapsed)

	return map[string]any{
		"status":   "ok",
		"msg":      string(SCREEN_IMAGE_TENNIS_BALL_ROLLING),
		"animated": true,
		"frames":   frames,
		"elapsed":  elapsed.String(),
	}, nil
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

// renderImage draws img on the cached SSD1306, or errors if the device failed to
// initialize in newHawkeye — callers decide whether to log or surface it.
func (h *hawkeye) renderImage(img *image1bit.VerticalLSB) error {
	if h.screenDev == nil {
		return errors.New("SSD1306 not initialized")
	}
	return h.screenDev.Draw(h.screenDev.Bounds(), img, image.Point{})
}
