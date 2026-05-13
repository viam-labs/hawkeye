package main

import "time"

const (
	BATTERY_ROUTINE_NAME = "battery"

	// The onboard INA219 sits on the default I2C bus.
	BATTERY_I2C_ADDRESS = 0x42

	BATTERY_TICK_RATE = 1 * time.Minute

	// The PiRacer's 2S2P 18650 pack: 8.4V freshly charged, 6.0V the practical
	// "low" before the BMS cuts off around 5.0V.
	BATTERY_MAX_VOLTAGE = 8.4
	BATTERY_MIN_VOLTAGE = 6.0
)

type fetchState string
type servoDegrees int

const (
	FETCH_ROUTINE_NAME = "fetch"

	// Half the VISION_TICK_RATE: no missed readings and no phase drift.
	FETCH_TICK_RATE = VISION_TICK_RATE / 2

	// In the order they are entered. States 1,2 and 5,6 run as a cycle
	// rather than in sequence, trading the car back and forth. The timings
	// below follow the same order, grouped by the state that uses them.
	FETCH_STATE_0_IDLE             fetchState = "idle"
	FETCH_STATE_1_SEEK             fetchState = "seek"
	FETCH_STATE_2_EVALUATE_CHASE   fetchState = "evaluate_chase"
	FETCH_STATE_3_CHASE            fetchState = "chase"
	FETCH_STATE_4_GRIP             fetchState = "grip"
	FETCH_STATE_5_K_POINT_TURN     fetchState = "k_point_turn"
	FETCH_STATE_6_EVALUATE_DELIVER fetchState = "evaluate_deliver"
	FETCH_STATE_7_DELIVER          fetchState = "deliver"
	FETCH_STATE_8_DONE             fetchState = "done"

	// How long a start command waits before anything moves, so whoever ran it can
	// put the ball down and get themselves into position first.
	FETCH_START_DELAY = 5 * time.Second

	// The seek/evaluate cycle that opens every fetch, since the ball is usually
	// nowhere in frame from where the car is parked. The wheels swap lock to lock
	// every FETCH_SEEK_WAG_INTERVAL so the sweep covers an arc, not a line.
	//
	// FETCH_SEEK_DURATION is the budget that ends the cycle. It counts driving
	// only, spent across every stint rather than one, and running it out ends the
	// fetch. FETCH_EVALUATE_CHASE_DURATION bounds the other half: how long one
	// evaluation may go on before the car sweeps on.
	//
	// FETCH_SEEK_MIN_DRIVE_DURATION is what makes the cycle terminate — every
	// stint covers ground before it may stop again, so each pass spends budget. It
	// also gets the car away from whatever it just rejected, usually still in
	// frame.
	//
	// Half power keeps the camera able to follow. FETCH_SEEK_COAST_DURATION is the
	// roll to a halt before the detection is judged, since the stability test
	// wants the car settled; a coast bleeds off slower than braking would, so
	// erring long costs time, not distance.
	FETCH_SEEK_DURATION                        = 5 * time.Second
	FETCH_EVALUATE_CHASE_DURATION              = 2 * time.Second
	FETCH_SEEK_MIN_DRIVE_DURATION              = 1 * time.Second
	FETCH_SEEK_WAG_INTERVAL                    = 500 * time.Millisecond
	FETCH_SEEK_COAST_DURATION                  = 1 * time.Second
	FETCH_SEEK_MOTOR_ANGLE        servoDegrees = MOTOR_FORWARD_LOW + 3

	// How long a detection must hold still before the evaluation commits to it,
	// and how far it may drift meanwhile as a fraction of its own size.
	FETCH_STABLE_DETECTION_DURATION  = 1 * time.Second
	FETCH_STABLE_DETECTION_MAX_DRIFT = 0.3

	// How long the chase brakes before handing off to the grip. Neutral alone lets
	// the car coast over the ball, so it brakes instead — which is how the ESC
	// reads a reverse angle out of a forward drive. The ESC latches that brake
	// until a different angle is commanded, so erring long costs time, not
	// distance.
	FETCH_CHASE_BRAKE_DURATION = 500 * time.Millisecond

	// How much longer the car creeps once the ball is in reach. Crossing the
	// thresholds only means the ball has reached the jaws; this seats it between
	// them rather than closing on its edge.
	FETCH_GRIPPER_REACH_DURATION = 100 * time.Millisecond

	// The turn that brings the car back around: K legs alternating a forward swing
	// on full left lock with a back up on full right lock, separated by coasts. An
	// odd K opens and closes on a forward leg, leaving the car pointing the way it
	// came.
	//
	// Driven open-loop on timings, so these are measured against the surface the
	// car turns on. K and the drive durations trade off for a given rotation: more
	// short legs turn in less room, fewer long ones in less time. Retune the
	// durations whenever K changes — the same legs repeated more times swing
	// further, not the same distance more tidily.
	//
	// Measured at K=3.
	FETCH_K_POINT_TURN_DRIVE_LEGS                    = 9
	FETCH_K_POINT_TURN_FORWARD_ANGLE    servoDegrees = MOTOR_FORWARD_LOW + 4
	FETCH_K_POINT_TURN_FORWARD_DURATION              = 500 * time.Millisecond
	FETCH_K_POINT_TURN_COAST_DURATION                = 100 * time.Millisecond
	FETCH_K_POINT_TURN_REVERSE_ANGLE    servoDegrees = MOTOR_REVERSE_HIGH
	FETCH_K_POINT_TURN_REVERSE_DURATION              = 500 * time.Millisecond

	// How long the car watches for the person between legs of the turn, so the turn
	// can stop as soon as they are in view rather than grinding through every leg.
	//
	// The look is free: it runs over the ESC arming the next reverse leg needs,
	// which the car spends standing still anyway. Setting it below
	// MOTOR_REVERSE_BRAKE_DURATION + MOTOR_REVERSE_NEUTRAL_DURATION buys nothing,
	// since the state waits the arming out regardless.
	FETCH_EVALUATE_DELIVER_DURATION = 1 * time.Second

	// Setting the ball down in front of the person: a brake to pull up, then a short
	// reverse to come out from around the ball rather than shunting it along. The
	// drive in is steered by vision, so it has no constant here.
	FETCH_DELIVER_BRAKE_DURATION   = 1 * time.Second
	FETCH_DELIVER_REVERSE_DURATION = 600 * time.Millisecond
)

