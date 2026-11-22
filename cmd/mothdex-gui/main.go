//go:build gui

// Command mothdex-gui is the SDL2 emulator-mode front end, a separate binary built CGO_ENABLED=0 with the
// gui build tag so that `mothdex` stays fully static (docs/contracts/cli/gui.md section 1).
//
//	mothdex-gui [-config file] [-profile id] [-lang xx_yy] [-driver name] [-window WxH]
//	mothdex-gui --probe
//	mothdex-gui -spike [-driver dummy]
//
// With no flags it opens the full screen window and runs the GUI. --probe is the eligibility check the installer
// runs (docs/contracts/cli/ports.md section 5.2): it loads SDL2, checks the version, opens no window and says
// "sdl X.Y.Z" and exits 0, or says why and exits 1. -spike checks the whole SDL2 binding on a hidden window and
// exits (the CI smoke test; an executable named mothdex-gui-probe does it with no flag). -window opens a window
// of that size instead of the full screen, for a desktop session.
//
// Exit codes follow section 5 of the contract: 0 normal exit or SIGTERM, 1 SDL missing, too old or no
// display, 2 configuration error, 3 internal error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	exitOK       = 0
	exitSDL      = 1
	exitConfig   = 2
	exitInternal = 3
)

func init() {
	// SDL calls must all come from one thread: the one that runs main.
	runtime.LockOSThread()
}

func main() { os.Exit(run(os.Args, os.Stdout, os.Stderr)) }

// probeName is the start of the executable name that makes the binary probe with no flags: scripts/gui-probe.sh
// builds it as mothdex-gui-probe, and the CI legs (qemu, the DLL planting checks) run it by that name.
const probeName = "mothdex-gui-probe"

// probeByName reports whether the program was started under the probe's name (with or without .exe).
func probeByName(arg0 string) bool {
	return strings.HasPrefix(filepath.Base(strings.ReplaceAll(arg0, "\\", "/")), probeName)
}

type options struct {
	probe   bool // --probe: the installer's eligibility check
	spike   bool // -spike: the CI smoke test of the whole binding
	driver  string
	config  string
	profile string
	lang    string
	width   int
	height  int // zero: the full screen
}

// run takes os.Args: the name the program was started under, then its flags.
func run(argv []string, stdout, stderr io.Writer) int {
	args := argv[1:]
	fs := flag.NewFlagSet("mothdex-gui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	var window string
	fs.BoolVar(&o.probe, "probe", false, "check that SDL2 can be used (no window): print sdl X.Y.Z and exit 0, or the reason and exit 1")
	fs.BoolVar(&o.spike, "spike", false, "check the whole SDL2 binding on a hidden window and exit (the CI smoke test)")
	fs.StringVar(&o.driver, "driver", "", "SDL video driver to force, for example dummy (default: SDL_VIDEODRIVER or SDL's choice)")
	fs.StringVar(&o.config, "config", "", "configuration file (default: the standard location)")
	fs.StringVar(&o.profile, "profile", "", "frontend profile id (default: detected)")
	fs.StringVar(&o.lang, "lang", "", "interface language, such as pt_br (default: interface.language)")
	fs.StringVar(&window, "window", "", "open a window of WxH pixels instead of the full screen, such as 1280x720")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) { // -h prints the usage and is not a failure
			return exitOK
		}
		return exitInternal
	}
	if window != "" {
		if _, err := fmt.Sscanf(window, "%dx%d", &o.width, &o.height); err != nil || o.width < 160 || o.height < 120 {
			_, _ = fmt.Fprintf(stderr, "mothdex-gui: -window wants WxH, such as 1280x720\n")
			return exitInternal
		}
	}
	switch {
	case o.probe: // before anything else is read or opened: the installer runs this as root
		return runEligibility(stdout, stderr)
	case o.spike || probeByName(argv[0]):
		return runProbe(o.driver, stdout, stderr)
	}
	return runGUI(o, stderr)
}
