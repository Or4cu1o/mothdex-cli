package app

import (
	"context"
	"math/rand/v2"
	"time"
)

// sysClock, sysSleeper and sysRand are the production implementations of the downloader ports.

type sysClock struct{}

func (sysClock) Now() time.Time { return time.Now() }

func (sysClock) NewTimer(d time.Duration) (<-chan time.Time, func() bool) {
	t := time.NewTimer(d)
	return t.C, t.Stop
}

type sysSleeper struct{}

func (sysSleeper) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sysRand only spreads retry delays; it is not a security boundary.
type sysRand struct{}

func (sysRand) Int63n(n int64) int64 { return rand.Int64N(n) } //nolint:gosec // retry jitter
