package loop

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

// testClock is time the test moves by hand; its timers never fire.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time                       { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }
func (c *testClock) advance(d time.Duration)              { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

// step is what happens between two frames: time passes, then events arrive.
type step struct {
	advance time.Duration
	events  []input.Incoming
	before  func(frames int) // called when the loop asks for this step, with the frames drawn so far
}

// scriptedBackend gives the loop its events from a script, one step for each Wait.
type scriptedBackend struct {
	t        *testing.T
	clock    *testClock
	steps    []step
	next     int
	queue    []input.Incoming
	w, h     int
	timeouts []time.Duration
	frames   []frame
}

type frame struct {
	texts  []string
	layout draw.Layout
}

func (b *scriptedBackend) Size() (int, int) { return b.w, b.h }

func (b *scriptedBackend) Present(layout draw.Layout, l *draw.List) error {
	b.frames = append(b.frames, frame{texts: l.Texts(), layout: layout})
	return nil
}

func (b *scriptedBackend) Wait(timeout time.Duration) (input.Incoming, bool) {
	b.t.Helper()
	b.timeouts = append(b.timeouts, timeout)
	if b.next >= len(b.steps) {
		b.t.Fatalf("the script ran out after %d steps but the loop is still running", len(b.steps))
	}
	s := b.steps[b.next]
	b.next++
	if s.before != nil {
		s.before(len(b.frames))
	}
	b.clock.advance(s.advance)
	b.queue = slices.Clone(s.events)
	return b.Poll()
}

func (b *scriptedBackend) Poll() (input.Incoming, bool) {
	if len(b.queue) == 0 {
		return input.Incoming{}, false
	}
	ev := b.queue[0]
	b.queue = b.queue[1:]
	return ev, true
}

func (b *scriptedBackend) Wake() {}

func (b *scriptedBackend) SetTextInput(bool) {}

func btn(kind input.Kind, b input.Button) input.Incoming {
	return input.Incoming{Kind: input.IncomingInput, Input: input.Event{Kind: kind, Button: b}}
}
func press(b input.Button) input.Incoming   { return btn(input.ButtonPress, b) }
func release(b input.Button) input.Incoming { return btn(input.ButtonRelease, b) }

var epoch = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

type fixture struct {
	svc     *guitest.Fake
	clock   *testClock
	backend *scriptedBackend
	cfg     Config
}

func newFixture(t *testing.T, w, h int, steps ...step) *fixture {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	font, err := text.Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	svc := guitest.New()
	clock := &testClock{now: epoch}
	m, cmds := gui.New(gui.Options{
		Service: svc, T: bundle.New("en_us"), Font: font, Clock: clock, DataDir: t.TempDir(),
		Version: "0.3.0", Width: w, Height: h, Safe: gui.AutoSafeArea,
	}, epoch)
	be := &scriptedBackend{t: t, clock: clock, steps: steps, w: w, h: h}
	return &fixture{svc: svc, clock: clock, backend: be, cfg: Config{
		Model: m, Cmds: cmds, Backend: be, Clock: clock.Now,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Go:  func(f func()) { f() }, // service calls finish before the loop reads their results
	}}
}

func (f *fixture) run(t *testing.T) gui.Model {
	t.Helper()
	m, err := Run(context.Background(), f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fixture) sawText(s string) bool {
	for _, fr := range f.backend.frames {
		if slices.Contains(fr.texts, s) {
			return true
		}
	}
	return false
}

// Home shows when the service answers, and with nothing due the loop waits with no limit.
func TestTheLoopShowsHomeAndBlocksWhenNothingIsDue(t *testing.T) {
	f := newFixture(t, 1280, 720,
		step{events: []input.Incoming{press(input.Start)}}, // opens the menu: a frame with it
		step{events: []input.Incoming{press(input.East)}},  // closes it
		step{events: []input.Incoming{{Kind: input.IncomingQuit}}},
		step{advance: 5 * time.Second},
	)
	f.run(t)
	if !f.sawText("1,284,019 entries · Batocera · /roms") && !f.sawText("1,284,019 entries · Batocera · /userdata/roms") {
		t.Errorf("Home never showed the footer; frames: %v", f.backend.frames[0].texts)
	}
	if first := f.backend.timeouts[0]; first != -1 {
		t.Errorf("a calm Home must wait with no limit, the first wait was %v", first)
	}
}

// Back, Right, A: the quit dialog, Quit, the exit screen, and the loop ends when the exit screen is done.
func TestQuitThroughTheDialogRunsTheExitSequenceAndEndsTheLoop(t *testing.T) {
	f := newFixture(t, 1280, 720,
		step{events: []input.Incoming{press(input.East), release(input.East)}},
		step{advance: 200 * time.Millisecond, events: []input.Incoming{press(input.DPadRight), release(input.DPadRight)}},
		step{events: []input.Incoming{press(input.South), release(input.South)}},
		step{advance: 1100 * time.Millisecond},
	)
	m := f.run(t)
	if !m.Done() {
		t.Fatal("the loop ended before the model was done")
	}
	if !f.sawText("Quit and return to Batocera?") || !f.sawText("Returning to Batocera") {
		t.Errorf("the dialog and the exit screen must both have been drawn")
	}
	if got := f.svc.Calls(); !slices.Equal(got, []string{"wait", "close"}) {
		t.Fatalf("service calls %v", got)
	}
}

// A held direction repeats on the pad's timing and stops when a dialog opens over it.
func TestAHeldDirectionRepeatsAndIsReleasedWhenADialogOpens(t *testing.T) {
	f := newFixture(t, 1280, 720,
		step{events: []input.Incoming{press(input.DPadRight)}},
		step{advance: 400 * time.Millisecond},             // the first repeat
		step{events: []input.Incoming{press(input.East)}}, // a dialog opens with the direction still held
		step{advance: 2 * time.Second},                    // nothing may repeat into the dialog
		step{events: []input.Incoming{{Kind: input.IncomingQuit}}},
		step{advance: 2 * time.Second},
	)
	f.run(t)
	to := f.backend.timeouts
	if to[1] != 400*time.Millisecond {
		t.Errorf("the first repeat is due in 400 ms, the loop waited %v", to[1])
	}
	if dialogWait := to[3]; dialogWait != -1 {
		t.Errorf("with the dialog open the held direction must be released, so the loop waits with no limit; it waited %v", dialogWait)
	}
}

func TestPadAndResizeEventsReachTheModel(t *testing.T) {
	f := newFixture(t, 640, 480,
		step{events: []input.Incoming{{Kind: input.IncomingPad, Pad: input.TypeSwitchPro}}},
		step{events: []input.Incoming{{Kind: input.IncomingResize, W: 1920, H: 1080}}},
		step{events: []input.Incoming{{Kind: input.IncomingQuit}}},
		step{advance: 5 * time.Second},
	)
	f.run(t)
	var sawSwitchLabels, sawLarge bool
	for _, fr := range f.backend.frames {
		if i := slices.Index(fr.texts, "Open"); i > 0 && fr.texts[i-1] == "B" {
			sawSwitchLabels = true
		}
		sawLarge = sawLarge || (fr.layout.W == 1920 && fr.layout.H == 1080)
	}
	if !sawSwitchLabels {
		t.Error("a Nintendo pad must put B before Open in the hint bar")
	}
	if !sawLarge {
		t.Error("a 1920x1080 resize must draw a 1920x1080 layout")
	}
}

// SIGTERM, with the real goroutines: the loop ends without a dialog and the sequence ran.
func TestASignalEndsTheLoopWithTheExitSequence(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	font, err := text.Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	svc := guitest.New()
	m, cmds := gui.New(gui.Options{Service: svc, T: bundle.New("en_us"), Font: font, DataDir: t.TempDir(), Width: 1280, Height: 720}, time.Now())
	be := &blockingBackend{wake: make(chan struct{}, 1)}
	signals := make(chan struct{})
	done := make(chan gui.Model, 1)
	go func() {
		final, runErr := Run(context.Background(), Config{Model: m, Cmds: cmds, Backend: be, Clock: time.Now, Signals: signals})
		if runErr != nil {
			t.Error(runErr)
		}
		done <- final
	}()
	close(signals) // SIGTERM
	select {
	case final := <-done:
		if !final.Done() {
			t.Fatal("not done")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the loop did not end after the signal")
	}
	if got := svc.Calls(); !slices.Equal(got, []string{"wait", "close"}) {
		t.Fatalf("service calls %v", got)
	}
}

// blockingBackend waits on Wake, as the SDL backend does with SDL_WaitEventTimeout.
type blockingBackend struct{ wake chan struct{} }

func (b *blockingBackend) Size() (int, int)                      { return 1280, 720 }
func (b *blockingBackend) Present(draw.Layout, *draw.List) error { return nil }
func (b *blockingBackend) Poll() (input.Incoming, bool)          { return input.Incoming{}, false }
func (b *blockingBackend) SetTextInput(bool)                     {}
func (b *blockingBackend) Wake() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}
func (b *blockingBackend) Wait(timeout time.Duration) (input.Incoming, bool) {
	if timeout < 0 {
		<-b.wake
		return input.Incoming{}, false
	}
	select {
	case <-b.wake:
	case <-time.After(timeout):
	}
	return input.Incoming{}, false
}

// Stick noise below the threshold makes no action, so it must not make a frame (a handheld's battery).
func TestEventsThatChangeNothingDoNotRedraw(t *testing.T) {
	noise := input.Incoming{Kind: input.IncomingInput, Input: input.Event{Kind: input.AxisMotion, Axis: input.LeftX, Value: 4000}}
	var framesAfterNoise int
	f := newFixture(t, 1280, 720,
		step{events: []input.Incoming{noise}},
		step{before: func(frames int) { framesAfterNoise = frames }, events: []input.Incoming{noise, noise}},
		step{before: func(frames int) {
			if frames != framesAfterNoise {
				t.Errorf("drew %d frames for noise, expected %d", frames, framesAfterNoise)
			}
		}, events: []input.Incoming{{Kind: input.IncomingQuit}}},
		step{advance: 5 * time.Second},
	)
	f.run(t)
	if framesAfterNoise != 1 {
		t.Errorf("only the first frame was expected before the noise, got %d", framesAfterNoise)
	}
}
