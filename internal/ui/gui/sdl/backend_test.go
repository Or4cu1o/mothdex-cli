//go:build gui

package sdl

import (
	"context"
	"errors"
	"image"
	"image/color"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// These tests need a system SDL2 and run on the dummy video driver with the software renderer: no display and
// no pad. They are the headless render check of docs/ux/gui.md section 10 (PR 1) and skip where SDL2 is
// missing, unless GUI_REQUIRE_SDL=1 (the CI jobs that install libsdl2).

// Colors the model draws with (docs/ux/gui.md section 1.5).
var (
	colorBG        = [3]uint8{0x0D, 0x0D, 0x10}
	colorSurface   = [3]uint8{0x1C, 0x1C, 0x1E}
	colorFocusBG   = [3]uint8{0x2A, 0x2E, 0x44}
	colorFocusRing = [3]uint8{0xA8, 0xAC, 0xC2}
)

var testEpoch = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// headless opens a hidden software window of w x h on the dummy driver, or skips the test.
func headless(t *testing.T, w, h int) (*Backend, *text.Font) {
	t.Helper()
	return headlessWith(t, w, h, true)
}

// headlessWith is headless with or without the display font in the backend.
func headlessWith(t *testing.T, w, h int, withDisplay bool) (*Backend, *text.Font) {
	t.Helper()
	runtime.LockOSThread() // SDL calls stay on one OS thread
	t.Cleanup(runtime.UnlockOSThread)
	lib, err := Open()
	skipWithoutSDL(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if v := lib.Version(); !v.AtLeast(MinimumVersion()) {
		t.Fatalf("SDL %s is older than the minimum %s", v, MinimumVersion())
	}
	font, err := text.Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	display, err := text.Parse(assets.Handjet)
	if err != nil {
		t.Fatal(err)
	}
	if !withDisplay {
		display = nil
	}
	be, err := OpenBackend(lib, font, BackendOptions{Driver: "dummy", Windowed: true, Width: w, Height: h, Hidden: true, Software: true, Display: display})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(be.Close)
	return be, font
}

func guiOptions(t *testing.T, font *text.Font, svc gui.Service, w, h int) gui.Options {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	display, err := text.Parse(assets.Handjet)
	if err != nil {
		t.Fatal(err)
	}
	return gui.Options{Service: svc, T: bundle.New("en_us"), Font: font, Display: display, Clock: gui.SystemClock(), DataDir: t.TempDir(), Version: "0.3.0", Width: w, Height: h, Safe: gui.AutoSafeArea}
}

// homeModel returns the model on Home for a display of w x h, with the service answers applied.
func homeModel(t *testing.T, font *text.Font, w, h int, svc gui.Service) gui.Model {
	t.Helper()
	m, cmds := gui.New(guiOptions(t, font, svc, w, h), testEpoch)
	for _, cmd := range cmds {
		m, _ = m.Update(cmd(), testEpoch)
	}
	return m
}

func layoutOf(m gui.Model) gdraw.Layout {
	mt := m.Metrics()
	return gdraw.Layout{W: mt.W, H: mt.H, Scale: mt.Scale, Fit: mt.FitLogical}
}

func expectPixel(t *testing.T, be *Backend, x, y int, want [3]uint8, what string) {
	t.Helper()
	r, g, b, _, err := be.Pixel(x, y)
	if err != nil {
		t.Fatal(err)
	}
	if got := [3]uint8{r, g, b}; got != want {
		t.Errorf("%s: pixel (%d,%d) is %v, want %v", what, x, y, got, want)
	}
}

// A model given a display face and a backend without one is a wiring mistake: the frame fails loudly instead of drawing the
// labels in the body face at the display size.
func TestADisplayRunWithoutADisplayFontInTheBackendIsRefused(t *testing.T) {
	be, _ := headlessWith(t, 640, 480, false)
	var list gdraw.List
	list.Reset(640, 480)
	list.Fill(gdraw.Rect{W: 640, H: 480}, gdraw.Color{R: colorBG[0], G: colorBG[1], B: colorBG[2], A: 0xff})
	list.DisplayText(10, 40, 35, gdraw.Color{R: 255, G: 255, B: 255, A: 255}, "mothdex")
	if err := be.Draw(gdraw.Layout{W: 640, H: 480, Scale: 1}, &list); !errors.Is(err, errNoDisplayFont) {
		t.Fatalf("Draw returned %v, want errNoDisplayFont", err)
	}
	list.Reset(640, 480)
	list.Text(10, 40, 20, gdraw.Color{R: 255, G: 255, B: 255, A: 255}, "mothdex")
	if err := be.Draw(gdraw.Layout{W: 640, H: 480, Scale: 1}, &list); err != nil {
		t.Fatalf("a body run needs no display font: %v", err)
	}
}

// Home drawn by the real renderer: background, the safe margin, the top bar, the focused tile with its ring
// and some text, at the three sizes of the done criterion.
func TestHomeRendersAtThreeSizes(t *testing.T) {
	for _, size := range [][2]int{{640, 480}, {1280, 720}, {1920, 1080}} {
		w, h := size[0], size[1]
		t.Run(strconv.Itoa(w)+"x"+strconv.Itoa(h), func(t *testing.T) {
			be, font := headless(t, w, h)
			if gw, gh := be.Size(); gw != w || gh != h {
				t.Fatalf("window is %dx%d, want %dx%d", gw, gh, w, h)
			}
			m := homeModel(t, font, w, h, guitest.New())
			var list gdraw.List
			m.Render(testEpoch, &list)
			if err := be.Draw(layoutOf(m), &list); err != nil {
				t.Fatal(err)
			}

			safe := m.Metrics().Safe
			expectPixel(t, be, w-2, h/2, colorBG, "the right edge")
			if safe.X > 0 {
				expectPixel(t, be, 3, 3, colorBG, "inside the margin")
				expectPixel(t, be, safe.X-2, safe.Y+6, colorBG, "just left of the top bar")
			}
			expectPixel(t, be, safe.X+safe.W-8, safe.Y+4, colorSurface, "the top bar")

			ringColor := gdraw.Color{R: colorFocusRing[0], G: colorFocusRing[1], B: colorFocusRing[2], A: 0xff}
			var ring, fill gdraw.Op
			for i, op := range list.Ops {
				if op.Kind == gdraw.KindRect && op.Color == ringColor {
					ring, fill = op, list.Ops[i+1]
					break
				}
			}
			if ring.Kind != gdraw.KindRect {
				t.Fatal("no focus ring in the draw list")
			}
			expectPixel(t, be, ring.Rect.X+ring.Rect.W/2, ring.Rect.Y, colorFocusRing, "the ring above the focused tile")
			expectPixel(t, be, fill.Rect.X+fill.Rect.W/2, fill.Rect.Y+3, colorFocusBG, "the focused tile")

			// Text: the label of the focused tile puts light pixels in its middle.
			x0, y0 := fill.Rect.X+fill.Rect.W/4, fill.Rect.Y+fill.Rect.H/3
			bw, bh := fill.Rect.W/2, fill.Rect.H/3
			buf := make([]byte, bw*bh*4)
			area := Rect{X: int32(x0), Y: int32(y0), W: int32(bw), H: int32(bh)}                           //nolint:gosec // small test coordinates
			if err := be.lib.ReadPixels(be.ren, area, PixelFormatARGB8888, buf, int32(bw*4)); err != nil { //nolint:gosec // small test sizes
				t.Fatal(err)
			}
			light := 0
			for i := 0; i < len(buf); i += 4 {
				if buf[i+2] > 0xC0 && buf[i+1] > 0xC0 { // R and G of a near-white glyph pixel
					light++
				}
			}
			if light < 20 {
				t.Errorf("only %d light pixels where the label should be", light)
			}
		})
	}
}

// A display below 640x480 draws the 640x480 layout letterboxed: the bars are the background, and the layout
// is scaled, not cropped.
func TestASmallDisplayIsLetterboxedNotCropped(t *testing.T) {
	be, font := headless(t, 480, 320)
	m := homeModel(t, font, 480, 320, guitest.New())
	if !m.Metrics().FitLogical || m.Metrics().W != 640 || m.Metrics().H != 480 {
		t.Fatalf("metrics %+v", m.Metrics())
	}
	var list gdraw.List
	m.Render(testEpoch, &list)
	if err := be.Draw(layoutOf(m), &list); err != nil {
		t.Fatal(err)
	}
	// 640x480 in 480x320 leaves bars of about 26 px at each side; Clear paints them with the background, but
	// SDL_RenderReadPixels only reads inside the viewport, so the check is on the scaled layout itself: the
	// top bar spans its whole width.
	expectPixel(t, be, 240, 3, colorSurface, "the top bar, scaled")
	expectPixel(t, be, 440, 3, colorSurface, "the top bar, near its right end")
}

// Events pushed into SDL come back through Wait and Poll as the loop's events, and Wake ends a Wait.
func TestEventsRoundTripThroughSDL(t *testing.T) {
	be, _ := headless(t, 640, 480)
	push := func(fill func(e *Event)) {
		e := NewEvent()
		fill(e)
		if err := be.lib.PushEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	push(func(e *Event) { *e.Keyboard() = KeyboardEvent{Type: EventKeyDown, Sym: KeycodeEscape} })
	push(func(e *Event) { *e.Keyboard() = KeyboardEvent{Type: EventKeyDown, Sym: 'x', Repeat: 1} }) // SDL's own repeat: dropped
	push(func(e *Event) { *e.Keyboard() = KeyboardEvent{Type: EventKeyUp, Sym: 'x'} })
	push(func(e *Event) { e.SetControllerButton(EventControllerButtonDown, 0, ControllerButtonA, 1) })
	push(func(e *Event) {
		*e.ControllerAxis() = ControllerAxisEvent{Type: EventControllerAxisMotion, Axis: ControllerAxisLeftX, Value: 20000}
	})
	push(func(e *Event) { e.SetControllerButton(EventControllerButtonDown, 0, 8, 1) }) // a stick click: not used
	push(func(e *Event) {
		*e.TextInput() = TextInputEvent{Type: EventTextInput}
		copy(e.TextInput().Text[:], "ñ")
	})
	push(func(e *Event) { e.SetType(EventQuit) })

	want := []input.Incoming{
		{Kind: input.IncomingInput, Input: input.Event{Kind: input.KeyPress, Key: input.KeyEscape}},
		{Kind: input.IncomingInput, Input: input.Event{Kind: input.KeyRelease, Key: input.KeyRune, Rune: 'x'}},
		{Kind: input.IncomingInput, Input: input.Event{Kind: input.ButtonPress, Button: input.South}},
		{Kind: input.IncomingInput, Input: input.Event{Kind: input.AxisMotion, Axis: input.LeftX, Value: 20000}},
		{Kind: input.IncomingText, Text: "ñ"},
		{Kind: input.IncomingQuit},
	}
	first, ok := be.Wait(2 * time.Second)
	if !ok {
		t.Fatal("no event came back")
	}
	var got []input.Incoming
	// The start-up list of joysticks is reported even when it is empty (so the GUI knows no pad is there): it is
	// not one of the events this test pushed.
	if first.Kind != input.IncomingPadList {
		got = append(got, first)
	}
	for ev, ok := be.Poll(); ok; ev, ok = be.Poll() {
		if ev.Kind != input.IncomingPadList {
			got = append(got, ev)
		}
	}
	if len(got) < len(want) { // the first Wait may have returned the list only: read what SDL has queued
		if ev, ok := be.Wait(2 * time.Second); ok {
			got = append(got, ev)
		}
		for ev, ok := be.Poll(); ok; ev, ok = be.Poll() {
			got = append(got, ev)
		}
	}
	if !slices.EqualFunc(got, want, func(a, b input.Incoming) bool {
		return a.Kind == b.Kind && a.Input == b.Input && a.Text == b.Text && a.W == b.W && a.H == b.H && a.Pad == b.Pad
	}) {
		t.Fatalf("events %+v\nwant   %+v", got, want)
	}

	start := time.Now()
	be.Wake()
	if _, ok := be.Wait(5 * time.Second); ok || time.Since(start) > 3*time.Second {
		t.Fatalf("Wake must end a Wait quickly with no event (ok=%v after %v)", ok, time.Since(start))
	}
	start = time.Now()
	if _, ok := be.Wait(50 * time.Millisecond); ok || time.Since(start) > 2*time.Second {
		t.Fatalf("a Wait with nothing queued must time out (ok=%v after %v)", ok, time.Since(start))
	}
}

// pushOnFrame wraps the real backend for the end-to-end run: it drives time with a fake clock and pushes the
// next key press into SDL when the frame it was waiting for has been drawn, so no real sleeping is needed.
type pushOnFrame struct {
	*Backend
	now   time.Time
	steps []pushStep
}

type pushStep struct {
	afterText string // the frame must contain this text
	sym       int32  // the key to press
}

func (p *pushOnFrame) Clock() time.Time { return p.now }

func (p *pushOnFrame) Present(layout gdraw.Layout, l *gdraw.List) error {
	if err := p.Backend.Present(layout, l); err != nil {
		return err
	}
	p.now = p.now.Add(200 * time.Millisecond) // past the 150 ms a new dialog ignores input
	if len(p.steps) > 0 && slices.Contains(l.Texts(), p.steps[0].afterText) {
		for _, kind := range []uint32{EventKeyDown, EventKeyUp} {
			e := NewEvent()
			*e.Keyboard() = KeyboardEvent{Type: kind, Sym: p.steps[0].sym}
			if err := p.lib.PushEvent(e); err != nil {
				return err
			}
		}
		p.steps = p.steps[1:]
	}
	return nil
}

// A timed wait advances the fake clock instead of sleeping; an unlimited one waits on SDL for the pushed keys.
func (p *pushOnFrame) Wait(timeout time.Duration) (input.Incoming, bool) {
	if timeout >= 0 {
		p.now = p.now.Add(timeout)
		return input.Incoming{}, false
	}
	return p.Backend.Wait(timeout)
}

// The whole quit flow of section 5.13 through the real SDL: Escape asks, Right moves to Quit, Enter quits,
// the exit sequence runs against the fake service and the loop ends.
func TestQuitFlowEndToEndThroughSDL(t *testing.T) {
	be, font := headless(t, 1280, 720)
	svc := guitest.New()
	p := &pushOnFrame{Backend: be, now: testEpoch, steps: []pushStep{
		{afterText: "1,284,019 entries · Batocera · /userdata/roms", sym: KeycodeEscape},
		{afterText: "Quit and return to Batocera?", sym: KeycodeRight},
		{afterText: "Quit and return to Batocera?", sym: KeycodeReturn},
	}}
	m, cmds := gui.New(guiOptions(t, font, svc, 1280, 720), p.now)

	done := make(chan struct{})
	go func() { // a guard: a stuck loop fails the test instead of hanging it
		select {
		case <-done:
		case <-time.After(60 * time.Second):
			t.Error("the loop did not end in 60 s")
			quit := NewEvent()
			quit.SetType(EventQuit) // makes the loop leave through the exit sequence instead of waiting for ever
			_ = be.lib.PushEvent(quit)
		}
	}()
	final, err := loop.Run(context.Background(), loop.Config{Model: m, Cmds: cmds, Backend: p, Clock: p.Clock})
	close(done)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Done() {
		t.Fatal("the loop ended before the exit screen was done")
	}
	if got := svc.ExitCalls(); !slices.Equal(got, []string{"wait", "close"}) {
		t.Fatalf("service calls %v", got)
	}
	if len(p.steps) != 0 {
		t.Fatalf("keys never pushed: %+v", p.steps)
	}
}

// A cover is a texture uploaded once per id and drawn scaled into its rectangle; the cache keeps at most 48.
func TestImagesAreDrawnScaledAndTheTextureCacheIsBounded(t *testing.T) {
	be, _ := headless(t, 640, 480)
	solid := func(c color.RGBA) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 4, 4))
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				img.SetRGBA(x, y, c)
			}
		}
		return img
	}
	var list gdraw.List
	list.Reset(640, 480)
	list.Fill(gdraw.Rect{W: 640, H: 480}, gdraw.Color{R: colorBG[0], G: colorBG[1], B: colorBG[2], A: 0xff})
	for i := 0; i < 50; i++ {
		list.Image(gdraw.Rect{X: 10, Y: 10, W: 100, H: 80}, uint64(i), solid(color.RGBA{R: 200, G: uint8(i), B: 50, A: 255}))
	}
	if err := be.Draw(gdraw.Layout{W: 640, H: 480, Scale: 1}, &list); err != nil {
		t.Fatal(err)
	}
	if be.images.len() != maxImageTextures {
		t.Fatalf("%d textures held, want %d", be.images.len(), maxImageTextures)
	}
	r, g, b, _, err := be.Pixel(60, 50)
	if err != nil {
		t.Fatal(err)
	}
	if r != 200 || g != 49 || b != 50 { // the last image drawn wins, scaled from 4x4 to 100x80
		t.Errorf("pixel inside the image is %d,%d,%d", r, g, b)
	}
	expectPixel(t, be, 300, 300, colorBG, "outside the image")
}

