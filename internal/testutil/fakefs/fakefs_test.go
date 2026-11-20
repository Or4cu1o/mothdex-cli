package fakefs_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
)

func write(t *testing.T, f *fakefs.FS, name, data string) {
	t.Helper()
	fh, err := f.OpenExcl(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenExclRefusesExistingNamesAndSymlinks(t *testing.T) {
	f := fakefs.New(t)
	write(t, f, "a.part", "x")
	if _, err := f.OpenExcl("a.part"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("existing file: err = %v, want ErrExist", err)
	}
	victim := f.Path("victim")
	if err := os.WriteFile(victim, []byte("v"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, f.Path("link.part")); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	if _, err := f.OpenExcl("link.part"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("symlink at the name: err = %v, want ErrExist", err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "v" { //nolint:gosec // path built from t.TempDir
		t.Fatalf("victim changed: %q", got)
	}
}

func TestNamesCannotLeaveTheRoot(t *testing.T) {
	f := fakefs.New(t)
	if _, err := f.OpenExcl("../escape.part"); err == nil {
		t.Fatal("OpenExcl accepted a name outside the root")
	}
	if _, err := f.Lstat("../.."); err == nil {
		t.Fatal("Lstat accepted a name outside the root")
	}
}

func TestLimitWritesFillsTheDiskMidStream(t *testing.T) {
	f := fakefs.New(t)
	f.LimitWrites(10, fakefs.ErrNoSpace)
	fh, err := f.OpenExcl("a.part")
	if err != nil {
		t.Fatal(err)
	}
	n, err := fh.Write([]byte("0123456789ABCDEF"))
	if n != 10 || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Write = %d, %v; want 10 bytes then ENOSPC", n, err)
	}
	if n, err := fh.Write([]byte("x")); n != 0 || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("second Write = %d, %v", n, err)
	}
}

func TestFileIsReusableAcrossRetries(t *testing.T) {
	f := fakefs.New(t)
	fh, _ := f.OpenExcl("a.part")
	_, _ = fh.Write([]byte("partial"))
	if err := fh.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := fh.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	_, _ = fh.Write([]byte("full"))
	_ = fh.Close()
	if got, _ := os.ReadFile(f.Path("a.part")); string(got) != "full" {
		t.Fatalf("content = %q", got)
	}
}

func TestFailConsumesItsCountAndMatchesByName(t *testing.T) {
	f := fakefs.New(t)
	f.Fail(fakefs.OpMkdirAll, "roms/*", fakefs.ErrNoSpace, 1)
	if err := f.MkdirAll("other", 0o700); err != nil {
		t.Fatalf("non-matching name failed: %v", err)
	}
	if err := f.MkdirAll("roms/ps1", 0o700); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("first match: %v", err)
	}
	if err := f.MkdirAll("roms/ps1", 0o700); err != nil {
		t.Fatalf("fault should be spent: %v", err)
	}
}

func TestBeforeHookRunsOnceAndSeesTheWorldChange(t *testing.T) {
	f := fakefs.New(t)
	runs := 0
	f.Before(fakefs.OpRename, "final.zip", func() {
		runs++
		write(t, f, "final.zip", "second writer")
	})
	write(t, f, "a.part", "mine")
	err := fakefs.Finalizer{}.Rename(f, "a.part", "final.zip", false)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Rename = %v, want ErrExist after the race", err)
	}
	if got, _ := os.ReadFile(f.Path("final.zip")); string(got) != "second writer" {
		t.Fatalf("existing file was replaced: %q", got)
	}
	_ = fakefs.Finalizer{}.Rename(f, "a.part", "final.zip", false)
	if runs != 1 {
		t.Fatalf("hook ran %d times, want 1", runs)
	}
}

func TestFinalizerNoReplaceVersusReplace(t *testing.T) {
	f := fakefs.New(t)
	write(t, f, "a.part", "new")
	write(t, f, "final.zip", "old")
	if err := (fakefs.Finalizer{}).Rename(f, "a.part", "final.zip", false); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("no-replace over existing: %v", err)
	}
	if got, _ := os.ReadFile(f.Path("final.zip")); string(got) != "old" {
		t.Fatalf("target changed: %q", got)
	}
	if err := (fakefs.Finalizer{}).Rename(f, "a.part", "final.zip", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(f.Path("final.zip")); string(got) != "new" {
		t.Fatalf("replace did not replace: %q", got)
	}
}

func TestFallbackChainFaultsAreIndependent(t *testing.T) {
	f := fakefs.New(t)
	write(t, f, "a.part", "x")
	f.Fail(fakefs.OpRename, "*", fakefs.ErrNoReplaceUnsupported, 1)
	if err := (fakefs.Finalizer{}).Rename(f, "a.part", "final.zip", false); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Rename = %v, want ErrUnsupported", err)
	}
	f.Fail(fakefs.OpLink, "*", fakefs.ErrHardLinkUnsupported, 1)
	if err := f.Link("a.part", "final.zip"); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("Link = %v, want ErrUnsupported", err)
	}
	if err := f.Link("a.part", "final.zip"); err != nil { // fault spent: a real hard link
		t.Fatal(err)
	}
	if err := f.Link("a.part", "final.zip"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Link onto existing = %v, want ErrExist", err)
	}
}

