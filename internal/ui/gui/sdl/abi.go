//go:build gui

// Package sdl is a thin purego binding of the few SDL2 functions the GUI needs, loaded from the system
// SDL2 at run time (docs/contracts/cli/gui.md section 2). It is the only package that imports purego.
//
// Only pointers, integers and floats cross the boundary: no struct by value and no callback, because
// purego does not support them on linux/386 and linux/arm. SDL_Event and SDL_Rect go by pointer, and
// abi_test.go checks their layout against the SDL2 headers for every GOARCH.
package sdl

import (
	"fmt"
	"unsafe"
)

// SDL2 constants from SDL.h, SDL_video.h, SDL_render.h, SDL_pixels.h and SDL_events.h.
const (
	InitVideo          = 0x00000020
	InitGameController = 0x00002000

	WindowHidden = 0x00000008

	RendererSoftware = 0x00000001

	TextureAccessStatic = 0

	// PixelFormatARGB8888 is SDL_PIXELFORMAT_ARGB8888: on a little-endian CPU the bytes are B, G, R, A.
	PixelFormatARGB8888 = 0x16362004

	EventQuit = 0x100
	// EventControllerButtonDown is SDL_CONTROLLERBUTTONDOWN: SDL_CONTROLLERAXISMOTION is 0x650, then
	// BUTTONDOWN, BUTTONUP and DEVICEADDED (0x653) follow in that order.
	EventControllerButtonDown = 0x651
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
