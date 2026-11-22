//go:build gui

package sdl

import (
	"errors"
	"fmt"
	"unsafe"
)

// Window is an SDL_Window handle: an opaque pointer owned by SDL, zero for "none".
type Window uintptr

// Renderer is an SDL_Renderer handle, zero for "none".
type Renderer uintptr

// Texture is an SDL_Texture with the size and pixel size it was created with. The zero value is "none".
type Texture struct {
	handle uintptr
	w, h   int32
	bytes  int32 // bytes per pixel
}

// Library is the SDL2 library bound through purego. All its methods must be called from the thread that
// called Init (the main OS thread of the program, see runtime.LockOSThread).
type Library struct{ a api }

// openLib is openLibrary, replaceable by a test.
var openLib = openLibrary

// Open loads the system SDL2 and binds the functions. It wraps ErrNotFound when the library is not
// there, ErrSearchPath when the DLL search path could not be restricted first (Windows), and ErrIncompatible
// when it lacks a function.
func Open() (*Library, error) {
	h, err := openLib()
	if errors.Is(err, ErrSearchPath) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	l := &Library{}
	if err := l.a.bind(h); err != nil {
		return nil, err
	}
	return l, nil
}

// Version is the version of the SDL2 that was loaded.
func (l *Library) Version() Version {
	var v Version
	l.a.getVersion(&v)
	return v
}

// sdlError is the last SDL error, as a Go error with the context of the call that failed.
func (l *Library) sdlError(call string) error {
	msg := l.a.getError()
	if msg == "" {
		msg = "unknown error"
	}
	return fmt.Errorf("%s: %s", call, msg)
}

// Init starts the given subsystems. A machine with no video device gives ErrNoDisplay.
func (l *Library) Init(flags uint32) error {
	if l.a.init(flags) == 0 {
		return nil
	}
	err := l.sdlError("SDL_Init")
	if flags&InitVideo != 0 {
		return fmt.Errorf("%w: %w", ErrNoDisplay, err)
	}
	return err
}

// Quit shuts SDL down.
func (l *Library) Quit() { l.a.quit() }

// SetHint sets an SDL hint such as SDL_VIDEODRIVER before Init.
func (l *Library) SetHint(name, value string) bool { return l.a.setHint(name, value) != 0 }

// CurrentVideoDriver is the name of the video driver in use ("dummy", "x11", "KMSDRM"...).
func (l *Library) CurrentVideoDriver() string { return l.a.getCurrentVideoDriver() }

// CreateWindow makes a window; SDL positions it.
func (l *Library) CreateWindow(title string, w, h int32, flags uint32) (Window, error) {
	const undefined = 0x1FFF0000 // SDL_WINDOWPOS_UNDEFINED
	win := Window(l.a.createWindow(title, undefined, undefined, w, h, flags))
	if win == 0 {
		return 0, l.sdlError("SDL_CreateWindow")
	}
	return win, nil
}

// DestroyWindow frees a window.
func (l *Library) DestroyWindow(w Window) { l.a.destroyWindow(uintptr(w)) }

// CreateRenderer makes a renderer for a window. flags is a mask of the SDL_RENDERER_* values.
func (l *Library) CreateRenderer(w Window, flags uint32) (Renderer, error) {
	r := Renderer(l.a.createRenderer(uintptr(w), -1, flags))
	if r == 0 {
		return 0, l.sdlError("SDL_CreateRenderer")
	}
	return r, nil
}

// DestroyRenderer frees a renderer.
func (l *Library) DestroyRenderer(r Renderer) { l.a.destroyRenderer(uintptr(r)) }

func (l *Library) check(rc int32, call string) error {
	if rc < 0 {
		return l.sdlError(call)
	}
	return nil
}

// SetDrawColor sets the colour of the next Clear and FillRect.
func (l *Library) SetDrawColor(r Renderer, red, green, blue, alpha uint8) error {
	return l.check(l.a.setRenderDrawColor(uintptr(r), red, green, blue, alpha), "SDL_SetRenderDrawColor")
}

// Clear fills the whole target with the draw colour.
func (l *Library) Clear(r Renderer) error {
	return l.check(l.a.renderClear(uintptr(r)), "SDL_RenderClear")
}

// FillRect fills a rectangle with the draw colour.
func (l *Library) FillRect(r Renderer, rect Rect) error {
	return l.check(l.a.renderFillRect(uintptr(r), &rect), "SDL_RenderFillRect")
}

// Copy draws a texture; a nil src is the whole texture and a nil dst the whole target.
func (l *Library) Copy(r Renderer, t Texture, src, dst *Rect) error {
	return l.check(l.a.renderCopy(uintptr(r), t.handle, src, dst), "SDL_RenderCopy")
}

// Present shows what was drawn.
func (l *Library) Present(r Renderer) { l.a.renderPresent(uintptr(r)) }

// SetScale sets the render scale. The two floats cross the boundary in float registers, which is why the
// spike checks the result with ReadPixels.
func (l *Library) SetScale(r Renderer, sx, sy float32) error {
	return l.check(l.a.renderSetScale(uintptr(r), sx, sy), "SDL_RenderSetScale")
}