func TestFreeUsesTheLongestVolumePrefix(t *testing.T) {
	f := fakefs.New(t)
	f.SetVolume(".", 1000, 1)
	f.SetVolume("dest", 10, 2)
	tests := []struct {
		name string
		free int64
		dev  uint64
	}{{"temp/x.part", 1000, 1}, {"dest/ps1", 10, 2}, {"dest", 10, 2}, {"destiny/x", 1000, 1}}
	for _, tc := range tests {
		free, dev, err := f.Free(tc.name)
		if err != nil || free != tc.free || dev != tc.dev {
			t.Errorf("Free(%q) = %d, %d, %v; want %d, %d", tc.name, free, dev, err, tc.free, tc.dev)
		}
	}
}

// Regression (Crivo, Minor 1): two concurrent no-replace renames onto one name
// must never both succeed.
func TestFinalizerNoReplaceIsAtomicUnderConcurrency(t *testing.T) {
	for round := range 200 {
		f := fakefs.New(t)
		write(t, f, "a.part", "a")
		write(t, f, "b.part", "b")
		errs := make(chan error, 2)
		for _, src := range []string{"a.part", "b.part"} {
			go func() { errs <- (fakefs.Finalizer{}).Rename(f, src, "final.bin", false) }()
		}
		wins := 0
		for range 2 {
			if err := <-errs; err == nil {
				wins++
			} else if !errors.Is(err, fs.ErrExist) {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: %d renames succeeded, want exactly 1", round, wins)
		}
	}
}

// Regression (Crivo, Minor 2): "*" must also hit staging files in sub-folders.
func TestFailPatternsCrossDirectories(t *testing.T) {
	f := fakefs.New(t)
	if err := f.MkdirAll("dest", 0o700); err != nil {
		t.Fatal(err)
	}
	f.Fail(fakefs.OpOpenExcl, "*", fakefs.ErrNoSpace, 0)
	if _, err := f.OpenExcl("dest/.job.mothdex-tmp"); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("nested name with *: %v", err)
	}
	g := fakefs.New(t)
	_ = g.MkdirAll("dest", 0o700)
	g.Fail(fakefs.OpOpenExcl, "*.part", fakefs.ErrNoSpace, 0)
	if _, err := g.OpenExcl("dest/x.part"); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("base-name pattern: %v", err)
	}
	if _, err := g.OpenExcl("dest/x.zip"); err != nil {
		t.Fatalf("non-matching name failed: %v", err)
	}
}

// Regression (Crivo, Minor 3): the catch-all loses to one-letter folders and a
// later registration of the same prefix wins.
func TestFreeVolumePrecedence(t *testing.T) {
	f := fakefs.New(t)
	f.SetVolume("", 100, 1)
	f.SetVolume("a", 5, 2)
	f.SetVolume("dest", 7, 3)
	f.SetVolume("dest", 9, 4)
	type vol struct {
		free int64
		dev  uint64
	}
	for name, want := range map[string]vol{"a/file": {5, 2}, "dest/x": {9, 4}, "other": {100, 1}} {
		free, dev, _ := f.Free(name)
		if (vol{free, dev}) != want {
			t.Errorf("Free(%q) = %d, %d; want %v", name, free, dev, want)
		}
	}
}

func TestLimitWritesCreditsTruncateAndRemove(t *testing.T) {
	f := fakefs.New(t)
	f.LimitWrites(10, fakefs.ErrDiskFullWindows)
	fh, _ := f.OpenExcl("a.part")
	if _, err := fh.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if _, err := fh.Write([]byte("x")); !errors.Is(err, fakefs.ErrDiskFullWindows) {
		t.Fatalf("full disk: %v", err)
	}
	_ = fh.Truncate(0) // a retry frees what the failed attempt wrote
	_, _ = fh.Seek(0, io.SeekStart)
	if n, err := fh.Write([]byte("0123456789")); n != 10 || err != nil {
		t.Fatalf("after truncate: %d, %v", n, err)
	}
	_ = fh.Close()
	if err := f.Remove("a.part"); err != nil {
		t.Fatal(err)
	}
	g, _ := f.OpenExcl("b.part")
	if n, err := g.Write([]byte("0123456789")); n != 10 || err != nil {
		t.Fatalf("after remove: %d, %v", n, err)
	}
}

func TestLimitWritesRefusesNilError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("LimitWrites accepted a nil error")
		}
	}()
	fakefs.New(t).LimitWrites(1, nil)
}

func TestSyncSpyAndCallLog(t *testing.T) {
	f := fakefs.New(t)
	fh, _ := f.OpenExcl("a.part")
	_ = fh.Sync()
	_ = fh.Sync()
	_ = fh.Close()
	_, _ = f.Lstat("a.part")
	if f.Syncs("a.part") != 2 {
		t.Fatalf("Syncs = %d", f.Syncs("a.part"))
	}
	calls := f.Calls()
	if len(calls) < 4 || calls[0].Op != fakefs.OpOpenExcl || calls[len(calls)-1].Op != fakefs.OpLstat {
		t.Fatalf("calls = %+v", calls)
	}
}
