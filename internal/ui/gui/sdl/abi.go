//go:build gui

// Package sdl is a thin purego binding of the few SDL2 functions the GUI needs, loaded from the system
// SDL2 at run time (docs/contracts/cli/gui.md section 2). It is the only package that imports purego.
//
// Only pointers, integers and floats cross the boundary: no struct by value and no callback, because
// purego does not support them on linux/386 and linux/arm. SDL_Event and SDL_Rect go by pointer, and
// abi_test.go checks their layout against the SDL2 headers for every GOARCH.
package sdl

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"unsafe"
)

// SDL2 constants from SDL.h, SDL_video.h, SDL_render.h, SDL_blendmode.h, SDL_pixels.h, SDL_events.h,
// SDL_gamecontroller.h and SDL_keycode.h. scripts/check-sdl-constants.sh compares every one of them with the
// headers: a new numeric constant here needs an entry in its MAP.
const (
	InitVideo          = 0x00000020
	InitGameController = 0x00002000

	WindowShown             = 0x00000004
	WindowHidden            = 0x00000008
	WindowFullscreenDesktop = 0x00001001

	RendererSoftware     = 0x00000001
	RendererPresentVSync = 0x00000004

	TextureAccessStatic = 0

	BlendNone  = 0 // SDL_BLENDMODE_NONE
	BlendBlend = 1 // SDL_BLENDMODE_BLEND: alpha blending

	// PixelFormatARGB8888 is SDL_PIXELFORMAT_ARGB8888: on a little-endian CPU the bytes are B, G, R, A.
	PixelFormatARGB8888 = 0x16362004

	EventQuit                 = 0x100
	EventWindow               = 0x200 // SDL_WINDOWEVENT
	EventKeyDown              = 0x300
	EventKeyUp                = 0x301
	EventTextInput            = 0x303
	EventControllerAxisMotion = 0x650
	// EventControllerButtonDown is SDL_CONTROLLERBUTTONDOWN: SDL_CONTROLLERAXISMOTION is 0x650, then
	// BUTTONDOWN, BUTTONUP, DEVICEADDED and DEVICEREMOVED follow in that order.
	EventControllerButtonDown    = 0x651
	EventControllerButtonUp      = 0x652
	EventControllerDeviceAdded   = 0x653
	EventControllerDeviceRemoved = 0x654
	// EventJoyDeviceAdded is SDL_JOYDEVICEADDED: SDL_JOYAXISMOTION is 0x600, then JOYBALLMOTION, JOYHATMOTION,
	// JOYBUTTONDOWN, JOYBUTTONUP, DEVICEADDED and DEVICEREMOVED follow in that order. A joystick that has no
	// game controller mapping only ever gives these two events.
	EventJoyDeviceAdded   = 0x605
	EventJoyDeviceRemoved = 0x606

	WindowEventShown       = 1 // SDL_WINDOWEVENT_SHOWN
	WindowEventExposed     = 3 // SDL_WINDOWEVENT_EXPOSED
	WindowEventRestored    = 9 // SDL_WINDOWEVENT_RESTORED
	WindowEventResized     = 5 // SDL_WINDOWEVENT_RESIZED
	WindowEventSizeChanged = 6 // SDL_WINDOWEVENT_SIZE_CHANGED

	// SDL_GameControllerButton, the ones the GUI reads. They are positions: A is south, B east, X west, Y north.
	ControllerButtonA             = 0
	ControllerButtonB             = 1
	ControllerButtonX             = 2
	ControllerButtonY             = 3
	ControllerButtonBack          = 4
	ControllerButtonStart         = 6
	ControllerButtonLeftShoulder  = 9
	ControllerButtonRightShoulder = 10
	ControllerButtonDPadUp        = 11
	ControllerButtonDPadDown      = 12
	ControllerButtonDPadLeft      = 13
	ControllerButtonDPadRight     = 14

	// SDL_GameControllerAxis, the left stick.
	ControllerAxisLeftX = 0
	ControllerAxisLeftY = 1

	// SDL_Keycode values of the keys the keyboard fallback reads (letters are their ASCII codes).
	KeycodeBackspace = 8
	KeycodeTab       = 9
	KeycodeReturn    = 13
	KeycodeEscape    = 27
	KeycodeRight     = 0x4000004F
	KeycodeLeft      = 0x40000050
	KeycodeDown      = 0x40000051
	KeycodeUp        = 0x40000052
	KeycodePageUp    = 0x4000004B
	KeycodePageDown  = 0x4000004E
	KeycodeKPEnter   = 0x40000058 // SDLK_KP_ENTER
)

// Version is SDL_version.
type Version struct{ Major, Minor, Patch uint8 }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// AtLeast reports whether v is the same as or newer than min.
func (v Version) AtLeast(min Version) bool {
	if v.Major != min.Major {
		return v.Major > min.Major
	}
	if v.Minor != min.Minor {
		return v.Minor > min.Minor
	}
	return v.Patch >= min.Patch
}

// Rect is SDL_Rect: four C ints.
type Rect struct{ X, Y, W, H int32 }

// eventSize is sizeof(SDL_Event): a union padded to 56 bytes on every platform SDL2 supports.
const eventSize = 56

// Event is SDL_Event. It is held as seven uint64 so that its start is 8-byte aligned, which SDL_Event
// needs on ARM EABI (it holds 64-bit members). Allocate it with NewEvent, so it lives on the heap.
type Event struct{ raw [eventSize / 8]uint64 }

// NewEvent returns a zeroed event on the heap.
func NewEvent() *Event { return new(Event) }

// Type is the first field of every SDL_Event member.
func (e *Event) Type() uint32 { return *(*uint32)(unsafe.Pointer(&e.raw[0])) } //nolint:gosec // raw is 56 bytes and its first four are the type

