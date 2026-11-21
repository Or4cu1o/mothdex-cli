package uistate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesTheFolderAndReplacesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "state")
	for _, want := range []string{"first\n", "second\n"} {
		if err := WriteAtomic(path, []byte(want)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path) //nolint:gosec // a path inside t.TempDir()
		if err != nil || string(got) != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*"))
	if len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

func TestWriteAtomicFailsUnderARegularFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(filepath.Join(file, "sub", "x"), []byte("x")); err == nil {
		t.Fatal("expected an error")
	}
}
