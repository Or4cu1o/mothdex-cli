// Package input turns SDL-neutral input events into actions (docs/ux/gui.md section 2): buttons by
// position, the confirm/back swap, key repeat, stick hysteresis, Select on release, and the keyboard
// fallback. It imports nothing, so every rule here is tested without SDL or a clock: time comes in as a
// value.
package input

import "time"

// Button is a controller button by position on the pad, which is how SDL_GameControllerDB names them
// (a = south, b = east, x = west, y = north).
type Button int

// Buttons.
const (
	South Button = iota
	East
	West
	North
	LeftShoulder
	RightShoulder
	Start
	Select // SDL "back"
	DPadUp
	DPadDown
	DPadLeft
	DPadRight
)

// Action is what the model reacts to. Names follow the roles of the document (A, B, X, Y, L, R, Start,
// Select), not the buttons, so the swap is invisible to it.
type Action int

// Actions.
const (
	None Action = iota
	Up
	Down
	Left
	Right
	Confirm  // A: confirm, open, toggle
	Back     // B: back, close, cancel
	Download // X: the screen's main action
	Info     // Y: info, details
	PageUp   // L
	PageDown // R
	Menu     // Start
	Queue    // Select, on release
	Delete   // Backspace on the keyboard screen
)

// Kind is the kind of an Event.
type Kind int

// Event kinds.
const (
	ButtonPress Kind = iota
	ButtonRelease
	AxisMotion
	KeyPress
	KeyRelease
)

// Axis is a stick axis.
type Axis int

// Axes.
const (
	LeftX Axis = iota
	LeftY
)

// KeyCode is a key of the keyboard fallback. KeyRune carries the letter in Event.Rune.
type KeyCode int

// Key codes.
const (
	KeyNone KeyCode = iota
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyPageUp
	KeyPageDown
	KeyTab
	KeyRune
)

// Event is one thing the player did, as the backend saw it.
type Event struct {
	Kind   Kind
	Button Button  // ButtonPress, ButtonRelease
	Axis   Axis    // AxisMotion
	Value  int     // AxisMotion: -32768 to 32767
	Key    KeyCode // KeyPress, KeyRelease
	Rune   rune    // KeyRune
}

// Timing and thresholds of docs/ux/gui.md section 2.2.
const (
	FirstRepeat  = 400 * time.Millisecond // before the first repeat of a held direction
	RepeatEvery  = 80 * time.Millisecond  // then every
	FastAfter    = 2 * time.Second        // after this long a hold repeats faster
	FastEvery    = 40 * time.Millisecond
	axisRange    = 32768                // the half-range of a signed 16-bit axis
	stickPress   = axisRange * 50 / 100 // a direction counts from 50 % of the range
	stickRelease = axisRange * 35 / 100 // and ends below 35 %
	maxCatchUp   = 4                    // repeats one Tick may emit after a late frame
)

// Config is what the player can change.
type Config struct {
	// ConfirmEast swaps A and B only (gui.confirm_button = east).
	ConfirmEast bool
}

// source says what holds the repeating action, so releasing something else does not stop it.
type source struct {
	kind   Kind
	button Button
	key    KeyCode
	stick  bool
}

// Mapper keeps the state input needs between events: what is held, the stick, and Select.
type Mapper struct {
	cfg Config

	// TextMode is on while the virtual keyboard is open: letters arrive as text, so x, y and q are not
	// the X, Y and Select of the keyboard fallback, and Backspace deletes.
	TextMode bool
	// RepeatBack makes a held B repeat (deleting on the keyboard screen).
	RepeatBack bool

	rep struct {
		on    bool
		act   Action
		src   source
		since time.Time
		next  time.Time
	}

	stickX, stickY int
	stickDir       Action

	selectDown  bool
	selectClean bool
}

// NewMapper returns a mapper with the given configuration.
func NewMapper(cfg Config) *Mapper { return &Mapper{cfg: cfg} }

// SetConfig applies a change of configuration at once.
func (m *Mapper) SetConfig(cfg Config) { m.cfg = cfg }

// buttonAction is the role of a button by position, with the confirm/back swap.
func (m *Mapper) buttonAction(b Button) Action {
	switch b {
	case South:
		if m.cfg.ConfirmEast {
			return Back
		}
		return Confirm
	case East:
		if m.cfg.ConfirmEast {
			return Confirm
		}
		return Back
	case West:
		return Download
	case North:
		return Info
	case LeftShoulder:
		return PageUp
	case RightShoulder:
		return PageDown
	case Start:
		return Menu
	case DPadUp:
		return Up
	case DPadDown:
		return Down
	case DPadLeft:
		return Left
	case DPadRight:
		return Right
	}
	return None // Select acts on release
}

// repeats says whether holding the action repeats it.
func (m *Mapper) repeats(a Action) bool {
	switch a {
	case Up, Down, Left, Right, PageUp, PageDown, Delete:
		return true
	case Back:
		return m.RepeatBack
	}
	return false
}