// ControllerButtonEvent is SDL_ControllerButtonEvent.
type ControllerButtonEvent struct {
	Type      uint32
	Timestamp uint32
	Which     int32 // SDL_JoystickID
	Button    uint8
	State     uint8
	_         [2]uint8
}

// ControllerButton views the event as a controller button event. The caller checks Type first.
func (e *Event) ControllerButton() *ControllerButtonEvent {
	return (*ControllerButtonEvent)(unsafe.Pointer(e)) //nolint:gosec // the struct is 16 bytes, Event is 56, same start, same layout (abi_test.go)
}

// SetControllerButton fills the event as a controller button event.
func (e *Event) SetControllerButton(typ uint32, which int32, button, state uint8) {
	*e = Event{}
	*e.ControllerButton() = ControllerButtonEvent{Type: typ, Which: which, Button: button, State: state}
}

// WindowEvent is SDL_WindowEvent.
type WindowEvent struct {
	Type      uint32
	Timestamp uint32
	WindowID  uint32
	Event     uint8
	_         [3]uint8
	Data1     int32
	Data2     int32
}

// Window views the event as a window event. The caller checks Type first.
func (e *Event) Window() *WindowEvent {
	return (*WindowEvent)(unsafe.Pointer(e)) //nolint:gosec // 24 bytes at the start of a 56-byte event, same layout (abi_test.go)
}

// KeyboardEvent is SDL_KeyboardEvent with its SDL_Keysym inlined.
type KeyboardEvent struct {
	Type      uint32
	Timestamp uint32
	WindowID  uint32
	State     uint8
	Repeat    uint8
	_         [2]uint8
	Scancode  int32
	Sym       int32 // SDL_Keycode
	Mod       uint16
	_         uint16
	Unused    uint32
}

// Keyboard views the event as a keyboard event. The caller checks Type first.
func (e *Event) Keyboard() *KeyboardEvent {
	return (*KeyboardEvent)(unsafe.Pointer(e)) //nolint:gosec // 32 bytes at the start of a 56-byte event, same layout (abi_test.go)
}

// ControllerAxisEvent is SDL_ControllerAxisEvent.
type ControllerAxisEvent struct {
	Type      uint32
	Timestamp uint32
	Which     int32 // SDL_JoystickID
	Axis      uint8
	_         [3]uint8
	Value     int16
	_         uint16
}

// ControllerAxis views the event as a controller axis event. The caller checks Type first.
func (e *Event) ControllerAxis() *ControllerAxisEvent {
	return (*ControllerAxisEvent)(unsafe.Pointer(e)) //nolint:gosec // 20 bytes at the start of a 56-byte event, same layout (abi_test.go)
}

// ControllerDeviceEvent is SDL_ControllerDeviceEvent: Which is a device index for DEVICEADDED and an instance
// id for DEVICEREMOVED.
type ControllerDeviceEvent struct {
	Type      uint32
	Timestamp uint32
	Which     int32
}

// ControllerDevice views the event as a controller device event. The caller checks Type first.
func (e *Event) ControllerDevice() *ControllerDeviceEvent {
	return (*ControllerDeviceEvent)(unsafe.Pointer(e)) //nolint:gosec // 12 bytes at the start of a 56-byte event, same layout (abi_test.go)
}

// JoyDevice views the event as a joystick device event, which has the layout of a controller device event: Which is
// a device index for DEVICEADDED and an instance id for DEVICEREMOVED. The caller checks Type first.
func (e *Event) JoyDevice() *ControllerDeviceEvent { return e.ControllerDevice() }

// JoystickGUID is SDL_JoystickGUID, 16 bytes that name a model of device. It is declared as two 64-bit words, not
// as the C struct { Uint8 data[16] }: purego cannot return a struct with an array field (it panics when the function
// is called), while a struct of two integers is returned in registers exactly as the C one is on amd64 and arm64
// (found by Crivo's probe against a virtual joystick). Lo holds bytes 0 to 7 and Hi bytes 8 to 15, little-endian.
type JoystickGUID struct{ Lo, Hi uint64 }

// String is the 32 lower-case hex digits SDL_JoystickGetGUIDString writes, the form of gamecontrollerdb.txt.
func (g JoystickGUID) String() string {
	var raw [16]byte
	binary.LittleEndian.PutUint64(raw[:8], g.Lo)
	binary.LittleEndian.PutUint64(raw[8:], g.Hi)
	return hex.EncodeToString(raw[:])
}

// SetType makes the event an otherwise empty event of the given type, as SDL_UserEvent is for a wake-up.
func (e *Event) SetType(typ uint32) {
	*e = Event{}
	*(*uint32)(unsafe.Pointer(&e.raw[0])) = typ //nolint:gosec // raw is 56 bytes and its first four are the type
}

// textInputSize is the size of the Text field of SDL_TextInputEvent: SDL_TEXTINPUTEVENT_TEXT_SIZE.
const textInputSize = 32

// TextInputEvent is SDL_TextInputEvent: the UTF-8 text typed, ended by a zero byte.
type TextInputEvent struct {
	Type      uint32
	Timestamp uint32
	WindowID  uint32
	Text      [textInputSize]byte
}

// TextInput views the event as a text input event. The caller checks Type first.
func (e *Event) TextInput() *TextInputEvent {
	return (*TextInputEvent)(unsafe.Pointer(e)) //nolint:gosec // 44 bytes at the start of a 56-byte event, same layout (abi_test.go)
}

// String is the text of the event, up to its first zero byte.
func (t *TextInputEvent) String() string {
	for i, b := range t.Text {
		if b == 0 {
			return string(t.Text[:i])
		}
	}
	return string(t.Text[:])
}
