//go:build gui

package sdl

import (
	"errors"
	"testing"
)

// TestSmokeProbe is the headless check of docs/contracts/cli/gui.md section 2.2. It needs a system SDL2
// and runs on the dummy video driver, so it needs no display. Without SDL2 it is skipped: the CI jobs of
// the GUI smoke test install libsdl2 and run it on each architecture.
func TestSmokeProbe(t *testing.T) {
	rep, err := Probe(ProbeOptions{Driver: "dummy"})
	switch {
	case errors.Is(err, ErrNotFound):
		t.Skipf("no system SDL2 here: %v", err)
	case err != nil:
		t.Fatalf("%v\nsteps that passed: %v", err, rep.Steps)
	}
	if rep.Driver != "dummy" {
		t.Errorf("video driver %q, want dummy", rep.Driver)
	}
	if want := 8; len(rep.Steps) < want {
		t.Errorf("only %d steps passed: %v", len(rep.Steps), rep.Steps)
	}
	t.Logf("SDL %s, driver %s, %d functions, steps: %v", rep.Version, rep.Driver, len(rep.Functions), rep.Steps)
}

// With a library that cannot be found the probe must say so with the error the exit code is built on.
func TestProbeReportsAMissingLibraryAsNotFound(t *testing.T) {
	if _, err := Open(); err == nil {
		t.Skip("a system SDL2 exists here")
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open without SDL2 returned %v, want ErrNotFound", err)
	}
	_, err := Probe(ProbeOptions{})
	var pe *ProbeError
	if !errors.As(err, &pe) || pe.Step != "load" || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Probe error %v", err)
	}
}
