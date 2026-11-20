package fakeclock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeclock"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestAdvanceFiresOnlyDueTimers(t *testing.T) {
	clk := fakeclock.New(epoch)
	short := clk.After(time.Second)
	long := clk.After(time.Minute)
	clk.Advance(time.Second)
	select {
	case <-short:
	default:
		t.Fatal("due timer did not fire")
	}
	select {
	case <-long:
		t.Fatal("timer fired early")
	default:
	}
	if clk.Pending() != 1 || !clk.Now().Equal(epoch.Add(time.Second)) {
		t.Fatalf("pending = %d, now = %v", clk.Pending(), clk.Now())
	}
}

// Regression (Crivo, T-18 Minor 1): an abandoned wait must not stay pending.
func TestStoppedTimerIsNotPending(t *testing.T) {
	clk := fakeclock.New(epoch)
	_, stop := clk.NewTimer(time.Hour)
	if !stop() || clk.Pending() != 0 {
		t.Fatalf("stop did not remove the timer (pending %d)", clk.Pending())
	}
}

func TestSleepCanceledLeavesNoTimer(t *testing.T) {
	clk := fakeclock.New(epoch)
	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() { errc <- clk.Sleep(ctx, time.Hour) }()
	clk.BlockUntil(t, 1)
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep error = %v", err)
	}
	clk.BlockUntilIdle(t)
}

func TestSleepReturnsAfterAdvance(t *testing.T) {
	clk := fakeclock.New(epoch)
	errc := make(chan error, 1)
	go func() { errc <- clk.Sleep(t.Context(), time.Minute) }()
	clk.BlockUntil(t, 1)
	clk.Advance(time.Minute)
	if err := <-errc; err != nil {
		t.Fatalf("Sleep error = %v", err)
	}
}
