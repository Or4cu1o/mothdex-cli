package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupKeepsForeignFilesAndActiveStaging(t *testing.T) {
	u, s := fixture(t)
	old := filepath.Join(s.Dir, binaryName(false)+".old")
	foreign := filepath.Join(s.Dir, "foo.old")
	fakeStage := filepath.Join(s.Dir, ".mothdex-update-personal")
	for _, p := range []string{old, foreign} {
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(fakeStage, 0700); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(s.Dir, ".mothdex-update-")
	if err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(stage, binaryName(false))
	if err = os.WriteFile(payload, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = os.Chtimes(stage, past, past); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireLock(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanupOld(s.Dir, nil, time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC))
	if _, err = os.Stat(old); err != nil {
		t.Fatal("cleanup touched active update backup")
	}
	unlock()
	cleanupOld(s.Dir, nil, time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC))
	for _, p := range []string{old, stage} {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("did not clean %s: %v", p, err)
		}
	}
	for _, p := range []string{foreign, fakeStage} {
		if _, err = os.Stat(p); err != nil {
			t.Fatalf("foreign entry touched: %s", p)
		}
	}
	_ = u
}
func TestApplyHonorsCancelBoundary(t *testing.T) {
	u, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := u.Apply(ctx, ApplyOptions{Confirm: func(Staged) bool { t.Error("confirmation after cancellation"); return true }})
	errorCode(t, err, "canceled", "canceled")
}

func TestCleanupChecksExistingStageLockWithoutCreatingOne(t *testing.T) {
	_, s := fixture(t)
	stage, err := os.MkdirTemp(s.Dir, ".mothdex-update-")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireLock(stage)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err = os.Chtimes(stage, past, past); err != nil {
		t.Fatal(err)
	}
	cleanupOld(s.Dir, nil, time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC))
	if _, err = os.Stat(stage); err != nil {
		t.Fatal("removed active staging")
	}
	unlock()
	cleanupOld(s.Dir, nil, time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC))
	if _, err = os.Stat(stage); !os.IsNotExist(err) {
		t.Fatalf("stale unlocked stage retained: %v", err)
	}
	absent := t.TempDir()
	if fn, err := acquireExistingLock(absent); err == nil {
		fn()
		t.Fatal("created missing lock")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(absent)
	if err != nil || len(entries) != 0 {
		t.Fatal("existing-lock check wrote a file")
	}
}
