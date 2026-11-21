//go:build gui

package sdl

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Errors that tell the caller what is wrong with the SDL2 on this machine (exit code 1 of the GUI).
var (
	ErrNotFound     = errors.New("SDL2 was not found")
	ErrIncompatible = errors.New("SDL2 lacks a function this program needs")
	ErrTooOld       = errors.New("SDL2 is too old")
	ErrNoDisplay    = errors.New("SDL2 has no video device")
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
}

// binding is one function to look up: its C name, the SDL2 release that introduced it, and where the
// Go function goes.
type binding struct {
	name  string
	since Version
	fn    any
}

func (a *api) table() []binding {
	v200 := Version{2, 0, 0}
	return []binding{
		{"SDL_GetVersion", v200, &a.getVersion},
		{"SDL_Init", v200, &a.init},
		{"SDL_Quit", v200, &a.quit},
		{"SDL_GetError", v200, &a.getError},
		{"SDL_SetHint", v200, &a.setHint},
		{"SDL_GetCurrentVideoDriver", v200, &a.getCurrentVideoDriver},
		{"SDL_CreateWindow", v200, &a.createWindow},
		{"SDL_DestroyWindow", v200, &a.destroyWindow},
		{"SDL_CreateRenderer", v200, &a.createRenderer},
		{"SDL_DestroyRenderer", v200, &a.destroyRenderer},
		{"SDL_SetRenderDrawColor", v200, &a.setRenderDrawColor},
		{"SDL_RenderClear", v200, &a.renderClear},
		{"SDL_RenderFillRect", v200, &a.renderFillRect},
		{"SDL_RenderCopy", v200, &a.renderCopy},
		{"SDL_RenderPresent", v200, &a.renderPresent},
		{"SDL_RenderReadPixels", v200, &a.renderReadPixels},
		{"SDL_RenderSetScale", v200, &a.renderSetScale},
		{"SDL_CreateTexture", v200, &a.createTexture},
		{"SDL_UpdateTexture", v200, &a.updateTexture},
		{"SDL_DestroyTexture", v200, &a.destroyTexture},
		{"SDL_PollEvent", v200, &a.pollEvent},
		{"SDL_PushEvent", v200, &a.pushEvent},
		{"SDL_GameControllerAddMapping", v200, &a.gameControllerAddMapping},
	}
}

// bind looks every function up in the loaded library. A missing symbol makes purego panic; it becomes
// ErrIncompatible here.
func (a *api) bind(handle uintptr) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrIncompatible, r)
		}
	}()
	for _, b := range a.table() {
		purego.RegisterLibFunc(b.fn, handle, b.name)
	}
	return nil
}

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

// MinimumVersion is the newest release among the functions the binding calls: an SDL2 older than this
// cannot be used.
func MinimumVersion() Version {
	var a api
	min := Version{}
	for _, b := range a.table() {
		if b.since.AtLeast(min) {
			min = b.since
		}
	}
	return min
}
