//go:build gui && crashtest && linux

package main

import (
	"bytes"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

// leavingBackend asks the program to end, as a frontend does, once the crash screen has been drawn.
type leavingBackend struct {
	loop.Backend
	frames *int
}

func (b leavingBackend) Present(layout draw.Layout, l *draw.List) error {
	err := b.Backend.Present(layout, l) // the second call panics
	if *b.frames++; *b.frames == 2 {    // the first frame of the run, then the first of the crash screen
		for _, text := range l.Texts() {
			if text == "Something went wrong" {
				_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			}
		}
	}
	return err
}

// The whole way of a forced panic, on a real SDL window: the crash file with the stack, the line with the ref in
// latest.log, no secret in either, the crash screen, and exit code 1.
func TestAForcedPanicLeavesACrashFileAndShowsTheCrashScreen(t *testing.T) {
	if _, err := sdl.Open(); err != nil {
		t.Skipf("no SDL2 here: %v", err)
	}
	home := isolate(t)
	crashing := wrapBackend
	exited := -1
	defer func(w func(loop.Backend) loop.Backend, e func(int)) { wrapBackend, exitProcess = w, e }(wrapBackend, exitProcess)
	frames := 0
	wrapBackend = func(b loop.Backend) loop.Backend { return leavingBackend{Backend: crashing(b), frames: &frames} }
	exitProcess = func(code int) { exited = code }

	var out, errOut bytes.Buffer
	run([]string{"mothdex-gui", "--driver", "dummy", "--window", "640x480", "--lang", "en_us"}, &out, &errOut)
	if exited != exitInternal {
		t.Fatalf("exit %d, want %d (stderr %q)", exited, exitInternal, errOut.String())
	}
	ref := refOf(t, errOut.String())
	crash := onlyFile(t, home, "crash")
	latest := readFile(t, onlyFile(t, home, "latest.log"))
	for name, text := range map[string]string{"crash file": readFile(t, crash), "latest.log": latest, "stderr": errOut.String()} {
		if strings.Contains(text, "CRASHTESTSECRET") {
			t.Errorf("the secret reached the %s:\n%s", name, text)
		}
	}
	if text := readFile(t, crash); !strings.Contains(text, "ref: "+ref) || !strings.Contains(text, "crashingBackend") {
		t.Errorf("the crash file lacks the ref %s or the stack:\n%s", ref, text)
	}
	if !strings.Contains(latest, "ref="+ref+" code=internal") {
		t.Errorf("latest.log lacks the line of %s:\n%s", ref, latest)
	}
}
