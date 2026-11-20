// Package realfs holds helpers for tests that need a real directory tree:
// symlinks and junctions out of a root, a case-sensitivity probe, and a tree
// snapshot to prove that nothing outside the destination was touched
// (T-16 cases 16 to 19, 23, 25, 39).
package realfs

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Dir returns a fresh temp directory with symlinks in its path resolved, so
// comparisons against resolved paths (macOS /var, Windows 8.3 names) hold.
func Dir(t testing.TB) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("realfs: resolve temp dir: %v", err)
	}
	return dir
}

// WriteFile creates name (and its parents) with the given content.
func WriteFile(t testing.TB, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Symlink makes link point at target and skips the test when the platform or
// the current privileges do not allow it (Windows without developer mode).
func Symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("realfs: symlinks not available: %v", err)
	}
}

// IsCaseInsensitive reports whether dir's file system treats names that differ
// only in case as the same file.
func IsCaseInsensitive(t testing.TB, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "CaseProbe.tmp")
	WriteFile(t, probe, []byte("x"))
	defer func() { _ = os.Remove(probe) }()
	_, err := os.Lstat(filepath.Join(dir, "caseprobe.tmp"))
	return err == nil
}

// Snapshot describes every entry under dir (relative path, type, content hash,
// link target) so a test can assert that a tree is unchanged. It never follows
// symlinks or junctions.
func Snapshot(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			out[rel] = "symlink -> " + filepath.ToSlash(target)
		case info.IsDir():
			out[rel] = "dir"
		case info.Mode().IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[rel] = fmt.Sprintf("file %x", sha256.Sum256(data))
		default:
			out[rel] = "other " + info.Mode().String()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("realfs: snapshot %s: %v", dir, err)
	}
	return out
}

// DiffSnapshots returns the differences between two snapshots, one line per
// added, removed or changed entry, or nil when they are equal.
func DiffSnapshots(before, after map[string]string) []string {
	var diff []string
	for k, v := range before {
		switch w, ok := after[k]; {
		case !ok:
			diff = append(diff, "removed "+k)
		case w != v:
			diff = append(diff, "changed "+k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			diff = append(diff, "added "+k)
		}
	}
	return diff
}

// Names lists the entry names directly inside dir.
func Names(t testing.TB, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// HasSuffixFold reports whether any name ends with suffix, ignoring case.
func HasSuffixFold(names []string, suffix string) bool {
	for _, n := range names {
		if strings.HasSuffix(strings.ToLower(n), strings.ToLower(suffix)) {
			return true
		}
	}
	return false
}
