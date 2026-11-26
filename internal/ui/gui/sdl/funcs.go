//go:build gui

package sdl

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Errors that tell the caller what is wrong with the SDL2 on this machine (exit code 8 of the GUI).
var (
	ErrNotFound     = errors.New("SDL2 was not found")
	ErrIncompatible = errors.New("SDL2 lacks a function this program needs")
	ErrTooOld       = errors.New("SDL2 is too old")
	ErrNoDisplay    = errors.New("SDL2 has no video device")
	// ErrNoWindowIcon is not fatal: SDL2 before 2.0.5 cannot make a surface from pixels, so the window keeps its default icon.
	ErrNoWindowIcon = errors.New("SDL2 cannot set a window icon (needs 2.0.5)")
	// ErrSearchPath is Windows only: the process could not be limited to its own folder and System32 for DLLs,
	// so SDL2 was not loaded. It is not "not found": the library may be right there (T-72).
	ErrSearchPath = errors.New("cannot restrict the DLL search path")
)

// TargetVersion is the SDL2 the GUI is meant for (docs/contracts/cli/gui.md section 2). MinimumVersion is
// what the bound functions need; an SDL2 older than that is refused.
var TargetVersion = Version{2, 0, 18}

// api holds the SDL2 functions the spike calls, each as a Go function with the C signature. Types follow
// the purego rules: pointers, ints and floats only; a C string is a Go string; SDL_bool is an int32.
//
// A C string returned as a Go string (getError, getCurrentVideoDriver) is a copy: purego v0.11.3 builds it
// with internal/strings.GoString, which ends in string(unsafe.Slice(ptr, n)) and so allocates. What SDL
// returns can change on the next call, and the Go string is not affected. A purego bump must keep this
// true (re-read GoString).
type api struct {
	getVersion               func(v *Version)
	init                     func(flags uint32) int32
	quit                     func()
	getError                 func() string
	setHint                  func(name, value string) int32
	getCurrentVideoDriver    func() string
	createWindow             func(title string, x, y, w, h int32, flags uint32) uintptr
	destroyWindow            func(window uintptr)
	showWindow               func(window uintptr)
	raiseWindow              func(window uintptr)
	getWindowFlags           func(window uintptr) uint32
	createRenderer           func(window uintptr, index int32, flags uint32) uintptr
	destroyRenderer          func(renderer uintptr)
	setRenderDrawColor       func(renderer uintptr, r, g, b, a uint8) int32
	renderClear              func(renderer uintptr) int32
	renderFillRect           func(renderer uintptr, rect *Rect) int32
	renderCopy               func(renderer, texture uintptr, src, dst *Rect) int32
	renderPresent            func(renderer uintptr)
	renderReadPixels         func(renderer uintptr, rect *Rect, format uint32, pixels unsafe.Pointer, pitch int32) int32
	renderSetScale           func(renderer uintptr, sx, sy float32) int32
	createTexture            func(renderer uintptr, format uint32, access, w, h int32) uintptr
	updateTexture            func(texture uintptr, rect *Rect, pixels unsafe.Pointer, pitch int32) int32
	destroyTexture           func(texture uintptr)
	pollEvent                func(e *Event) int32
	pushEvent                func(e *Event) int32
	gameControllerAddMapping func(mapping string) int32

	registerEvents         func(n int32) uint32
	waitEventTimeout       func(e *Event, timeoutMS int32) int32
	getRendererOutputSize  func(renderer uintptr, w, h *int32) int32
	renderSetLogicalSize   func(renderer uintptr, w, h int32) int32
	setRenderDrawBlendMode func(renderer uintptr, mode int32) int32
	setTextureBlendMode    func(texture uintptr, mode int32) int32
	setTextureColorMod     func(texture uintptr, r, g, b uint8) int32
	setTextureAlphaMod     func(texture uintptr, a uint8) int32
	showCursor             func(toggle int32) int32
	numJoysticks           func() int32
	isGameController       func(index int32) int32
	gameControllerOpen     func(index int32) uintptr
	gameControllerClose    func(controller uintptr)
	gameControllerName     func(controller uintptr) string
	gameControllerGetJoy   func(controller uintptr) uintptr
	joystickInstanceID     func(joystick uintptr) int32
	gameControllerGetType  func(controller uintptr) int32 // SDL 2.0.12; nil on an older SDL2
	joystickNameForIndex   func(index int32) string
	joystickDeviceInstance func(index int32) int32        // SDL 2.0.6; nil on an older SDL2
	joystickDeviceGUID     func(index int32) JoystickGUID // SDL 2.0.6; nil on an older SDL2 and where purego cannot return a struct
	startTextInput         func()
	stopTextInput          func()

	createRGBSurfaceWithFormatFrom func(pixels unsafe.Pointer, w, h, depth, pitch int32, format uint32) uintptr // SDL 2.0.5; nil on an older SDL2
	freeSurface                    func(surface uintptr)                                                        // SDL 2.0.5 table entry, with the function above
	setWindowIcon                  func(window, surface uintptr)                                                // SDL 2.0.5 table entry, with the functions above
}