const (
	GRIPPER_ROUTINE_NAME = "gripper"

	GRIPPER_TICK_RATE = 50 * time.Millisecond

	// Empirical measurements of the servo angle for each gripper position.
	// Neutral exists to avoid straining the servo during rest.
	GRIPPER_ANGLE_OPEN    servoDegrees = 130
	GRIPPER_ANGLE_CLOSED  servoDegrees = 80
	GRIPPER_ANGLE_NEUTRAL servoDegrees = 85
)

type driveDirection string

// The actions a handleTestMotor sequence step can ask for. Distinct from
// driveDirection: a direction is the motor's resulting state, whereas
// "brake" is an action that ends in the neutral state.
type motorAction string

const (
	MOTOR_ROUTINE_NAME = "motor"

	// Half the VISION_TICK_RATE: no missed readings and no phase drift.
	MOTOR_TICK_RATE = VISION_TICK_RATE / 2

	MOTOR_DRIVE_DIRECTION_FORWARD driveDirection = "forward"
	MOTOR_DRIVE_DIRECTION_REVERSE driveDirection = "reverse"
	MOTOR_DRIVE_DIRECTION_NEUTRAL driveDirection = "neutral"

	// Empirical measurements of the servo angle for no/min/max motor power.
	MOTOR_NEUTRAL      servoDegrees = 90
	MOTOR_FORWARD_LOW  servoDegrees = 100
	MOTOR_FORWARD_HIGH servoDegrees = 112 // real top is ~120; anything around ~125 may result in power loss
	MOTOR_REVERSE_LOW  servoDegrees = 81
	MOTOR_REVERSE_HIGH servoDegrees = 71

	MOTOR_GRIP servoDegrees = MOTOR_FORWARD_LOW

	// The exponent on how far through the detection-area band the ball is. Above 1
	// carries speed until the last moment; below 1 sheds it earlier.
	//
	// Area grows as the inverse square of distance, so 1 leaves the car near full
	// speed for most of the actual distance and sheds it all in the last few
	// frames. ~0.33 makes the slowdown track distance instead. Lower it if the car
	// still arrives too hot.
	MOTOR_DECELERATION_FACTOR = 0.33

	MOTOR_ACTION_FORWARD motorAction = "forward"
	MOTOR_ACTION_REVERSE motorAction = "reverse"
	MOTOR_ACTION_BRAKE   motorAction = "brake"

	// The two drive states needed before reverse will engage after a forward
	// drive. See armMotorForReverse.
	MOTOR_REVERSE_BRAKE_DURATION   = 500 * time.Millisecond
	MOTOR_REVERSE_NEUTRAL_DURATION = 250 * time.Millisecond

	TEST_MOTOR_DEFAULT_DRIVE_DURATION = 1 * time.Second

	// The servo's whole span rather than the calibrated MOTOR_* band, since pinning
	// that band down is what the test command is for.
	TEST_MOTOR_MIN_ANGLE = 1
	TEST_MOTOR_MAX_ANGLE = 180

	// So an ad-hoc test cannot leave the car driving unattended for minutes.
	TEST_MOTOR_MAX_SEQUENCE_STEPS = 10

	// Headroom added to a sequence's own timings when sizing its context deadline.
	TEST_MOTOR_SEQUENCE_TIMEOUT_HEADROOM = 10 * time.Second
)

