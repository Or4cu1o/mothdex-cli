//go:build gui

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

// runProbe is `mothdex-gui -spike`: the headless check of the SDL2 binding that the CI runs as the smoke
// test (docs/contracts/cli/gui.md section 2). It loads the system library, draws one rectangle on a hidden
// software window, reads a pixel back, round-trips one event, prints the SDL version and exits.
func runProbe(driver string, stdout, stderr io.Writer) int {
	rep, err := sdl.Probe(sdl.ProbeOptions{Driver: driver})
	if rep.Version != (sdl.Version{}) { // known as soon as the library loaded, even if a later step fails
		_, _ = fmt.Fprintf(stdout, "SDL %s (needs %s or later, meant for %s or later)", rep.Version, rep.Minimum, rep.Target)
		if rep.Driver != "" {
			_, _ = fmt.Fprintf(stdout, ", video driver %s", rep.Driver)
		}
		_, _ = fmt.Fprintln(stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		return sdlExitCode(err)
	}
	for _, step := range rep.Steps {
		_, _ = fmt.Fprintf(stdout, "ok   %s\n", step)
	}
	_, _ = fmt.Fprintf(stdout, "probe ok: %d SDL2 functions, SDL %s\n", len(rep.Functions), rep.Version)
	return exitOK
}

// sdlExitCode maps an SDL error to the exit codes of the contract: 1 for a missing or old SDL2 or no display,
// 3 for anything else.
func sdlExitCode(err error) int {
	for _, e := range []error{sdl.ErrNotFound, sdl.ErrSearchPath, sdl.ErrIncompatible, sdl.ErrTooOld, sdl.ErrNoDisplay} {
		if errors.Is(err, e) {
			return exitSDL
		}
	}
	return exitInternal
}