// A cover goes the whole way through real SDL: the service serves a PNG, the model decodes it with
// app.DecodeCover, the draw list carries the picture and the renderer shows it in the cover panel.
func TestACoverIsDecodedDrawnAndVisibleInTheCoverPanel(t *testing.T) {
	be, font := headless(t, 1280, 720)
	svc := guitest.New()
	svc.Catalog = []mothdex.Entry{guitest.Game("zelda-1", "Zelda One", "snes", "us")}
	svc.Covers = map[string]bool{"zelda-1": true}
	now := testEpoch
	m, cmds := gui.New(guiOptions(t, font, svc, 1280, 720), now)
	run := func(cmds []gui.Cmd) {
		for len(cmds) > 0 {
			next := cmds[0]
			cmds = cmds[1:]
			var more []gui.Cmd
			m, more = m.Update(next(), now)
			cmds = append(cmds, more...)
		}
	}
	run(cmds)
	send := func(msg gui.Msg) { var c []gui.Cmd; m, c = m.Update(msg, now); run(c) }
	send(gui.InputMsg{Action: input.Confirm}) // Search
	send(gui.TextMsg{Text: "zelda"})
	send(gui.InputMsg{Action: input.Menu}) // run it
	now = now.Add(400 * time.Millisecond)
	send(gui.TickMsg{}) // the focus settled: the cover is asked, decoded and cached

	var list gdraw.List
	m.Render(now, &list)
	var img *gdraw.Op
	for i := range list.Ops {
		if list.Ops[i].Kind == gdraw.KindImage {
			img = &list.Ops[i]
		}
	}
	if img == nil {
		t.Fatal("no image op in the Results frame")
	}
	if err := be.Draw(layoutOf(m), &list); err != nil {
		t.Fatal(err)
	}
	cx, cy := img.Rect.X+img.Rect.W/2, img.Rect.Y+img.Rect.H/2
	r, g, b, _, err := be.Pixel(cx, cy)
	if err != nil {
		t.Fatal(err)
	}
	if r != 200 || g != 80 || b != 40 { // the solid color guitest.PNG makes
		t.Errorf("the cover pixel at %d,%d is %d,%d,%d", cx, cy, r, g, b)
	}
}
