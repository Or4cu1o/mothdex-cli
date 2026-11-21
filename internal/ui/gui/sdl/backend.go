//go:build gui

package sdl

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
)

const (
	atlasSide            = 1024 // the glyph atlas is one 1024 x 1024 texture (4 MiB)
	hintNoSignalHandlers = "SDL_NO_SIGNAL_HANDLERS"
	hintVideoDriver      = "SDL_VIDEODRIVER"
)

// BackendOptions say how to open the window.
type BackendOptions struct {
	Title    string
	Driver   string // SDL_VIDEODRIVER to force, such as "dummy"; empty leaves SDL to choose
	Windowed bool   // a Width x Height window instead of the whole desktop (tests, a desktop session)
	Width    int
	Height   int
	Hidden   bool
	Software bool     // use the software renderer
	Mappings []string // game controller database lines to add, in order, later ones winning
	Log      *slog.Logger
}

// Backend is the SDL2 display and input devices: it executes a draw list and turns SDL events into
// input.Incoming. All its methods except Wake must be called from the thread that opened it.
type Backend struct {
	lib  *Library
	log  *slog.Logger
	font *text.Font

	inited   bool
	win      Window
	ren      Renderer
	atlas    *text.Atlas
	atlasTex Texture
	upload   []byte
	placed   []text.Placed

	wakeType   uint32
	ev         *Event
	layout     gdraw.Layout
	haveLayout bool
	blend      int32 // the draw blend mode last set

	pads    map[int32]Controller
	padType map[int32]input.ControllerType
	pad     input.ControllerType // the type of the pad last heard from
	padSeen bool                 // a pad was reported to the loop at least once
	pending []input.Incoming
}

// OpenBackend starts SDL's video and game controller subsystems, adds the controller mappings and opens a
// window with a renderer and the glyph atlas texture. lib must already be loaded and version-checked.
func OpenBackend(lib *Library, font *text.Font, o BackendOptions) (b *Backend, err error) {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	b = &Backend{
		lib: lib, log: o.Log, font: font, ev: NewEvent(),
		atlas: text.NewAtlas(atlasSide, atlasSide),
		pads:  map[int32]Controller{}, padType: map[int32]input.ControllerType{},
	}
	defer func() {
		if err != nil {
			b.Close()
			b = nil
		}
	}()

	// A frontend's Port gets SIGTERM from the frontend; main handles it, not SDL.
	lib.SetHint(hintNoSignalHandlers, "1")
	if o.Driver != "" {
		lib.SetHint(hintVideoDriver, o.Driver)
	}
	if err = b.initSubsystems(); err != nil {
		return nil, err
	}
	if o.Driver != "" && lib.CurrentVideoDriver() != o.Driver {
		return nil, fmt.Errorf("the video driver %q was asked for and SDL chose %q", o.Driver, lib.CurrentVideoDriver())
	}
	b.addMappings(o.Mappings)

	if err = b.openWindow(o); err != nil {
		return nil, err
	}
	if b.atlasTex, err = lib.CreateTexture(b.ren, PixelFormatARGB8888, TextureAccessStatic, atlasSide, atlasSide); err != nil {
		return nil, err
	}
	if err = lib.SetTextureBlend(b.atlasTex, BlendBlend); err != nil {
		return nil, err
	}
	lib.HideCursor()
	if b.wakeType, err = lib.RegisterUserEvent(); err != nil {
		return nil, err
	}
	b.openControllers()
	return b, nil
}

// initSubsystems starts video and game controllers; with no joystick support the GUI still runs from the
// keyboard.
func (b *Backend) initSubsystems() error {
	b.inited = true // SDL_Quit is safe after a failed SDL_Init, so Close always calls it
	err := b.lib.Init(InitVideo | InitGameController)
	if err == nil {
		return nil
	}
	if videoErr := b.lib.Init(InitVideo); videoErr != nil {
		return err
	}
	b.log.Warn("sdl: game controllers are unavailable, the keyboard still works", "err", err)
	return nil
}

