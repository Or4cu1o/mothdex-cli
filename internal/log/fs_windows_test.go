//go:build windows

package log

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAnotherUsersFolderOrFileIsRefused(t *testing.T) {
	dir := logDir(t)
	plant(t, dir, ".", LatestName)
	path := filepath.Join(dir, LatestName)
	if err := safeDir(dir); err != nil {
		t.Fatalf("the user's own folder: %v", err)
	}
	f, err := openFile(path, os.O_RDONLY)
	if err != nil {
		t.Fatalf("the user's own file: %v", err)
	}
	_ = f.Close()

	real := isOwn
	t.Cleanup(func() { isOwn = real })
	isOwn = func(*windows.SID) bool { return false }
	if err := safeDir(dir); !errors.Is(err, errUnsafeDir) {
		t.Errorf("another user's folder: %v", err)
	}
	if _, err := openFile(path, os.O_RDONLY); !errors.Is(err, errUnsafeDir) {
		t.Errorf("another user's file: %v", err)
	}
	f, _, err = createNew(dir, "fresh", ".log")
	if err != nil {
		t.Fatalf("a file this process creates: %v", err)
	}
	_ = f.Close()
}
