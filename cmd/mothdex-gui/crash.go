//go:build gui

package main

import (
	"context"
	"os"
	"runtime/debug"
	"time"

	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
)

// These are variables only so that a test, and a build with the crashtest tag, can replace them.
var (
	// closeBudget is how long a crashed run waits for the service to flush its history before it leaves.
	closeBudget = 2 * time.Second
	// wrapBackend is the display the run loop draws on.
	wrapBackend = func(b loop.Backend) loop.Backend { return b }
	// exitProcess ends a run that cannot return.
	exitProcess = os.Exit
)

// panicked is a panic of the render thread, with the stack of where it happened.
type panicked struct {
	value any
	stack []byte
}

// runLoop is loop.Run with a panic of the render thread returned instead of ending the process, so the crash
// screen can be shown on the same window. A panic of another goroutine cannot be caught: the runtime writes it
// to the session file and ends the process (logs.md section 5, G-LOG-1).
func runLoop(ctx context.Context, c loop.Config) (final gui.Model, crash *panicked, err error) {
	defer func() {
		if r := recover(); r != nil {
			crash = &panicked{value: r, stack: debug.Stack()}
		}
	}()
	final, err = loop.Run(ctx, c)
	return final, nil, err
}

// showCrash shows the crash screen on the window of the run until the person leaves it. A second panic, or a
// frame that cannot be drawn, ends it.
func showCrash(ctx context.Context, screen gui.Model, c loop.Config) {
	c.Model, c.Cmds, c.Events = screen, nil, nil // no service call and no job event reaches a broken run
	_, again, err := runLoop(ctx, c)
	switch {
	case again != nil:
		c.Log.Error("gui: the crash screen failed too", "panic", again.value, "stack", string(again.stack))
	case err != nil:
		c.Log.Error("gui: the crash screen could not be drawn", "err", err)
	}
}

// leaveCrashed ends the process after a crash: SDL is freed, the service gets closeBudget to flush its history,
// and the log is closed. It does not return to the deferred closes, which could wait on what panicked.
func leaveCrashed(closeBackend, closeService func(), logs *mlog.Session, exit func(int)) {
	closeBackend()
	closeWithin(closeBudget, closeService)
	_ = logs.Close()
	exit(exitInternal)
}

// closeWithin runs shutdown and gives up on it after budget.
func closeWithin(budget time.Duration, shutdown func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() { _ = recover() }() // the process is leaving: a second panic must not change its exit code
		shutdown()
	}()
	select {
	case <-done:
	case <-time.After(budget):
	}
}
