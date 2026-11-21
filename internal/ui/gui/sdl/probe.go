//go:build gui

package sdl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
)

// ProbeOptions tune the probe.
type ProbeOptions struct {
	// Driver forces SDL_VIDEODRIVER (for example "dummy") through a hint. Empty leaves SDL to choose, and
	// to read the SDL_VIDEODRIVER environment variable.
	Driver string
}

// Report is what a probe learned, in the order it learned it.
type Report struct {
	Version   Version
	Minimum   Version
	Target    Version
	Driver    string
	Functions []string
	Steps     []string // the checks that passed
}

// ProbeError says which step of the probe failed.
type ProbeError struct {
	Step string
	Err  error
}

func (e *ProbeError) Error() string { return "probe " + e.Step + ": " + e.Err.Error() }
func (e *ProbeError) Unwrap() error { return e.Err }

// mapping is a made-up line in the GameControllerDB format, to prove SDL_GameControllerAddMapping works.
const mapping = "03000000deadbeef0000000000000000,Spike Pad,a:b0,b:b1,x:b2,y:b3,back:b4,start:b6,leftshoulder:b9,rightshoulder:b10,dpup:h0.1,dpdown:h0.4,dpleft:h0.8,dpright:h0.2,platform:Linux,"

const (
	surfaceW, surfaceH = 64, 48

	// What is drawn, and the pixel it must read back as (ARGB8888: 0xAARRGGBB).
	pixelBlack = 0xFF000000
	pixelRect  = 0xFF0AC81E // rect filled with (10, 200, 30)
	pixelAtlas = 0xFF3C78B4 // every pixel of the 4x4 atlas is (60, 120, 180)
	pixelScale = 0xFFC8140A // rect drawn under SetScale(2, 2) is (200, 20, 10)
)

// Probe runs the headless check of docs/contracts/cli/gui.md section 2.2, in order: load and init, the
// version, the controller mapping, a window and a renderer, an RGBA atlas texture, a draw, a pixel read
// back (which also proves the float arguments), and an event pushed and polled with the same fields.
// It locks the calling goroutine to its OS thread, as every SDL call must come from one thread.
func Probe(opts ProbeOptions) (Report, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	rep := Report{Minimum: MinimumVersion(), Target: TargetVersion, Functions: Functions()}
	fail := func(step string, err error) (Report, error) { return rep, &ProbeError{Step: step, Err: err} }
	pass := func(step string) { rep.Steps = append(rep.Steps, step) }

	lib, err := Open()
	if err != nil {
		return fail("load", err)
	}
	pass("load")

	rep.Version = lib.Version()
	if !rep.Version.AtLeast(rep.Minimum) {
		return fail("version", fmt.Errorf("%w: have %s, need %s", ErrTooOld, rep.Version, rep.Minimum))
	}
	pass("version")

	if opts.Driver != "" {
		lib.SetHint("SDL_VIDEODRIVER", opts.Driver)
	}
	if err := lib.Init(InitVideo | InitGameController); err != nil {
		return fail("init", err)
	}
	defer lib.Quit()
	rep.Driver = lib.CurrentVideoDriver()
	pass("init")

	if _, err := lib.AddControllerMapping(mapping); err != nil {
		return fail("mappings", err)
	}
	pass("mappings")

	win, err := lib.CreateWindow("mothdex-gui probe", surfaceW, surfaceH, WindowHidden)
	if err != nil {
		return fail("window", err)
	}
	defer lib.DestroyWindow(win)
	ren, err := lib.CreateRenderer(win, RendererSoftware)
	if err != nil {
		return fail("renderer", err)
	}
	defer lib.DestroyRenderer(ren)
	pass("window and renderer")

	atlas, err := uploadAtlas(lib, ren)
	if err != nil {
		return fail("atlas", err)
	}
	defer lib.DestroyTexture(atlas)
	pass("atlas texture")

	if err := draw(lib, ren, atlas); err != nil {
		return fail("draw", err)
	}
	pass("draw")

	pix, err := readBack(lib, ren)
	if err != nil {
		return fail("read pixels", err)
	}
	for _, c := range []struct {
		name string
		x, y int
		want uint32
	}{
		{"filled rect", 12, 12, pixelRect},
		{"copied atlas", 40, 16, pixelAtlas},
		{"background", 1, 1, pixelBlack},
	} {
		if got := pixelAt(pix, c.x, c.y); got != c.want {
			return fail("read pixels", fmt.Errorf("%s at (%d,%d) is %#08x, want %#08x", c.name, c.x, c.y, got, c.want))
		}
	}
	pass("pixels read back")

	if err := checkScale(lib, ren); err != nil {
		return fail("render scale", err)
	}
	pass("render scale (float arguments)")

	if err := roundTripEvent(lib); err != nil {
		return fail("events", err)
	}
	pass("event pushed and polled")
	return rep, nil
}

