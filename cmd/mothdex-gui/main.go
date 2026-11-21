//go:build gui

// Command mothdex-gui is the SDL2 emulator-mode front end, a separate binary built CGO_ENABLED=0 with the
// gui build tag so that `mothdex` stays fully static (docs/contracts/cli/gui.md section 1).
//
// This is the spike: it only probes SDL2. It loads the system library, draws one rectangle on a hidden
// software window, reads a pixel back, round-trips one event, prints the SDL version and exits.
//
//	mothdex-gui [-driver dummy]
//
// Exit codes follow section 5 of the contract: 0 success, 1 SDL missing, too old, or no display, 3 an
// internal error (a check of the probe failed).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

const (
	exitOK       = 0
	exitSDL      = 1
	exitInternal = 3
)

func init() {
	// SDL calls must all come from one thread: the one that runs main.
	runtime.LockOSThread()
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mothdex-gui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	driver := fs.String("driver", "", "SDL video driver to force, for example dummy (default: SDL_VIDEODRIVER or SDL's choice)")
	if err := fs.Parse(args); err != nil {
		return exitInternal
	}

	rep, err := sdl.Probe(sdl.ProbeOptions{Driver: *driver})
	if rep.Version != (sdl.Version{}) { // known as soon as the library loaded, even if a later step fails
		_, _ = fmt.Fprintf(stdout, "SDL %s (needs %s or later, meant for %s or later)", rep.Version, rep.Minimum, rep.Target)
		if rep.Driver != "" {
			_, _ = fmt.Fprintf(stdout, ", video driver %s", rep.Driver)
		}
		_, _ = fmt.Fprintln(stdout)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mothdex-gui: %v\n", err)
		return exitCode(err)
	}
	for _, step := range rep.Steps {
		_, _ = fmt.Fprintf(stdout, "ok   %s\n", step)
	}
	_, _ = fmt.Fprintf(stdout, "probe ok: %d SDL2 functions, SDL %s\n", len(rep.Functions), rep.Version)
	return exitOK
}

// exitCode maps a probe error to the exit codes of the contract.
func exitCode(err error) int {
	for _, e := range []error{sdl.ErrNotFound, sdl.ErrIncompatible, sdl.ErrTooOld, sdl.ErrNoDisplay} {
		if errors.Is(err, e) {
			return exitSDL
		}
	}
	return exitInternal
}
