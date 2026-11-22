//go:build gui

package sdl

import (
	"errors"
	"fmt"
)

// Controller is an opened SDL_GameController, zero for "none".
type Controller uintptr

// userEventNone is what SDL_RegisterEvents returns when there are no event types left: (Uint32)-1.
const userEventNone = ^uint32(0)

// RegisterUserEvent reserves one event type for the program's own use: the wake-up that lets another
// goroutine end a WaitEvent.
func (l *Library) RegisterUserEvent() (uint32, error) {
	typ := l.a.registerEvents(1)
	if typ == userEventNone {
		return 0, errors.New("SDL_RegisterEvents: no event type left")
	}
	return typ, nil
}

// WaitEvent waits up to timeoutMS milliseconds for an event and puts it in e; a negative timeout waits with
// no limit and zero only looks. It reports false when no event came.
func (l *Library) WaitEvent(e *Event, timeoutMS int32) bool {
	return l.a.waitEventTimeout(e, timeoutMS) != 0
}

// OutputSize is the size of the render target in pixels.
func (l *Library) OutputSize(r Renderer) (w, h int32, err error) {
	rc := l.a.getRendererOutputSize(uintptr(r), &w, &h)
	return w, h, l.check(rc, "SDL_GetRendererOutputSize")
}

// SetLogicalSize makes the renderer scale a w x h drawing to the target, keeping the aspect ratio with
// bars around it; 0 x 0 turns it off.
func (l *Library) SetLogicalSize(r Renderer, w, h int32) error {
	return l.check(l.a.renderSetLogicalSize(uintptr(r), w, h), "SDL_RenderSetLogicalSize")
}

// SetDrawBlend sets how FillRect combines with what is there: BlendNone or BlendBlend.
func (l *Library) SetDrawBlend(r Renderer, mode int32) error {
	return l.check(l.a.setRenderDrawBlendMode(uintptr(r), mode), "SDL_SetRenderDrawBlendMode")
}

// SetTextureBlend sets how a texture combines with what is there.
func (l *Library) SetTextureBlend(t Texture, mode int32) error {
	return l.check(l.a.setTextureBlendMode(t.handle, mode), "SDL_SetTextureBlendMode")
}

// SetTextureColor tints a texture and sets its opacity, which is how a white glyph atlas takes any color.
func (l *Library) SetTextureColor(t Texture, red, green, blue, alpha uint8) error {
	if err := l.check(l.a.setTextureColorMod(t.handle, red, green, blue), "SDL_SetTextureColorMod"); err != nil {
		return err
	}
	return l.check(l.a.setTextureAlphaMod(t.handle, alpha), "SDL_SetTextureAlphaMod")
}

// HideCursor hides the mouse pointer: the GUI is for a pad.
func (l *Library) HideCursor() { l.a.showCursor(0) }

// JoystickCount is the number of joysticks SDL sees now.
func (l *Library) JoystickCount() int { return int(l.a.numJoysticks()) }

// IsController reports whether the joystick at index has a game controller mapping.
func (l *Library) IsController(index int) bool { return l.a.isGameController(int32(index)) != 0 } //nolint:gosec // a device index is small

// OpenController opens the game controller at a joystick index.
func (l *Library) OpenController(index int) (Controller, error) {
	c := Controller(l.a.gameControllerOpen(int32(index))) //nolint:gosec // a device index is small
	if c == 0 {
		return 0, l.sdlError("SDL_GameControllerOpen")
	}
	return c, nil
}

// CloseController closes a controller.
func (l *Library) CloseController(c Controller) { l.a.gameControllerClose(uintptr(c)) }

// ControllerName is the name SDL gives the pad.
func (l *Library) ControllerName(c Controller) string { return l.a.gameControllerName(uintptr(c)) }

// ControllerType is SDL_GameControllerType, or 0 (unknown) when this SDL2 is older than 2.0.12 and cannot say.
func (l *Library) ControllerType(c Controller) int {
	if l.a.gameControllerGetType == nil {
		return 0
	}
	return int(l.a.gameControllerGetType(uintptr(c)))
}

// ControllerInstanceID is the id the events of this pad carry in their Which field.
func (l *Library) ControllerInstanceID(c Controller) (int32, error) {
	joy := l.a.gameControllerGetJoy(uintptr(c))
	if joy == 0 {
		return 0, fmt.Errorf("SDL_GameControllerGetJoystick: %w", l.sdlError("call"))
	}
	return l.a.joystickInstanceID(joy), nil
}
