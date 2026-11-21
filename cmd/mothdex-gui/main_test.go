//go:build gui

package main

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
)

func TestTheProbeNameSelectsProbeMode(t *testing.T) {
	for arg0, want := range map[string]bool{
		"mothdex-gui-probe":                       true,
		"/tmp/dist/mothdex-gui-probe":             true,
		`C:\dist\mothdex-gui-probe.exe`:           true,
		"./mothdex-gui-probe-amd64":               true,
		"mothdex-gui":                             false,
		"/usr/bin/mothdex-gui":                    false,
		`C:\Program Files\mothdex-gui.exe`:        false,
		"/home/mothdex-gui-probe-dir/mothdex-gui": false, // only the last element counts
	} {
		if got := probeByName(arg0); got != want {
			t.Errorf("probeByName(%q) = %v, want %v", arg0, got, want)
		}
	}
}

func TestBadFlagsExitWithTheInternalCode(t *testing.T) {
	for _, args := range [][]string{{"mothdex-gui", "-window", "nope"}, {"mothdex-gui", "-window", "10x10"}, {"mothdex-gui", "-nosuchflag"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitInternal {
			t.Errorf("%v: exit %d, want %d (%s)", args, code, exitInternal, errOut.String())
		}
	}
}

func TestHelpExitsZero(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "-h"}, &out, &errOut); code != exitOK {
		t.Fatalf("-h exits %d, want 0", code)
	}
	if errOut.Len() == 0 {
		t.Error("-h prints the usage")
	}
}

// When the exit sequence ran out of its budget the process must leave without closing the service again
// (PR-71 review, Major 3); otherwise it keeps what the normal path does.
func TestAStuckExitLeavesWithoutClosingTheServiceAgain(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var closed, exited []int
	closeBackend := func() { closed = append(closed, 1) }
	exit := func(code int) { exited = append(exited, code) }

	if leaveStuck(false, log, closeBackend, exit) || len(closed) != 0 || len(exited) != 0 {
		t.Fatal("a normal exit must not take the stuck path")
	}
	if !leaveStuck(true, log, closeBackend, exit) {
		t.Fatal("a timed out exit must take the stuck path")
	}
	if len(closed) != 1 || len(exited) != 1 || exited[0] != exitOK {
		t.Fatalf("backend closed %d times, exit calls %v: want one close and exit(0)", len(closed), exited)
	}
}
