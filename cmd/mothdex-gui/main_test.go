//go:build gui

package main

import (
	"bytes"
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