type screenImage string

const (
	SCREEN_ROUTINE_NAME = "screen"

	// The images the fetch routine can ask the screen to show, in the order a
	// fetch walks through them. A battery reading outranks all of them.
	SCREEN_IMAGE_VIAM_LOGO           screenImage = "viam-logo"
	SCREEN_IMAGE_EYE                 screenImage = "eye"
	SCREEN_IMAGE_TENNIS_BALL_ROLLING screenImage = "tennis-ball-rolling"
	SCREEN_IMAGE_LOCK_FOUND          screenImage = "lock-found"
	SCREEN_IMAGE_LOCK_LOST           screenImage = "lock-lost"
	SCREEN_IMAGE_CLAW                screenImage = "claw"
	SCREEN_IMAGE_CLAW_WITH_BALL      screenImage = "claw-with-ball"
	SCREEN_IMAGE_K                   screenImage = "k"
	SCREEN_IMAGE_PERSON              screenImage = "person"

	// The rate at which the screen routine redraws the OLED.
	// For the tennis ball animation, 500ms gives a 2s full cycle
	// with 4 positions per rolling-ball cycle.
	SCREEN_TICK_RATE = 500 * time.Millisecond

	// How long the test command rolls the tennis ball for when asked to animate
	// rather than draw one frame. At SCREEN_TICK_RATE that is 20 frames, and a
	// round trip takes six of them — out across the four slots and back over the
	// middle two — so this is a little over three of them, enough to watch the
	// ball turn around at both ends more than once.
	SCREEN_TEST_ANIMATION_DURATION = 10 * time.Second

	// A 128x32 SSD1306 on the default I2C bus at 0x3C.
	SCREEN_WIDTH  = 128
	SCREEN_HEIGHT = 32
)

const (
	STEERING_ROUTINE_NAME = "steering"

	// Half the VISION_TICK_RATE: no missed readings and no phase drift.
	STEERING_TICK_RATE = VISION_TICK_RATE / 2

	// Empirical measurements of the servo angle for no/min/max steering angle.
	STEERING_NEUTRAL   servoDegrees = 100
	STEERING_MAX_LEFT  servoDegrees = 115
	STEERING_MAX_RIGHT servoDegrees = 85

	// The exponent on how far off center the detection sits (0 center, 1 at either
	// edge). At 1 the deflection is linear. Below 1 turns hard for small offsets
	// and saturates early, keeping the ball centered as the car closes in; above 1
	// softens the center and saves deflection for the edges.
	STEERING_SENSITIVITY_SOFTNESS = 0.75
)

