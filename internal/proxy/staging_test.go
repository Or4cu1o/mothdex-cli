package proxy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestOpenStagingFSRefusesSharedDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits do not exist on Windows")
	}
	base := t.TempDir()
	open := func(name string, mode os.FileMode) error {
		dir := filepath.Join(base, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		r, err := OpenStagingFS(dir)
		if err == nil {
			_ = r.Close()
		}
		return err
	}
	if err := open("private", 0o700); err != nil {
		t.Fatalf("a private temp directory must work: %v", err)
	}
	if err := open("shared", 0o777); err == nil {
		t.Fatal("a world-writable directory without a sticky bit must be refused")
	}
	if err := open("sticky", 0o777|os.ModeSticky); err != nil {
		t.Fatalf("a sticky shared directory (like /tmp) is fine: %v", err)
	}
	loose := filepath.Join(base, "loose")
	if err := os.MkdirAll(filepath.Join(loose, stagingDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(loose, stagingDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if r, err := OpenStagingFS(loose); err == nil {
		_ = r.Close()
		t.Fatal("a downloads directory open to others must be refused")
	}
}
