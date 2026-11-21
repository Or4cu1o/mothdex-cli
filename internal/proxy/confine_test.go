package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func codeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestConfinerResolve(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root := filepath.Join(base, "roms")
	sibling := filepath.Join(base, "roms-old")
	outsideDir := filepath.Join(base, "elsewhere")
	for _, d := range []string{root, sibling, outsideDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	rfs, err := OpenRootFS(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rfs.Close() })
	isLauncher := func(p string) bool { return strings.Contains(filepath.ToSlash(p), "/ports") }
	c := &Confiner{FS: rfs, IsLauncher: isLauncher}

	ok := map[string]string{root: ".", filepath.Join(root, "snes"): "snes", filepath.Join(root, "a", "b"): "a/b"}
	for dest, want := range ok {
		if got, err := c.Resolve(dest); err != nil || got != want {
			t.Errorf("%s: got %q, %v, want %q", dest, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b")); err == nil {
		t.Error("Resolve must not create the directory: that happens with the staging file")
	}

	bad := map[string]Code{
		sibling:                           CodePathRejected, // string prefix trap
		filepath.Join(root, "..", "x"):    CodePathRejected,
		base:                              CodePathRejected,
		filepath.Join(root, "ports"):      CodePathRejected,
		filepath.Join(root, "ports", "x"): CodePathRejected,
		"":                                CodePathRejected,
	}
	for dest, want := range bad {
		if _, err := c.Resolve(dest); codeOf(err) != want {
			t.Errorf("%q: want %s, got %v", dest, want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "ports")); err == nil {
		t.Error("a refused destination must not be created")
	}
	lift := &Confiner{FS: rfs, IsLauncher: isLauncher, AllowExecutable: true}
	if _, err := lift.Resolve(filepath.Join(root, "ports")); err != nil {
		t.Errorf("--allow-executable lifts the launcher refusal: %v", err)
	}
}

func TestConfinerRefusesALinkOutsideTheRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlinks need privileges on Windows; junctions are covered by realfs tests")
	}
	base, _ := filepath.EvalSymlinks(t.TempDir())
	root, target := filepath.Join(base, "roms"), filepath.Join(base, "victim")
	for _, d := range []string{root, target} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, filepath.Join(root, "snes")); err != nil {
		t.Fatal(err)
	}
	rfs, _ := OpenRootFS(root)
	t.Cleanup(func() { _ = rfs.Close() })
	c := &Confiner{FS: rfs}
	for _, dest := range []string{filepath.Join(root, "snes"), filepath.Join(root, "snes", "sub")} {
		if _, err := c.Resolve(dest); codeOf(err) != CodePathRejected {
			t.Errorf("%s: want dest_outside_root, got %v", dest, err)
		}
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Fatal("nothing may be created behind the link")
	}
	// -o with the real path makes it the root: allowed.
	real, _ := OpenRootFS(target)
	t.Cleanup(func() { _ = real.Close() })
	if _, err := (&Confiner{FS: real}).Resolve(target); err != nil {
		t.Fatalf("the real path as -o must work: %v", err)
	}
}
