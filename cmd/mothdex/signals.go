package main

import (
	"os"
	"sync"
	"time"
)

// secondInterrupt is the window in which a second signal ends the process at once.
const secondInterrupt = 2 * time.Second

// exitInterrupted is the exit code of a process ended by a second interrupt.
const exitInterrupted = 130

// signalRouter decides what a signal does. By default the first signal cancels the root context
// and a second one within secondInterrupt exits at once. The Shell takes SIGINT over with
// interrupts: Ctrl+C then cancels the command it is running, not the program. SIGTERM always
// follows the default (ui.md section 1).
type signalRouter struct {
	cancel func()
	exit   func(code int)
	now    func() time.Time

	mu    sync.Mutex
	sink  chan os.Signal
	prior time.Time // when the last signal that was not handed over arrived
}

func newSignalRouter(cancel func(), exit func(int), now func() time.Time) *signalRouter {
	return &signalRouter{cancel: cancel, exit: exit, now: now}
}

// handle reacts to one signal.
func (r *signalRouter) handle(sig os.Signal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sig == os.Interrupt && r.sink != nil {
		select {
		case r.sink <- sig:
		default: // one is already waiting to be read
		}
		return
	}
	now := r.now()
	if !r.prior.IsZero() && now.Sub(r.prior) < secondInterrupt {
		r.exit(exitInterrupted)
		return
	}
	r.prior = now
	r.cancel()
}

// interrupts hands SIGINT to the caller until release runs.
func (r *signalRouter) interrupts() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	r.mu.Lock()
	r.sink = ch
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.sink == ch {
			r.sink = nil
		}
	}
}
