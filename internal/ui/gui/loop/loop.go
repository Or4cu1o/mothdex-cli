// Package loop is the GUI's run loop: it turns backend events into actions, feeds the model, runs the
// service calls the model asks for off the render thread, and draws a frame when something changed. It talks
// to the display through the Backend interface and has no SDL, so it is tested with a fake backend.
package loop

import (
	"context"
	"log/slog"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Backend is the display and the input devices. Its methods are called from the render thread, except Wake,
// which any goroutine may call.
type Backend interface {
	// Size is the display size in pixels.
	Size() (w, h int)
	// Present draws one frame. The backend applies the layout (render scale or logical size) when it
	// changed since the last frame.
	Present(layout draw.Layout, l *draw.List) error
	// Wait blocks until an event arrives, Wake is called, or timeout passes; a negative timeout waits with
	// no limit. ok is false when it returned for any reason but an event.
	Wait(timeout time.Duration) (ev input.Incoming, ok bool)
	// Poll returns the next queued event without waiting.
	Poll() (ev input.Incoming, ok bool)
	// Wake makes a Wait that is blocked return.
	Wake()
}

// EventSource is where job events come from: the app service.
type EventSource interface {
	Events(ctx context.Context) <-chan app.Event
}

// Config is what Run needs.
type Config struct {
	Model   gui.Model
	Cmds    []gui.Cmd // the calls New asked for
	Backend Backend
	Events  EventSource
	Clock   func() time.Time
	Signals <-chan struct{} // closed or written to on SIGTERM and SIGINT
	Log     *slog.Logger

	// Go starts a goroutine. Tests replace it with a function that runs inline, so service calls finish
	// before the loop looks for their results.
	Go func(func())
}

const msgBuffer = 64

// Run draws and drives the model until it is done and returns the final model, with the error of the first
// frame that failed to draw, if any. It must be called from the thread that owns the display.
func Run(ctx context.Context, c Config) (gui.Model, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.Go == nil {
		c.Go = func(f func()) { go f() }
	}
	l := &loopState{Config: c, model: c.Model, msgs: make(chan gui.Msg, msgBuffer)}
	l.mapper = input.NewMapper(l.model.InputConfig())
	l.epoch = l.model.OverlayEpoch()
	l.start(ctx, c.Cmds)
	l.watch(ctx)

	w, h := c.Backend.Size()
	l.apply(gui.ResizeMsg{W: w, H: h})
	l.dirty = true
	for !l.model.Done() {
		now := c.Clock()
		for _, a := range l.mapper.Tick(now) {
			l.apply(gui.InputMsg{Action: a})
		}
		l.apply(gui.TickMsg{})
		l.drain()
		l.sync()
		if l.model.Done() {
			break
		}
		if l.dirty {
			if err := l.frame(now); err != nil {
				return l.model, err
			}
			l.dirty = false
		}
		l.waitAndRead()
	}
	return l.model, nil
}

type loopState struct {
	Config
	model  gui.Model
	mapper *input.Mapper
	msgs   chan gui.Msg
	epoch  int
	list   draw.List
	dirty  bool // something changed since the last frame: a message, a repeat or a timer
}

// start runs service calls off the render thread; each result comes back as a message and wakes the loop.
func (l *loopState) start(ctx context.Context, cmds []gui.Cmd) {
	for _, cmd := range cmds {
		l.Go(func() {
			msg := cmd()
			select {
			case l.msgs <- msg:
			case <-ctx.Done():
				return
			}
			l.Backend.Wake()
		})
	}
}

// watch forwards job events and signals into the message queue.
func (l *loopState) watch(ctx context.Context) {
	forward := func(msg gui.Msg) bool {
		select {
		case l.msgs <- msg:
			l.Backend.Wake()
			return true
		case <-ctx.Done():
			return false
		}
	}
	if l.Events != nil {
		events := l.Events.Events(ctx)
		go func() {
			for ev := range events {
				if !forward(gui.JobMsg{Job: ev.Job}) {
					return
				}
			}
		}()
	}
	if l.Signals != nil {
		go func() {
			select {
			case <-l.Signals:
				forward(gui.SignalMsg{})
			case <-ctx.Done():
			}
		}()
	}
}

// apply gives one message to the model and starts the calls it asks for. Anything but a Tick may change
// what is on screen; a Tick matters only when a timer was due, which waitAndRead notices.
func (l *loopState) apply(msg gui.Msg) {
	if _, tick := msg.(gui.TickMsg); !tick {
		l.dirty = true
	}
	var cmds []gui.Cmd
	l.model, cmds = l.model.Update(msg, l.Clock())
	l.start(context.Background(), cmds)
}

// drain applies every result that has come back.
func (l *loopState) drain() {
	for {
		select {
		case msg := <-l.msgs:
			l.apply(msg)
		default:
			return
		}
	}
}

// sync passes the model's settings to the input layer and releases a held button when an overlay opened or
// closed, so a held direction does not carry on into a dialog.
func (l *loopState) sync() {
	l.mapper.SetConfig(l.model.InputConfig())
	if e := l.model.OverlayEpoch(); e != l.epoch {
		l.epoch = e
		l.mapper.Release()
	}
}

func (l *loopState) frame(now time.Time) error {
	l.model.Render(now, &l.list)
	m := l.model.Metrics()
	return l.Backend.Present(draw.Layout{W: m.W, H: m.H, Scale: m.Scale, Fit: m.FitLogical}, &l.list)
}

// waitAndRead blocks until something needs doing, then reads every event that is queued.
func (l *loopState) waitAndRead() {
	now := l.Clock()
	timeout := time.Duration(-1)
	for _, due := range []func() (time.Time, bool){
		func() (time.Time, bool) { return l.model.NextWake(now) },
		l.mapper.Deadline,
	} {
		if t, ok := due(); ok {
			d := max(t.Sub(now), 0)
			if timeout < 0 || d < timeout {
				timeout = d
			}
		}
	}
	ev, ok := l.Backend.Wait(timeout)
	if !ok && timeout >= 0 && !l.Clock().Before(now.Add(timeout)) {
		l.dirty = true // a timer of the model came due: a toast ends, a spinner turns, Home opens
	}
	for ; ok; ev, ok = l.Backend.Poll() {
		l.handle(ev)
	}
	l.drain()
}

func (l *loopState) handle(ev input.Incoming) {
	switch ev.Kind {
	case input.IncomingInput:
		for _, a := range l.mapper.Handle(ev.Input, l.Clock()) {
			l.apply(gui.InputMsg{Action: a})
			l.sync()
		}
	case input.IncomingResize:
		l.apply(gui.ResizeMsg{W: ev.W, H: ev.H})
	case input.IncomingPad:
		l.apply(gui.ControllerMsg{Type: ev.Pad})
	case input.IncomingQuit:
		l.apply(gui.QuitRequestMsg{})
	case input.IncomingRedraw:
		l.apply(gui.RedrawMsg{})
	}
}