// uploadAtlas makes a 4x4 texture of one colour, the smallest stand-in for a glyph atlas.
func uploadAtlas(lib *Library, ren Renderer) (Texture, error) {
	const side int32 = 4
	t, err := lib.CreateTexture(ren, PixelFormatARGB8888, TextureAccessStatic, side, side)
	if err != nil {
		return 0, err
	}
	px := make([]byte, side*side*4)
	for i := 0; i < len(px); i += 4 {
		binary.LittleEndian.PutUint32(px[i:], pixelAtlas)
	}
	if err := lib.UpdateTexture(t, nil, px, side*4); err != nil {
		lib.DestroyTexture(t)
		return 0, err
	}
	return t, nil
}

func draw(lib *Library, ren Renderer, atlas Texture) error {
	if err := lib.SetDrawColor(ren, 0, 0, 0, 255); err != nil {
		return err
	}
	if err := lib.Clear(ren); err != nil {
		return err
	}
	if err := lib.SetDrawColor(ren, 10, 200, 30, 255); err != nil {
		return err
	}
	if err := lib.FillRect(ren, Rect{X: 8, Y: 8, W: 16, H: 16}); err != nil {
		return err
	}
	return lib.Copy(ren, atlas, nil, &Rect{X: 32, Y: 8, W: 16, H: 16})
}

func readBack(lib *Library, ren Renderer) ([]byte, error) {
	buf := make([]byte, surfaceW*surfaceH*4)
	if err := lib.ReadPixels(ren, nil, PixelFormatARGB8888, buf, surfaceW*4); err != nil {
		return nil, err
	}
	return buf, nil
}

func pixelAt(buf []byte, x, y int) uint32 {
	return binary.LittleEndian.Uint32(buf[(y*surfaceW+x)*4:])
}

// checkScale draws a 4x4 rect at (4,4) under a render scale of 2: it must land on device pixels 8 to 15.
// If the two floats were lost on the way to C the scale would stay 1 and the pixel would stay black.
func checkScale(lib *Library, ren Renderer) error {
	if err := lib.SetDrawColor(ren, 0, 0, 0, 255); err != nil {
		return err
	}
	if err := lib.Clear(ren); err != nil {
		return err
	}
	if err := lib.SetScale(ren, 2, 2); err != nil {
		return err
	}
	if err := lib.SetDrawColor(ren, 200, 20, 10, 255); err != nil {
		return err
	}
	if err := lib.FillRect(ren, Rect{X: 4, Y: 4, W: 4, H: 4}); err != nil {
		return err
	}
	if err := lib.SetScale(ren, 1, 1); err != nil {
		return err
	}
	pix, err := readBack(lib, ren)
	if err != nil {
		return err
	}
	if got := pixelAt(pix, 10, 10); got != pixelScale {
		return fmt.Errorf("pixel (10,10) under scale 2 is %#08x, want %#08x: the float arguments did not arrive", got, uint32(pixelScale))
	}
	if got := pixelAt(pix, 6, 6); got != pixelBlack {
		return fmt.Errorf("pixel (6,6) is %#08x, want black: the rect was not scaled", got)
	}
	return nil
}

// roundTripEvent pushes a synthetic controller button event and reads it back with the same fields,
// which proves the SDL_Event layout.
func roundTripEvent(lib *Library) error {
	const which, button, state = 7, 2, 1
	out := NewEvent()
	out.SetControllerButton(EventControllerButtonDown, which, button, state)
	if err := lib.PushEvent(out); err != nil {
		return err
	}
	in := NewEvent()
	for i := 0; i < 64; i++ { // other events (window shown...) may come first
		if !lib.PollEvent(in) {
			break
		}
		if in.Type() != EventControllerButtonDown {
			continue
		}
		got := in.ControllerButton()
		if got.Which != which || got.Button != button || got.State != state {
			return fmt.Errorf("event came back as which=%d button=%d state=%d, want %d %d %d", got.Which, got.Button, got.State, which, button, state)
		}
		return nil
	}
	return errors.New("the pushed event never came back from the queue")
}