type visionPixels int

// Which of the two ways the vision routine reduces a frame to one detection, and
// so which detector it works from.
type visionDetectionKind string

const (
	VISION_DETECTION_SINGLE visionDetectionKind = "single"
	VISION_DETECTION_PAIR   visionDetectionKind = "pair"
)

const (
	VISION_ROUTINE_NAME = "vision"

	// handleTestVision measures a round trip at just under 25ms.
	VISION_TICK_RATE = 25 * time.Millisecond

	// Must match the camera resolution set in the Viam config.
	VISION_MIN_X visionPixels = 0   // left-most
	VISION_MAX_X visionPixels = 640 // right-most
	VISION_MIN_Y visionPixels = 0   // top-most
	VISION_MAX_Y visionPixels = 360 // bottom-most

	// Detections centered above this line are dropped by both pipelines. The camera
	// sits low and looks along the floor, so the top of the frame is room the car
	// could never drive to — anything ball-like up there just shares its color.
	VISION_DETECTION_MIN_CENTER_Y visionPixels = VISION_MIN_Y + (VISION_MAX_Y-VISION_MIN_Y)/4

	// Detection single: the stitching gap, then the band of areas the fetch drives
	// on. Below the band is visual noise; reaching the top ends the chase.
	VISION_DETECTION_SINGLE_MERGE_MAX_GAP visionPixels = 50
	VISION_DETECTION_SINGLE_MIN_AREA      visionPixels = 85
	VISION_DETECTION_SINGLE_MAX_AREA      visionPixels = 1500

	// Detection pair: the same stitching on one shoe, far tighter than the
	// single's. Two shoes only read as a pair while they are still two boxes, and
	// the single's gap would swallow them into one first.
	VISION_DETECTION_PAIR_MERGE_MAX_GAP visionPixels = 10

	// What makes two boxes a pair: someone stands with both feet about equally far
	// from the camera and side by side, so the boxes come out similar in size,
	// level, and near each other.
	//
	// Generous on all three, since a stance is not symmetrical and by this point in
	// a fetch nothing else competes to be mistaken for feet.
	VISION_DETECTION_PAIR_MAX_CENTER_X_GAP visionPixels = (VISION_MAX_X - VISION_MIN_X) / 2
	VISION_DETECTION_PAIR_MAX_CENTER_Y_GAP visionPixels = (VISION_MAX_Y - VISION_MIN_Y) / 4
	VISION_DETECTION_PAIR_MIN_AREA_RATIO                = 0.5

	// The band the delivery drives on, measured on the box around both shoes. The
	// upper bound is the car at the person's feet; it sits above the single's
	// because two shoes cover more of the frame than a ball ever does.
	VISION_DETECTION_PAIR_COMBINED_MIN_AREA visionPixels = 50
	VISION_DETECTION_PAIR_COMBINED_MAX_AREA visionPixels = 23_000

	// The detection must have sunk into the bottom quarter of the frame before the
	// ball is between the jaws — reached by creeping, not by landing the chase on
	// an exact stopping point.
	//
	// Height carries the distance on its own here: anything tracked this late has
	// already passed VISION_DETECTION_SINGLE_MAX_AREA, so it is known to be the
	// ball and known to be close, and its apparent size now depends more on camera
	// aim than on range. Sensitive to camera resolution and placement.
	VISION_GRIPPER_Y_MIN_THRESHOLD visionPixels = VISION_MAX_Y - (VISION_MAX_Y-VISION_MIN_Y)/4

	// The middle 80% of the frame, which the detection's center must sit inside
	// before the jaws close. Only balls far enough aside to be shouldered are
	// worth excluding: at this range the nose has little room left to swing, so a
	// narrower band leaves the car creeping at a ball it will never accept.
	VISION_GRIPPER_X_MIN_THRESHOLD visionPixels = VISION_MIN_X + (VISION_MAX_X-VISION_MIN_X)/10
	VISION_GRIPPER_X_MAX_THRESHOLD visionPixels = VISION_MAX_X - (VISION_MAX_X-VISION_MIN_X)/10
)
