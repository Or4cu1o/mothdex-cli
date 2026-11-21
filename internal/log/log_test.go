package log

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test path under t.TempDir()
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestNewCreatesTheDirectoryAndFiltersByLevel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state", "logs") // does not exist yet
	logger, closer, err := New(Options{Level: "warn", File: true, Dir: dir, MaxSizeMB: 1, MaxFiles: 3})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("quiet")
	logger.Warn("loud", "key", "va lue")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(dir, FileName))
	if strings.Contains(got, "quiet") || !strings.Contains(got, `msg=loud key="va lue"`) {
		t.Fatalf("unexpected log content: %q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, FileName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestConsoleIsOptionalAndIndependentOfTheFile(t *testing.T) {
	var console bytes.Buffer
	dir := t.TempDir()
	logger, closer, err := New(Options{Level: "info", File: false, Console: true, Dir: dir, MaxSizeMB: 1, MaxFiles: 1, Stderr: &console})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hello")
	_ = closer.Close()
	if !strings.Contains(console.String(), "msg=hello") {
		t.Errorf("console = %q", console.String())
	}
	if exists(filepath.Join(dir, FileName)) {
		t.Error("log file created although log.file is false")
	}

	console.Reset()
	logger, closer, err = New(Options{Level: "info", File: true, Console: false, Dir: dir, MaxSizeMB: 1, MaxFiles: 1, Stderr: &console})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("file only")
	_ = closer.Close()
	if console.Len() != 0 {
		t.Errorf("console not empty with log.console false: %q", console.String())
	}
}

func TestRotationKeepsMaxFilesAndTheNewestLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	r, err := newRotator(path, 10, 3) // 10 bytes per file, 3 files in total
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"aaaaaaaa\n", "bbbbbbbb\n", "cccccccc\n", "dddddddd\n"} {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		path:        "dddddddd\n",
		path + ".1": "cccccccc\n",
		path + ".2": "bbbbbbbb\n",
	}
	for file, content := range want {
		if got := readFile(t, file); got != content {
			t.Errorf("%s = %q, want %q", filepath.Base(file), got, content)
		}
	}
	if exists(path + ".3") {
		t.Error("a fourth file was kept")
	}
}

func TestRotationWithOneFileTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r, err := newRotator(path, 10, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.Write([]byte("aaaaaaaa\n"))
	_, _ = r.Write([]byte("bbbbbbbb\n"))
	_ = r.Close()
	if got := readFile(t, path); got != "bbbbbbbb\n" || exists(path+".1") {
		t.Errorf("content %q, backup exists %v", got, exists(path+".1"))
	}
}

func TestReopeningContinuesTheSizeCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	r, _ := newRotator(path, 10, 2)
	_, _ = r.Write([]byte("aaaaaaaa\n"))
	_ = r.Close()
	r, err := newRotator(path, 10, 2) // a second run appends and must still rotate on size
	if err != nil {
		t.Fatal(err)
	}
	_, _ = r.Write([]byte("bbbbbbbb\n"))
	_ = r.Close()
	if readFile(t, path+".1") != "aaaaaaaa\n" || readFile(t, path) != "bbbbbbbb\n" {
		t.Error("size of the existing file was not counted")
	}
}

func TestAnUnusableLogDirectoryDoesNotStopTheLogger(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var console bytes.Buffer
	logger, closer, err := New(Options{Level: "info", File: true, Console: true, Dir: filepath.Join(blocker, "logs"), MaxSizeMB: 1, MaxFiles: 1, Stderr: &console})
	if err == nil {
		t.Fatal("expected an error for the log file")
	}
	logger.Info("still here")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(console.String(), "still here") {
		t.Errorf("console fallback missing: %q", console.String())
	}
	// No destination at all: still a usable logger.
	logger, _, _ = New(Options{Level: "info", File: false, Console: false})
	logger.Error("discarded")
}

func TestUnknownLevelFallsBackToInfoWithAnError(t *testing.T) {
	var console bytes.Buffer
	logger, _, err := New(Options{Level: "trace", Console: true, Stderr: &console})
	if !errors.Is(err, ErrLevel) {
		t.Fatalf("err = %v, want ErrLevel", err)
	}
	logger.Debug("hidden")
	logger.Info("shown")
	if strings.Contains(console.String(), "hidden") || !strings.Contains(console.String(), "shown") {
		t.Errorf("console = %q", console.String())
	}
}
