package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// gui.md G3: Wait with nothing queued returns at once, and the profile refresh runs once after a
// batch that finished a download, not again on an idle Wait.
func TestWaitOnIdleReturnsAtOnceAndRefreshesOnlyAfterADownload(t *testing.T) {
	var refreshed atomic.Int32
	r := newDLRigEnv(t, DownloadConfig{}, func(e *Env) {
		e.Refresh = func(context.Context) error { refreshed.Add(1); return nil }
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if res, err := r.svc.Wait(ctx); err != nil || len(res.Jobs) != 0 || refreshed.Load() != 0 {
		t.Fatalf("idle: %+v %v refreshed=%d", res, err, refreshed.Load())
	}
	if res := r.run(t, "game"); res.Done != 1 || refreshed.Load() != 1 {
		t.Fatalf("after a download: %+v refreshed=%d", res, refreshed.Load())
	}
	if res, err := r.svc.Wait(ctx); err != nil || len(res.Jobs) != 0 || refreshed.Load() != 1 {
		t.Fatalf("idle again: %+v %v refreshed=%d", res, err, refreshed.Load())
	}
}