// binding is one function to look up: its C name, the SDL2 release that introduced it, and where the
// Go function goes. An optional function is bound only when the loaded SDL2 has it, and its wrapper has a
// fallback for when it does not: it does not raise MinimumVersion.
type binding struct {
	name     string
	since    Version
	fn       any
	optional bool
}

func (a *api) table() []binding {
	v200 := Version{2, 0, 0}
	v205 := Version{2, 0, 5}
	v206 := Version{2, 0, 6}
	v2012 := Version{2, 0, 12}
	return []binding{
		{"SDL_GetVersion", v200, &a.getVersion, false},
		{"SDL_Init", v200, &a.init, false},
		{"SDL_Quit", v200, &a.quit, false},
		{"SDL_GetError", v200, &a.getError, false},
		{"SDL_SetHint", v200, &a.setHint, false},
		{"SDL_GetCurrentVideoDriver", v200, &a.getCurrentVideoDriver, false},
		{"SDL_CreateWindow", v200, &a.createWindow, false},
		{"SDL_DestroyWindow", v200, &a.destroyWindow, false},
		{"SDL_ShowWindow", v200, &a.showWindow, false},
		{"SDL_RaiseWindow", v200, &a.raiseWindow, false},
		{"SDL_GetWindowFlags", v200, &a.getWindowFlags, false},
		{"SDL_CreateRenderer", v200, &a.createRenderer, false},
		{"SDL_DestroyRenderer", v200, &a.destroyRenderer, false},
		{"SDL_SetRenderDrawColor", v200, &a.setRenderDrawColor, false},
		{"SDL_RenderClear", v200, &a.renderClear, false},
		{"SDL_RenderFillRect", v200, &a.renderFillRect, false},
		{"SDL_RenderCopy", v200, &a.renderCopy, false},
		{"SDL_RenderPresent", v200, &a.renderPresent, false},
		{"SDL_RenderReadPixels", v200, &a.renderReadPixels, false},
		{"SDL_RenderSetScale", v200, &a.renderSetScale, false},
		{"SDL_CreateTexture", v200, &a.createTexture, false},
		{"SDL_UpdateTexture", v200, &a.updateTexture, false},
		{"SDL_DestroyTexture", v200, &a.destroyTexture, false},
		{"SDL_PollEvent", v200, &a.pollEvent, false},
		{"SDL_PushEvent", v200, &a.pushEvent, false},
		{"SDL_GameControllerAddMapping", v200, &a.gameControllerAddMapping, false},

		{"SDL_RegisterEvents", v200, &a.registerEvents, false},
		{"SDL_WaitEventTimeout", v200, &a.waitEventTimeout, false},
		{"SDL_GetRendererOutputSize", v200, &a.getRendererOutputSize, false},
		{"SDL_RenderSetLogicalSize", v200, &a.renderSetLogicalSize, false},
		{"SDL_SetRenderDrawBlendMode", v200, &a.setRenderDrawBlendMode, false},
		{"SDL_SetTextureBlendMode", v200, &a.setTextureBlendMode, false},
		{"SDL_SetTextureColorMod", v200, &a.setTextureColorMod, false},
		{"SDL_SetTextureAlphaMod", v200, &a.setTextureAlphaMod, false},
		{"SDL_ShowCursor", v200, &a.showCursor, false},
		{"SDL_NumJoysticks", v200, &a.numJoysticks, false},
		{"SDL_IsGameController", v200, &a.isGameController, false},
		{"SDL_GameControllerOpen", v200, &a.gameControllerOpen, false},
		{"SDL_GameControllerClose", v200, &a.gameControllerClose, false},
		{"SDL_GameControllerName", v200, &a.gameControllerName, false},
		{"SDL_GameControllerGetJoystick", v200, &a.gameControllerGetJoy, false},
		{"SDL_JoystickInstanceID", v200, &a.joystickInstanceID, false},
		{"SDL_GameControllerGetType", v2012, &a.gameControllerGetType, true},
		{"SDL_JoystickNameForIndex", v200, &a.joystickNameForIndex, false},
		{"SDL_JoystickGetDeviceInstanceID", v206, &a.joystickDeviceInstance, true},
		{"SDL_JoystickGetDeviceGUID", v206, &a.joystickDeviceGUID, true},
		{"SDL_StartTextInput", v200, &a.startTextInput, false},
		{"SDL_StopTextInput", v200, &a.stopTextInput, false},
		// The window icon is cosmetic: an SDL2 older than 2.0.5 has no SDL_CreateRGBSurfaceWithFormatFrom and the window
		// simply keeps the default icon, so these do not raise the minimum version.
		{"SDL_CreateRGBSurfaceWithFormatFrom", v205, &a.createRGBSurfaceWithFormatFrom, true},
		{"SDL_FreeSurface", v205, &a.freeSurface, true},
		{"SDL_SetWindowIcon", v205, &a.setWindowIcon, true},
	}
}

