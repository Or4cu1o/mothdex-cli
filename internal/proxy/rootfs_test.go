package proxy

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newRoot(t *testing.T) *RootFS {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := OpenRootFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func put(t *testing.T, r *RootFS, name, body string) {
	t.Helper()
	f, err := r.OpenExcl(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(body)); err != nil || f.Close() != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func read(t *testing.T, r *RootFS, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Dir(), name)) //nolint:gosec // test helper on a temp dir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRealFinalizerNeverReplacesUnlessAsked(t *testing.T) {
	r := newRoot(t)
	if err := r.MkdirAll("d", 0o700); err != nil {
		t.Fatal(err)
	}
	put(t, r, "d/staging", "new")
	put(t, r, "d/final", "old")
	fin := NewFinalizer()

	err := fin.Rename(r, "d/staging", "d/final", false)
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no no-replace rename on this file system: %v", err)
	}
	if !errors.Is(err, fs.ErrExist) || read(t, r, "d/final") != "old" {
		t.Fatalf("err %v, final %q: the existing file must survive", err, read(t, r, "d/final"))
	}
	if err := fin.Rename(r, "d/staging", "d/free", false); err != nil || read(t, r, "d/free") != "new" {
		t.Fatalf("free name: %v", err)
	}
	put(t, r, "d/again", "newer")
	if err := fin.Rename(r, "d/again", "d/final", true); err != nil || read(t, r, "d/final") != "newer" {
		t.Fatalf("replace=true must replace: %v", err)
	}
}

func TestRealFinalizerStaysInsideTheRoot(t *testing.T) {
	r := newRoot(t)
	put(t, r, "a", "x")
	for _, to := range []string{"../escape", "/tmp/escape"} {
		if err := NewFinalizer().Rename(r, "a", to, false); err == nil {
			t.Errorf("%q: a name outside the root must fail", to)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Dir()), "escape")); err == nil {
		t.Fatal("a file escaped the root")
	}
}

func TestRealFSBasics(t *testing.T) {
	r := newRoot(t)
	put(t, r, "f", "x")
	if _, err := r.OpenExcl("f"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("OpenExcl on an existing name: %v", err)
	}
	if info, err := r.Lstat("f"); err != nil || info.Mode().Perm()&0o077 != 0 && runtime.GOOS != "windows" {
		t.Fatalf("staging files are 0600: %v %v", info, err)
	}
	if err := r.Link("f", "g"); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("link: %v", err)
	}
	free, dev, err := r.Free(".")
	if err != nil || free <= 0 || dev == 0 {
		t.Fatalf("free %d dev %d err %v", free, dev, err)
	}
	if err := r.SyncDir("."); err != nil {
		t.Fatalf("SyncDir: %v", err)
	}
	if _, err := r.OpenExcl("../x"); err == nil {
		t.Fatal("OpenExcl must refuse a name outside the root")
	}
}