func (b *Backend) addMappings(lines []string) {
	added, replaced := 0, 0
	for _, line := range lines {
		isNew, err := b.lib.AddControllerMapping(line)
		switch {
		case err != nil:
			b.log.Warn("sdl: a controller mapping was refused", "err", err)
		case isNew:
			added++
		default:
			replaced++
		}
	}
	b.log.Info("sdl: controller mappings loaded", "added", added, "replaced", replaced)
}

func (b *Backend) openWindow(o BackendOptions) error {
	var flags uint32
	w, h := int32(o.Width), int32(o.Height) //nolint:gosec // a window size, small
	if !o.Windowed {
		flags |= WindowFullscreenDesktop
		w, h = 0, 0
	}
	if o.Hidden {
		flags |= WindowHidden
	}
	title := o.Title
	if title == "" {
		title = "mothdex"
	}
	var err error
	if b.win, err = b.lib.CreateWindow(title, w, h, flags); err != nil {
		return err
	}
	attempts := []uint32{RendererPresentVSync, 0, RendererSoftware}
	if o.Software {
		attempts = []uint32{RendererSoftware}
	}
	for _, rflags := range attempts {
		if b.ren, err = b.lib.CreateRenderer(b.win, rflags); err == nil {
			return nil
		}
		b.log.Warn("sdl: renderer not available, trying the next kind", "flags", rflags, "err", err)
	}
	return err
}

func (b *Backend) openControllers() {
	for i := 0; i < b.lib.JoystickCount(); i++ {
		if b.lib.IsController(i) {
			b.openController(i)
		}
	}
}

// openController opens the pad at a device index and remembers its type for the hint labels.
func (b *Backend) openController(index int) {
	c, err := b.lib.OpenController(index)
	if err != nil {
		b.log.Warn("sdl: cannot open a controller", "index", index, "err", err)
		return
	}
	id, err := b.lib.ControllerInstanceID(c)
	if err != nil {
		b.lib.CloseController(c)
		b.log.Warn("sdl: a controller has no instance id", "err", err)
		return
	}
	if old, ok := b.pads[id]; ok { // the same pad announced twice
		b.lib.CloseController(old)
	}
	typ := input.ControllerType(b.lib.ControllerType(c))
	b.pads[id], b.padType[id] = c, typ
	b.log.Info("sdl: controller", "name", b.lib.ControllerName(c), "type", int(typ))
	b.setPad(typ)
}

// setPad records the pad in hand and queues a notice for the loop when it changed (the first pad always
// reports).
func (b *Backend) setPad(typ input.ControllerType) {
	if b.padSeen && typ == b.pad {
		return
	}
	b.pad, b.padSeen = typ, true
	b.pending = append(b.pending, input.Incoming{Kind: input.IncomingPad, Pad: typ})
}

// Close frees everything, in the order SDL needs.
func (b *Backend) Close() {
	for id, c := range b.pads {
		b.lib.CloseController(c)
		delete(b.pads, id)
	}
	if b.atlasTex.handle != 0 {
		b.lib.DestroyTexture(b.atlasTex)
		b.atlasTex = Texture{}
	}
	if b.ren != 0 {
		b.lib.DestroyRenderer(b.ren)
		b.ren = 0
	}
	if b.win != 0 {
		b.lib.DestroyWindow(b.win)
		b.win = 0
	}
	if b.inited {
		b.lib.Quit()
		b.inited = false
	}
}

// Size is the size of the window in pixels.
func (b *Backend) Size() (w, h int) {
	rw, rh, err := b.lib.OutputSize(b.ren)
	if err != nil {
		b.log.Warn("sdl: cannot read the output size", "err", err)
		return 0, 0
	}
	return int(rw), int(rh)
}

// Driver is the video driver in use.
func (b *Backend) Driver() string { return b.lib.CurrentVideoDriver() }

