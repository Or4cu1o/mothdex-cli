package input

// ControllerType is SDL_GameControllerType (SDL 2.0.12 and later), read as the plain number.
type ControllerType int

// SDL_GameControllerType values.
const (
	TypeUnknown ControllerType = iota
	TypeXbox360
	TypeXboxOne
	TypePS3
	TypePS4
	TypeSwitchPro
	TypeVirtual
	TypePS5
	TypeLuna
	TypeStadia
	TypeShield
	TypeJoyConLeft
	TypeJoyConRight
	TypeJoyConPair
)

type family int

const (
	familyGeneric family = iota // unknown, virtual, cloud pads: Xbox letters, plain L and R
	familyXbox
	familyPlayStation
	familyNintendo
)

func (t ControllerType) family() family {
	switch t {
	case TypeXbox360, TypeXboxOne:
		return familyXbox
	case TypePS3, TypePS4, TypePS5:
		return familyPlayStation
	case TypeSwitchPro, TypeJoyConLeft, TypeJoyConRight, TypeJoyConPair:
		return familyNintendo
	}
	return familyGeneric
}

// PositionLabel is the label printed on a pad of this type at a button position (docs/ux/gui.md
// section 2.1): Xbox and unknown pads A B X Y; Nintendo pads B A Y X (south is B there); PlayStation pads
// the words Cross, Circle, Square and Triangle, since the font has no shape glyphs.
func PositionLabel(b Button, t ControllerType) string {
	f := t.family()
	switch b {
	case South:
		return pick(f, "A", "A", "Cross", "B")
	case East:
		return pick(f, "B", "B", "Circle", "A")
	case West:
		return pick(f, "X", "X", "Square", "Y")
	case North:
		return pick(f, "Y", "Y", "Triangle", "X")
	case LeftShoulder:
		return pick(f, "L", "LB", "L1", "L")
	case RightShoulder:
		return pick(f, "R", "RB", "R1", "R")
	case Start:
		return pick(f, "Start", "Menu", "Start", "Start")
	case Select:
		return pick(f, "Select", "View", "Select", "Select")
	}
	return ""
}

// pick chooses by family: generic, Xbox, PlayStation, Nintendo.
func pick(f family, generic, xbox, ps, nintendo string) string {
	switch f {
	case familyXbox:
		return xbox
	case familyPlayStation:
		return ps
	case familyNintendo:
		return nintendo
	}
	return generic
}

// RoleButton is the button position that plays a role, with the confirm/back swap.
func RoleButton(role Action, confirmEast bool) (Button, bool) {
	switch role {
	case Confirm:
		if confirmEast {
			return East, true
		}
		return South, true
	case Back:
		if confirmEast {
			return South, true
		}
		return East, true
	case Download:
		return West, true
	case Info:
		return North, true
	case PageUp:
		return LeftShoulder, true
	case PageDown:
		return RightShoulder, true
	case Menu:
		return Start, true
	case Queue:
		return Select, true
	}
	return 0, false
}

// Hint is the label a hint bar shows for a role on the pad in hand: what the document calls "(A) Open".
// A role with no button, such as a direction, gives "".
func Hint(role Action, t ControllerType, confirmEast bool) string {
	switch role { // the D-pad has no printed label: the arrows of the font stand for its directions
	case Left:
		return "←"
	case Right:
		return "→"
	case Up:
		return "↑"
	case Down:
		return "↓"
	}
	b, ok := RoleButton(role, confirmEast)
	if !ok {
		return ""
	}
	return PositionLabel(b, t)
}

// PadInfo is one joystick SDL sees. Mapped says SDL has a game controller mapping for it; a joystick without one cannot
// drive the GUI until the person adds a line to gamecontrollerdb.txt (docs/ux/gui.md section 2.3). GUID is the 32 hex
// digits of its model, "" where SDL cannot say.
type PadInfo struct {
	Name   string
	GUID   string
	Type   ControllerType
	Mapped bool
}

// IncomingKind is what a backend reports to the run loop.
type IncomingKind int

// Kinds of Incoming.
const (
	IncomingInput   IncomingKind = iota // a button, stick or key event for the Mapper
	IncomingResize                      // the display changed size
	IncomingPad                         // the pad in hand changed type, for the hint labels
	IncomingPadList                     // the joysticks present changed: the list of them, for the notices and About
	IncomingQuit                        // the window was asked to close
	IncomingText                        // typed text, while text input is on
	IncomingRedraw                      // the window was uncovered or restored and must be drawn again
)

// Incoming is one thing a backend tells the run loop. It carries no SDL types, so the loop and its tests
// do not need SDL.
type Incoming struct {
	Kind  IncomingKind
	Input Event          // IncomingInput
	W, H  int            // IncomingResize: the new size in pixels
	Pad   ControllerType // IncomingPad
	Pads  []PadInfo      // IncomingPadList: every joystick present, mapped or not
	Text  string         // IncomingText: what the keyboard typed, as UTF-8
}