// bind looks the functions up in the loaded library. SDL_GetVersion comes first, because what the loaded
// SDL2 has decides which optional functions are looked up at all. A missing symbol makes purego panic; it
// becomes ErrIncompatible here, unless the function is optional and the SDL2 predates it.
func (a *api) bind(handle uintptr) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrIncompatible, r)
		}
	}()
	table := a.table()
	purego.RegisterLibFunc(table[0].fn, handle, table[0].name) // SDL_GetVersion
	var loaded Version
	a.getVersion(&loaded)
	for _, b := range table[1:] {
		if b.optional && !loaded.AtLeast(b.since) {
			continue // an older SDL2: the wrapper falls back
		}
		if returnsStruct[b.name] && !structsByValue() {
			continue // purego cannot return a struct here: the wrapper says "unavailable"
		}
		purego.RegisterLibFunc(b.fn, handle, b.name)
	}
	return nil
}

// returnsStruct names the bound functions that return a C struct by value. purego supports that only on amd64 and
// arm64 (its README, support notes 2 and 3), so on 32-bit ARM and x86 they stay unbound and their wrappers report
// "unavailable".
var returnsStruct = map[string]bool{"SDL_JoystickGetDeviceGUID": true}

// structsByValue reports whether purego can return a struct from a C function on this architecture.
func structsByValue() bool { return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" }

// Functions lists the SDL2 functions the binding calls, for the spike report.
func Functions() []string {
	var a api
	t := a.table()
	names := make([]string, len(t))
	for i, b := range t {
		names[i] = b.name
	}
	return names
}

// MinimumVersion is the newest release among the functions the binding must have: an SDL2 older than this
// cannot be used. Optional functions (SDL_GameControllerGetType, 2.0.12) do not count.
func MinimumVersion() Version {
	var a api
	min := Version{}
	for _, b := range a.table() {
		if !b.optional && b.since.AtLeast(min) {
			min = b.since
		}
	}
	return min
}