// bytesPerPixel is the size of one pixel of an SDL_PIXELFORMAT value: the low byte of a packed or array
// format. Formats that carry no such size (the FourCC ones) give 0 and are refused.
func bytesPerPixel(format uint32) int32 {
	n := int32(format & 0xFF)
	if n < 1 || n > 8 {
		return 0
	}
	return n
}

// checkBuffer says whether a buffer is big enough for SDL to read or write a w x h block of pixels of
// bpp bytes with a row stride of pitch: SDL touches pitch*(h-1)+w*bpp bytes. A shorter slice would make
// SDL read or write past the end of the Go heap object, so every call that hands SDL a buffer checks first.
// The arithmetic is in int64, so int32 inputs cannot overflow it.
func checkBuffer(w, h, pitch, bpp int32, have int) error {
	switch {
	case w <= 0 || h <= 0:
		return fmt.Errorf("pixel block %dx%d is empty or negative", w, h)
	case bpp < 1:
		return errors.New("unknown pixel size")
	}
	row := int64(w) * int64(bpp)
	if int64(pitch) < row {
		return fmt.Errorf("pitch %d is smaller than a row of %d pixels (%d bytes)", pitch, w, row)
	}
	need := int64(pitch)*int64(h-1) + row
	if int64(have) < need {
		return fmt.Errorf("buffer of %d bytes is too small for %dx%d pixels with pitch %d (needs %d)", have, w, h, pitch, need)
	}
	return nil
}

// ReadPixels copies the pixels of rect from the render target into buf, in the given pixel format. The
// rect is required, not optional: SDL would otherwise read the whole target, whose size the caller does
// not state. buf must hold pitch*(h-1)+w*bytes bytes; a shorter one is refused before SDL is called.
func (l *Library) ReadPixels(r Renderer, rect Rect, format uint32, buf []byte, pitch int32) error {
	if err := checkBuffer(rect.W, rect.H, pitch, bytesPerPixel(format), len(buf)); err != nil {
		return fmt.Errorf("ReadPixels: %w", err)
	}
	rc := l.a.renderReadPixels(uintptr(r), &rect, format, unsafe.Pointer(&buf[0]), pitch) //nolint:gosec // buf was checked to hold every byte SDL touches
	return l.check(rc, "SDL_RenderReadPixels")
}

// CreateTexture makes a texture of the given pixel format, access mode and size. The returned value
// remembers the size, so UpdateTexture can check the buffer it is given.
func (l *Library) CreateTexture(r Renderer, format uint32, access, w, h int32) (Texture, error) {
	bpp := bytesPerPixel(format)
	if w <= 0 || h <= 0 || bpp == 0 {
		return Texture{}, fmt.Errorf("CreateTexture: bad size %dx%d or pixel format %#x", w, h, format)
	}
	p := l.a.createTexture(uintptr(r), format, access, w, h)
	if p == 0 {
		return Texture{}, l.sdlError("SDL_CreateTexture")
	}
	return Texture{handle: p, w: w, h: h, bytes: bpp}, nil
}

// UpdateTexture uploads pixels to a texture. A nil rect is the whole texture. The rect must lie inside the
// texture, and pixels must hold pitch*(h-1)+w*bytes bytes for the area: SDL reads that much, so a shorter
// slice is refused before SDL is called.
func (l *Library) UpdateTexture(t Texture, rect *Rect, pixels []byte, pitch int32) error {
	area := Rect{W: t.w, H: t.h}
	if rect != nil {
		area = *rect
		if area.X < 0 || area.Y < 0 || int64(area.X)+int64(area.W) > int64(t.w) || int64(area.Y)+int64(area.H) > int64(t.h) {
			return fmt.Errorf("UpdateTexture: rect %+v lies outside the %dx%d texture", area, t.w, t.h)
		}
	}
	if err := checkBuffer(area.W, area.H, pitch, t.bytes, len(pixels)); err != nil {
		return fmt.Errorf("UpdateTexture: %w", err)
	}
	rc := l.a.updateTexture(t.handle, rect, unsafe.Pointer(&pixels[0]), pitch) //nolint:gosec // pixels was checked to hold every byte SDL reads
	return l.check(rc, "SDL_UpdateTexture")
}

// DestroyTexture frees a texture.
func (l *Library) DestroyTexture(t Texture) { l.a.destroyTexture(t.handle) }

// PollEvent takes the next event off the queue into e. It reports false when the queue is empty.
func (l *Library) PollEvent(e *Event) bool { return l.a.pollEvent(e) != 0 }

// PushEvent puts an event on the queue. SDL may filter it out, which is an error here.
func (l *Library) PushEvent(e *Event) error {
	switch rc := l.a.pushEvent(e); {
	case rc < 0:
		return l.sdlError("SDL_PushEvent")
	case rc == 0:
		return errors.New("SDL_PushEvent: the event was filtered out")
	}
	return nil
}

// AddControllerMapping adds one line of the game controller database. It reports whether the mapping
// was new (true) or replaced an earlier one (false).
func (l *Library) AddControllerMapping(line string) (added bool, err error) {
	switch rc := l.a.gameControllerAddMapping(line); {
	case rc < 0:
		return false, l.sdlError("SDL_GameControllerAddMapping")
	default:
		return rc == 1, nil
	}
}