// Wake ends a Wait from any goroutine, by pushing the user event registered at open. SDL_PushEvent is safe
// to call from any thread.
func (b *Backend) Wake() {
	e := NewEvent()
	e.SetType(b.wakeType)
	if err := b.lib.PushEvent(e); err != nil {
		b.log.Debug("sdl: wake", "err", err)
	}
}

// Present draws a frame and shows it.
func (b *Backend) Present(layout gdraw.Layout, l *gdraw.List) error {
	if err := b.Draw(layout, l); err != nil {
		return err
	}
	b.lib.Present(b.ren)
	return nil
}

// Draw draws a frame into the back buffer without showing it, which is when ReadPixels can see it.
func (b *Backend) Draw(layout gdraw.Layout, l *gdraw.List) error {
	if err := b.applyLayout(layout); err != nil {
		return err
	}
	// The bars around a letterboxed layout take the color of the background, the first thing drawn.
	bg := gdraw.Color{A: 0xff}
	if len(l.Ops) > 0 && l.Ops[0].Kind == gdraw.KindRect {
		bg = l.Ops[0].Color
	}
	if err := b.setBlend(BlendNone); err != nil {
		return err
	}
	if err := b.lib.SetDrawColor(b.ren, bg.R, bg.G, bg.B, 0xff); err != nil {
		return err
	}
	if err := b.lib.Clear(b.ren); err != nil {
		return err
	}
	for _, op := range l.Ops {
		var err error
		switch op.Kind {
		case gdraw.KindRect:
			err = b.fill(op.Rect, op.Radius, op.Color)
		case gdraw.KindText:
			err = b.drawText(op)
		case gdraw.KindSpinner:
			err = b.drawSpinner(op)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// applyLayout sets the render scale or the logical size when the layout changed.
func (b *Backend) applyLayout(layout gdraw.Layout) error {
	if b.haveLayout && layout == b.layout {
		return nil
	}
	if layout.Fit {
		if err := b.lib.SetScale(b.ren, 1, 1); err != nil {
			return err
		}
		if err := b.lib.SetLogicalSize(b.ren, int32(layout.W), int32(layout.H)); err != nil { //nolint:gosec // a layout size, small
			return err
		}
	} else {
		if err := b.lib.SetLogicalSize(b.ren, 0, 0); err != nil {
			return err
		}
		scale := float32(max(layout.Scale, 1))
		if err := b.lib.SetScale(b.ren, scale, scale); err != nil {
			return err
		}
	}
	b.layout, b.haveLayout = layout, true
	return nil
}

func (b *Backend) setBlend(mode int32) error {
	if mode == b.blend {
		return nil
	}
	if err := b.lib.SetDrawBlend(b.ren, mode); err != nil {
		return err
	}
	b.blend = mode
	return nil
}

func (b *Backend) setColor(c gdraw.Color) error {
	mode := int32(BlendNone)
	if c.A < 0xff {
		mode = BlendBlend
	}
	if err := b.setBlend(mode); err != nil {
		return err
	}
	return b.lib.SetDrawColor(b.ren, c.R, c.G, c.B, c.A)
}

func toRect(r gdraw.Rect) Rect {
	return Rect{X: int32(r.X), Y: int32(r.Y), W: int32(r.W), H: int32(r.H)} //nolint:gosec // logical pixel coordinates, small
}

// fill draws a rectangle, with rounded corners when radius is above zero.
func (b *Backend) fill(r gdraw.Rect, radius int, c gdraw.Color) error {
	if err := b.setColor(c); err != nil {
		return err
	}
	for _, part := range roundedRects(r, radius) {
		if err := b.lib.FillRect(b.ren, toRect(part)); err != nil {
			return err
		}
	}
	return nil
}

func (b *Backend) drawSpinner(op gdraw.Op) error {
	for k := 0; k < spinnerDots; k++ {
		x, y, side, alpha := spinnerDot(op, k)
		c := op.Color
		c.A = uint8(min(uint16(c.A)*uint16(alpha)/255, 255)) //nolint:gosec // clamped to a byte on this line
		if err := b.fill(gdraw.Rect{X: x, Y: y, W: side, H: side}, 0, c); err != nil {
			return err
		}
	}
	return nil
}

// drawText draws one line: each glyph is looked up in the atlas (rasterized the first time), the changed
// part of the atlas is uploaded, and the glyphs are copied tinted with the color of the run.
func (b *Backend) drawText(op gdraw.Op) error {
	face, err := b.font.Face(op.Size)
	if err != nil {
		return err
	}
	b.placed = face.Layout(op.Text, b.placed[:0])
	entries := make([]text.Entry, len(b.placed))
	for attempt := 0; attempt < 2; attempt++ {
		generation := b.atlas.Generation()
		for i, p := range b.placed {
			entries[i], _ = b.atlas.Glyph(face, p.Rune) // a glyph larger than the atlas draws as nothing
		}
		if b.atlas.Generation() == generation {
			break // otherwise the atlas started again in the middle of the run: look the glyphs up once more
		}
	}
	if err := b.flushAtlas(); err != nil {
		return err
	}
	if err := b.lib.SetTextureColor(b.atlasTex, op.Color.R, op.Color.G, op.Color.B, op.Color.A); err != nil {
		return err
	}
	for i, p := range b.placed {
		e := entries[i]
		if e.W == 0 || e.H == 0 {
			continue
		}
		src := Rect{X: int32(e.X), Y: int32(e.Y), W: int32(e.W), H: int32(e.H)}                              //nolint:gosec // inside a 1024 x 1024 atlas
		dst := Rect{X: int32(op.X + p.X + e.OffX), Y: int32(op.Base + e.OffY), W: int32(e.W), H: int32(e.H)} //nolint:gosec // logical pixel coordinates, small
		if err := b.lib.Copy(b.ren, b.atlasTex, &src, &dst); err != nil {
			return err
		}
	}
	return nil
}

// flushAtlas uploads what changed in the atlas since the last upload.
func (b *Backend) flushAtlas() error {
	dirty, ok := b.atlas.TakeDirty()
	if !ok {
		return nil
	}
	var pitch int
	b.upload, pitch = b.atlas.AppendARGB(b.upload[:0], dirty)
	rect := Rect{X: int32(dirty.Min.X), Y: int32(dirty.Min.Y), W: int32(dirty.Dx()), H: int32(dirty.Dy())} //nolint:gosec // inside the atlas
	return b.lib.UpdateTexture(b.atlasTex, &rect, b.upload, int32(pitch))                                  //nolint:gosec // at most 4096 bytes
}

// Pixel reads one pixel of the frame drawn by Draw, for the headless render test. Coordinates are in window
// pixels, which are logical pixels when the layout has scale 1.
func (b *Backend) Pixel(x, y int) (r, g, bl, a uint8, err error) {
	var buf [4]byte
	err = b.lib.ReadPixels(b.ren, Rect{X: int32(x), Y: int32(y), W: 1, H: 1}, PixelFormatARGB8888, buf[:], 4) //nolint:gosec // test coordinates, small
	return buf[2], buf[1], buf[0], buf[3], err
}

// Wait waits up to timeout for something the loop must act on; a negative timeout waits with no limit. It
// reports false when the timeout passed or Wake was called.
func (b *Backend) Wait(timeout time.Duration) (input.Incoming, bool) {
	if ev, ok := b.popPending(); ok {
		return ev, true
	}
	var deadline time.Time
	if timeout >= 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		ms := int32(-1)
		if timeout >= 0 {
			ms = waitMillis(time.Until(deadline))
		}
		if !b.lib.WaitEvent(b.ev, ms) {
			return input.Incoming{}, false
		}
		if b.ev.Type() == b.wakeType {
			return input.Incoming{}, false
		}
		b.translate(b.ev)
		if ev, ok := b.popPending(); ok {
			return ev, true
		}
		if timeout >= 0 && !time.Now().Before(deadline) { // events that mean nothing must not stretch the wait
			return input.Incoming{}, false
		}
	}
}

// Poll returns the next event that is already queued.
func (b *Backend) Poll() (input.Incoming, bool) {
	for {
		if ev, ok := b.popPending(); ok {
			return ev, true
		}
		if !b.lib.PollEvent(b.ev) {
			return input.Incoming{}, false
		}
		if b.ev.Type() == b.wakeType {
			continue
		}
		b.translate(b.ev)
	}
}

func (b *Backend) popPending() (input.Incoming, bool) {
	if len(b.pending) == 0 {
		return input.Incoming{}, false
	}
	ev := b.pending[0]
	b.pending = b.pending[1:]
	return ev, true
}

// translate turns one SDL event into zero or more loop events, queued in b.pending.
func (b *Backend) translate(e *Event) {
	switch e.Type() {
	case EventQuit:
		b.pending = append(b.pending, input.Incoming{Kind: input.IncomingQuit})
	case EventWindow:
		switch ev := e.Window().Event; ev {
		case WindowEventResized, WindowEventSizeChanged:
			if w, h := b.Size(); w > 0 && h > 0 {
				b.pending = append(b.pending, input.Incoming{Kind: input.IncomingResize, W: w, H: h})
			}
		case WindowEventShown, WindowEventExposed, WindowEventRestored: // uncovered: draw again
			b.pending = append(b.pending, input.Incoming{Kind: input.IncomingRedraw})
		}
	case EventKeyDown, EventKeyUp:
		b.translateKey(e)
	case EventControllerButtonDown, EventControllerButtonUp:
		b.translateButton(e)
	case EventControllerAxisMotion:
		a := e.ControllerAxis()
		if axis, ok := axisFromSDL(a.Axis); ok {
			b.noticePad(a.Which)
			b.pending = append(b.pending, input.Incoming{Kind: input.IncomingInput, Input: input.Event{Kind: input.AxisMotion, Axis: axis, Value: int(a.Value)}})
		}
	case EventControllerDeviceAdded:
		b.openController(int(e.ControllerDevice().Which))
	case EventControllerDeviceRemoved:
		b.closeController(e.ControllerDevice().Which)
	}
}

func (b *Backend) translateKey(e *Event) {
	k := e.Keyboard()
	if k.Repeat != 0 { // SDL's own key repeat is ignored: the repeat timing is the pad's
		return
	}
	code, r := keyFromSym(k.Sym)
	if code == input.KeyNone {
		return
	}
	kind := input.KeyPress
	if k.Type == EventKeyUp {
		kind = input.KeyRelease
	}
	b.pending = append(b.pending, input.Incoming{Kind: input.IncomingInput, Input: input.Event{Kind: kind, Key: code, Rune: r}})
}

func (b *Backend) translateButton(e *Event) {
	c := e.ControllerButton()
	btn, ok := buttonFromSDL(c.Button)
	if !ok {
		return
	}
	b.noticePad(c.Which)
	kind := input.ButtonPress
	if c.Type == EventControllerButtonUp {
		kind = input.ButtonRelease
	}
	b.pending = append(b.pending, input.Incoming{Kind: input.IncomingInput, Input: input.Event{Kind: kind, Button: btn}})
}

// noticePad reports a change of pad type when the pad that spoke is not the one in hand.
func (b *Backend) noticePad(which int32) {
	if typ, ok := b.padType[which]; ok && typ != b.pad {
		b.setPad(typ)
	}
}

func (b *Backend) closeController(id int32) {
	if c, ok := b.pads[id]; ok {
		b.lib.CloseController(c)
		delete(b.pads, id)
		delete(b.padType, id)
	}
}
