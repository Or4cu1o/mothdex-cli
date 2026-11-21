package gui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// The quit decision table of docs/ux/gui.md section 5.13.
func TestDecideQuit(t *testing.T) {
	tests := []struct {
		name       string
		from       QuitRequest
		unfinished int
		want       QuitStep
	}{
		{"B on Home with nothing running confirms", QuitFromBack, 0, QuitConfirm},
		{"Start menu Quit with nothing running quits", QuitFromMenu, 0, QuitNow},
		{"B on Home with a download asks the three-way question", QuitFromBack, 1, QuitAskBusy},
		{"Start menu Quit with downloads asks too", QuitFromMenu, 3, QuitAskBusy},
	}
	for _, tc := range tests {
		if got := DecideQuit(tc.from, tc.unfinished); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

// fakeClock hands out timers the test fires by hand, and records what was asked.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	d  time.Duration
	ch chan time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.timers = append(c.timers, fakeTimer{d, ch})
	return ch
}

func (c *fakeClock) timer(i int) (fakeTimer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i < len(c.timers) {
		return c.timers[i], true
	}
	return fakeTimer{}, false
}

// exitFake records the calls of the exit sequence, in order.
type exitFake struct {
	mu       sync.Mutex
	calls    []string
	jobs     []app.JobSnapshot
	batch    app.BatchResult
	waitErr  error
	closeErr error
	waitHold chan struct{} // when set, Wait blocks until it is closed or its context ends
	waitCtx  context.Context
}

func (f *exitFake) record(s string) { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }

func (f *exitFake) order() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *exitFake) Jobs() []app.JobSnapshot { f.record("jobs"); return f.jobs }
func (f *exitFake) Cancel(id app.JobID) error {
	f.record("cancel:" + string(id))
	return nil
}

func (f *exitFake) Wait(ctx context.Context) (app.BatchResult, error) {
	f.record("wait")
	f.mu.Lock()
	f.waitCtx = ctx
	hold := f.waitHold
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return app.BatchResult{}, ctx.Err()
		}
	}
	return f.batch, f.waitErr
}

func (f *exitFake) Close() error { f.record("close"); return f.closeErr }

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func job(id string, state app.JobSnapshot) app.JobSnapshot { state.ID = app.JobID(id); return state }

func TestExitSequenceCancelsWaitsThenCloses(t *testing.T) {
	fake := &exitFake{
		jobs: []app.JobSnapshot{
			job("1", app.JobSnapshot{State: app.StateTransferring}),
			job("2", app.JobSnapshot{State: app.StateQueued}),
			job("3", app.JobSnapshot{State: app.StateDone}), // finished: not canceled
		},
		batch: app.BatchResult{Done: 1, Failed: 1, RefreshWarning: "refresh by hand"},
	}
	res := runExit(fake, &fakeClock{now: time.Unix(0, 0)}, true, exitBudget, quietLog())
	want := []string{"jobs", "cancel:1", "cancel:2", "wait", "close"}
	if got := fake.order(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] || got[4] != want[4] {
		t.Fatalf("calls %v, want %v", got, want)
	}
	if res.Canceled != 2 || res.TimedOut || res.Batch.Done != 1 || res.Batch.RefreshWarning != "refresh by hand" {
		t.Fatalf("%+v", res)
	}
}

func TestExitSequenceWithoutCancelDoesNotTouchJobs(t *testing.T) {
	fake := &exitFake{jobs: []app.JobSnapshot{job("1", app.JobSnapshot{State: app.StateTransferring})}}
	runExit(fake, &fakeClock{now: time.Unix(0, 0)}, false, exitBudget, quietLog())
	if got := fake.order(); len(got) != 2 || got[0] != "wait" || got[1] != "close" {
		t.Fatalf("calls %v, want wait then close", got)
	}
}

// A Wait that never returns is abandoned when the budget runs out, its context is canceled, and Close still
// runs afterwards.
func TestAStuckWaitIsAbandonedAtTheBudgetAndCloseStillRuns(t *testing.T) {
	fake := &exitFake{waitHold: make(chan struct{})}
	defer close(fake.waitHold)
	clk := &fakeClock{now: time.Unix(0, 0)}
	done := make(chan exitResult, 1)
	go func() { done <- runExit(fake, clk, false, exitBudget, quietLog()) }()

	var timer fakeTimer
	for i := 0; ; i++ {
		var ok bool
		if timer, ok = clk.timer(0); ok {
			break
		}
		time.Sleep(time.Millisecond)
		if i > 2000 {
			t.Fatal("the sequence never asked for a timer")
		}
	}
	if want := exitBudget - closeReserv; timer.d != want {
		t.Errorf("Wait is bounded by %v, want the budget minus the Close reserve, %v", timer.d, want)
	}
	timer.ch <- time.Now() // the Wait budget is used up
	res := <-done
	if !res.TimedOut {
		t.Error("a stuck Wait must be reported as a timeout")
	}
	if got := fake.order(); got[len(got)-1] != "close" {
		t.Errorf("Close must still run after a stuck Wait: %v", got)
	}
	fake.mu.Lock()
	ctx := fake.waitCtx
	fake.mu.Unlock()
	if ctx.Err() == nil {
		t.Error("the context given to Wait must be canceled when the budget runs out")
	}
}

func TestAStuckCloseDoesNotHoldTheExitPastTheBudget(t *testing.T) {
	block := make(chan struct{})
	svc := &blockingClose{exitFake: exitFake{}, block: block}
	defer close(block)
	clk := &fakeClock{now: time.Unix(0, 0)}
	done := make(chan exitResult, 1)
	go func() { done <- runExit(svc, clk, false, exitBudget, quietLog()) }()
	for i := 0; ; i++ {
		if timer, ok := clk.timer(1); ok { // timer 0 belongs to Wait, which returned at once
			timer.ch <- time.Now()
			break
		}
		time.Sleep(time.Millisecond)
		if i > 2000 {
			t.Fatal("Close was never bounded")
		}
	}
	if res := <-done; !res.TimedOut {
		t.Fatal("a stuck Close must be reported as a timeout")
	}
}

type blockingClose struct {
	exitFake
	block chan struct{}
}

func (b *blockingClose) Close() error { b.record("close"); <-b.block; return nil }

func TestExitReportsServiceErrorsWithoutFailing(t *testing.T) {
	fake := &exitFake{waitErr: errors.New("refresh failed"), closeErr: errors.New("flush failed")}
	res := runExit(fake, &fakeClock{now: time.Unix(0, 0)}, false, exitBudget, quietLog())
	if res.WaitErr == nil || res.CloseErr == nil || res.TimedOut {
		t.Fatalf("%+v", res)
	}
}
