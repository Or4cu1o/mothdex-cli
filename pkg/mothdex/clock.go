package mothdex

import (
	"context"
	"time"
)

// Clock supplies the current time. Tests inject a fake.
type Clock interface{ Now() time.Time }

// Sleeper waits for d or until ctx ends, returning ctx.Err() in the latter case.
// Tests inject one that records the waits and returns at once.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type realSleeper struct{}

func (realSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