// Handle reads one event and returns the actions it causes, in order.
func (m *Mapper) Handle(e Event, now time.Time) []Action {
	switch e.Kind {
	case ButtonPress:
		return m.buttonDown(e.Button, now)
	case ButtonRelease:
		return m.buttonUp(e.Button)
	case AxisMotion:
		return m.axis(e, now)
	case KeyPress:
		return m.keyDown(e, now)
	case KeyRelease:
		m.stopIf(source{kind: KeyPress, key: e.Key})
	}
	return nil
}

func (m *Mapper) buttonDown(b Button, now time.Time) []Action {
	if b == Select {
		m.selectDown, m.selectClean = true, true
		return nil
	}
	if m.selectDown { // Select is a hotkey modifier for the frontend: this is a combo, not a Queue press
		m.selectClean = false
	}
	a := m.buttonAction(b)
	if a == None {
		return nil
	}
	return m.press(a, source{kind: ButtonPress, button: b}, now)
}

func (m *Mapper) buttonUp(b Button) []Action {
	if b == Select {
		clean := m.selectDown && m.selectClean
		m.selectDown, m.selectClean = false, false
		if clean {
			return []Action{Queue}
		}
		return nil
	}
	m.stopIf(source{kind: ButtonPress, button: b})
	return nil
}

// press emits an action and starts its repeat when it has one.
func (m *Mapper) press(a Action, src source, now time.Time) []Action {
	if m.repeats(a) {
		m.rep.on, m.rep.act, m.rep.src, m.rep.since, m.rep.next = true, a, src, now, now.Add(FirstRepeat)
	}
	return []Action{a}
}

func (m *Mapper) stopIf(src source) {
	if m.rep.on && m.rep.src == src {
		m.rep.on = false
	}
}

// axis applies the stick rules: only the dominant axis counts, a direction starts past 50 % and ends
// below 35 %, and there is no diagonal.
func (m *Mapper) axis(e Event, now time.Time) []Action {
	if e.Axis == LeftX {
		m.stickX = e.Value
	} else {
		m.stickY = e.Value
	}
	ax, ay := abs(m.stickX), abs(m.stickY)
	mag, dir := ax, stickDirection(LeftX, m.stickX)
	if ay > ax {
		mag, dir = ay, stickDirection(LeftY, m.stickY)
	}
	switch {
	case m.stickDir == None && mag >= stickPress:
		m.stickDir = dir
		return m.press(dir, source{stick: true}, now)
	case m.stickDir != None && mag < stickRelease:
		m.stickDir = None
		m.stopIf(source{stick: true})
	case m.stickDir != None && dir != m.stickDir && mag >= stickPress:
		m.stickDir = dir
		return m.press(dir, source{stick: true}, now)
	}
	return nil
}

func stickDirection(a Axis, v int) Action {
	switch {
	case a == LeftX && v < 0:
		return Left
	case a == LeftX:
		return Right
	case v < 0:
		return Up
	}
	return Down
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// keyDown is the keyboard fallback of docs/ux/gui.md section 2.4. SDL's own key repeat is ignored by the
// backend: the repeat timing here is the one of the pad.
func (m *Mapper) keyDown(e Event, now time.Time) []Action {
	src := source{kind: KeyPress, key: e.Key}
	var a Action
	switch e.Key {
	case KeyUp:
		a = Up
	case KeyDown:
		a = Down
	case KeyLeft:
		a = Left
	case KeyRight:
		a = Right
	case KeyEnter:
		a = Confirm
	case KeyEscape:
		a = Back
	case KeyBackspace:
		a = Back
		if m.TextMode {
			a = Delete
		}
	case KeyPageUp:
		a = PageUp
	case KeyPageDown:
		a = PageDown
	case KeyTab:
		a = Menu
	case KeyRune:
		if m.TextMode {
			return nil // typing goes to the field through text input
		}
		switch e.Rune {
		case 'x', 'X':
			a = Download
		case 'y', 'Y':
			a = Info
		case 'q', 'Q':
			a = Queue
		}
	}
	if a == None {
		return nil
	}
	return m.press(a, src, now)
}

// Tick emits the repeats that are due at now. Call it with the time of each frame.
func (m *Mapper) Tick(now time.Time) []Action {
	var out []Action
	for i := 0; m.rep.on && !now.Before(m.rep.next) && i < maxCatchUp; i++ {
		out = append(out, m.rep.act)
		every := RepeatEvery
		if now.Sub(m.rep.since) >= FastAfter {
			every = FastEvery
		}
		m.rep.next = m.rep.next.Add(every)
	}
	if m.rep.on && now.After(m.rep.next.Add(FirstRepeat)) { // a long stall: do not replay the whole backlog
		m.rep.next = now.Add(RepeatEvery)
	}
	return out
}

// Deadline is when the next repeat is due, so the loop can sleep until then.
func (m *Mapper) Deadline() (time.Time, bool) { return m.rep.next, m.rep.on }

// Held reports whether an action is repeating now.
func (m *Mapper) Held() (Action, bool) { return m.rep.act, m.rep.on }

// Release stops any repeat: call it when focus moves to somewhere a held button must not carry on,
// for example when a dialog opens.
func (m *Mapper) Release() { m.rep.on = false }
