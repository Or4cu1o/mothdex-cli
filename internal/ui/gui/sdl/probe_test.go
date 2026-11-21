//go:build gui

package sdl

import (
	"errors"
	"os"
	"testing"
	"unsafe"
)

// skipWithoutSDL skips a test that needs a system SDL2, except when GUI_REQUIRE_SDL=1: the CI jobs that
// install libsdl2 set it, so a broken install fails the job instead of passing as a skip.
func skipWithoutSDL(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrNotFound) {
		return
	}
	if os.Getenv("GUI_REQUIRE_SDL") == "1" {
		t.Fatalf("GUI_REQUIRE_SDL=1 and there is no system SDL2: %v", err)
	}
	t.Skipf("no system SDL2 here: %v", err)
}

// TestSmokeProbe is the headless check of docs/contracts/cli/gui.md section 2.2. It needs a system SDL2
// and runs on the dummy video driver, so it needs no display. Without SDL2 it is skipped: the CI jobs of
// the GUI smoke test install libsdl2 and run it on each architecture.
func TestSmokeProbe(t *testing.T) {
	rep, err := Probe(ProbeOptions{Driver: "dummy"})
	skipWithoutSDL(t, err)
	if err != nil {
		t.Fatalf("%v\nsteps that passed: %v", err, rep.Steps)
	}
	if rep.Driver != "dummy" {
		t.Errorf("video driver %q, want dummy", rep.Driver)
	}
	if want := 8; len(rep.Steps) < want {
		t.Errorf("only %d steps passed: %v", len(rep.Steps), rep.Steps)
	}
	t.Logf("SDL %s, driver %s, %d functions, steps: %v", rep.Version, rep.Driver, len(rep.Functions), rep.Steps)
}

// With a library that cannot be found the probe must say so with the error the exit code is built on.
func TestProbeReportsAMissingLibraryAsNotFound(t *testing.T) {
	if _, err := Open(); err == nil {
		t.Skip("a system SDL2 exists here")
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open without SDL2 returned %v, want ErrNotFound", err)
	}
	_, err := Probe(ProbeOptions{})
	var pe *ProbeError
	if !errors.As(err, &pe) || pe.Step != "load" || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Probe error %v", err)
	}
}

// With a real library: the sized calls refuse a short buffer without calling SDL, and accept a good one.
func TestReadPixelsAndUpdateTextureRefuseShortBuffers(t *testing.T) {
	lib, err := Open()
	skipWithoutSDL(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if !lib.SetHint("SDL_VIDEODRIVER", "dummy") {
		t.Log("the SDL_VIDEODRIVER hint was not set")
	}
	if err := lib.Init(InitVideo); err != nil {
		t.Skipf("no usable video driver: %v", err)
	}
	defer lib.Quit()
	win, err := lib.CreateWindow("buffers", 32, 32, WindowHidden)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.DestroyWindow(win)
	ren, err := lib.CreateRenderer(win, RendererSoftware)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.DestroyRenderer(ren)

	if err := lib.ReadPixels(ren, Rect{W: 32, H: 32}, PixelFormatARGB8888, make([]byte, 32*32*4-1), 32*4); err == nil {
		t.Error("ReadPixels accepted a buffer one byte short")
	}
	if err := lib.ReadPixels(ren, Rect{W: 32, H: 32}, PixelFormatARGB8888, make([]byte, 32*32*4), 32*4); err != nil {
		t.Errorf("ReadPixels refused a good buffer: %v", err)
	}
	tex, err := lib.CreateTexture(ren, PixelFormatARGB8888, TextureAccessStatic, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer lib.DestroyTexture(tex)
	if err := lib.UpdateTexture(tex, nil, make([]byte, 4*4*4-1), 16); err == nil {
		t.Error("UpdateTexture accepted a buffer one byte short")
	}
	if err := lib.UpdateTexture(tex, &Rect{X: 2, Y: 2, W: 4, H: 4}, make([]byte, 64), 16); err == nil {
		t.Error("UpdateTexture accepted a rect outside the texture")
	}
	if err := lib.UpdateTexture(tex, nil, make([]byte, 4*4*4), 16); err != nil {
		t.Errorf("UpdateTexture refused a good buffer: %v", err)
	}
}

// A buffer that is too small must be refused before SDL is called: with a fake api, SDL being reached is
// the failure (PR-69 review, Ensaio V1 and Sentinela M1). No SDL2 is needed.
func TestShortBuffersNeverReachSDL(t *testing.T) {
	reached := 0
	lib := &Library{}
	lib.a.getError = func() string { return "" }
	lib.a.renderReadPixels = func(uintptr, *Rect, uint32, unsafe.Pointer, int32) int32 { reached++; return 0 }
	lib.a.updateTexture = func(uintptr, *Rect, unsafe.Pointer, int32) int32 { reached++; return 0 }
	tex := Texture{handle: 1, w: 4, h: 4, bytes: 4}

	refused := []struct {
		name string
		err  error
	}{
		{"ReadPixels, one byte short", lib.ReadPixels(1, Rect{W: 4, H: 4}, PixelFormatARGB8888, make([]byte, 63), 16)},
		{"ReadPixels, pitch below the row", lib.ReadPixels(1, Rect{W: 4, H: 4}, PixelFormatARGB8888, make([]byte, 256), 15)},
		{"ReadPixels, empty rect", lib.ReadPixels(1, Rect{}, PixelFormatARGB8888, make([]byte, 256), 16)},
		{"ReadPixels, format without a pixel size", lib.ReadPixels(1, Rect{W: 4, H: 4}, 0x32315659, make([]byte, 256), 16)},
		{"UpdateTexture, one byte short", lib.UpdateTexture(tex, nil, make([]byte, 63), 16)},
		{"UpdateTexture, pitch below the row", lib.UpdateTexture(tex, nil, make([]byte, 256), 15)},
		{"UpdateTexture, rect outside", lib.UpdateTexture(tex, &Rect{X: 2, Y: 2, W: 4, H: 4}, make([]byte, 256), 16)},
		{"UpdateTexture, negative origin", lib.UpdateTexture(tex, &Rect{X: -1, W: 2, H: 2}, make([]byte, 256), 16)},
	}
	for _, r := range refused {
		if r.err == nil {
			t.Errorf("%s: accepted", r.name)
		}
	}
	if reached != 0 {
		t.Fatalf("SDL was called %d times with a buffer that was too small", reached)
	}

	if err := lib.ReadPixels(1, Rect{W: 4, H: 4}, PixelFormatARGB8888, make([]byte, 64), 16); err != nil {
		t.Errorf("a good ReadPixels was refused: %v", err)
	}
	if err := lib.UpdateTexture(tex, &Rect{X: 1, Y: 1, W: 2, H: 2}, make([]byte, 16+8), 16); err != nil {
		t.Errorf("a good UpdateTexture was refused: %v", err)
	}
	if reached != 2 {
		t.Fatalf("SDL was reached %d times for two good calls", reached)
	}
}
