//go:build gui

// Command mothdex-gui is the SDL2 emulator-mode front end, a separate binary built CGO_ENABLED=0 with the
// gui build tag so that `mothdex` stays fully static (docs/contracts/cli/gui.md section 1).
//
//	mothdex-gui [--config file] [--profile id] [--lang xx_yy] [--log-level level] [--window WxH] [--driver name]
//	mothdex-gui --probe
//	mothdex-gui --version
//	mothdex-gui --spike [--driver dummy]
//	mothdex-gui --help
//
// With no flags it opens the full screen window and runs the GUI. --probe is the eligibility check the installer
// runs (docs/contracts/cli/ports.md section 5.2): it loads SDL2, checks the version, opens no window and says
// "sdl X.Y.Z" and exits 0, or says why and exits 8. --version prints the version line of `mothdex version` (name, version,
// short commit and date, from the ldflags of the release build) and exits 0, touching neither SDL2 nor the configuration.
// --spike checks the whole SDL2 binding on a hidden window and
// exits (the CI smoke test; an executable named mothdex-gui-probe does it with no flag). --window opens a window
// of that size instead of the full screen, for a desktop session. --log-level is a shortcut for MOTHDEX_LOG_LEVEL.
// --help prints the flags and exit codes in the interface language. Go takes one dash or two.
//
// A run that opens the GUI, or tries to, has a session log (docs/contracts/cli/logs.md); --probe, --version,
// --spike and --help write no file.
//
// Exit codes follow docs/contracts/cli/cli-output.md section 4.1: 0 normal exit or SIGTERM/SIGINT, 1 internal
// error, 2 configuration error (configuration, flags or profile), 8 SDL missing or too old, or no display.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

const (
	exitOK       = 0
	exitInternal = 1
	exitConfig   = 2
	exitSDL      = 8
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
	help     bool // -h, --help: print the help and exit
	version  bool // --version: print the version line and exit
	probe    bool // --probe: the installer's eligibility check
	spike    bool // --spike: the CI smoke test of the whole binding
	driver   string
	config   string
	profile  string
	lang     string
	logLevel string // --log-level: log.level for this run
	window   string // --window as written; width and height are its parsed form
	width    int
	height   int // zero: the full screen
}

// newFlags declares every flag. The flag package prints nothing: the help and the errors are written by this
// program, in the interface language (help.go). A test fails when a flag declared here has no help text.
func newFlags(o *options) *flag.FlagSet {
	fs := flag.NewFlagSet("mothdex-gui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	fs.BoolVar(&o.probe, "probe", false, "")
	fs.BoolVar(&o.spike, "spike", false, "")
	fs.StringVar(&o.driver, "driver", "", "")
	fs.StringVar(&o.config, "config", "", "")
	fs.StringVar(&o.profile, "profile", "", "")
	fs.StringVar(&o.lang, "lang", "", "")
	fs.StringVar(&o.logLevel, "log-level", "", "")
	fs.StringVar(&o.window, "window", "", "")
	return fs
}

// run takes os.Args: the name the program was started under, then its flags.
func run(argv []string, stdout, stderr io.Writer) (code int) {
	// A panic outside the normal mode (which logs its own, gui.go) must not leave with the code Go gives a
	// crash, 2, which the contract keeps for configuration errors.
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(stderr, "mothdex-gui: internal error: %s\n", oneLine(fmt.Sprint(r), reasonMax))
			code = exitInternal
		}
	}()
	var o options
	flags := newFlags(&o)
	if err := flags.Parse(argv[1:]); err != nil {
		key, args := parseProblem(err)
		return usageError(withLanguageFlags(o, argv[1:]), argv, stderr, key, args)
	}
	if o.help { // before the checks below and every mode: asking for help never fails on another flag's value
		return runHelp(o, stdout, stderr)
	}
	if flags.NArg() > 0 { // the program takes none, and parsing stops at one: the flags after it would be lost
		return usageError(withLanguageFlags(o, argv[1:]), argv, stderr, "ui.gui.cmdline.unexpected_argument", i18n.Args{"argument": flags.Arg(0)})
	}
	if o.window != "" {
		if _, err := fmt.Sscanf(o.window, "%dx%d", &o.width, &o.height); err != nil || o.width < 160 || o.height < 120 {
			return usageError(o, argv, stderr, "ui.gui.cmdline.bad_window", nil)
		}
	}
	switch {
	case o.version: // nothing is loaded, read or opened: it is the version of this binary and nothing else
		_, _ = fmt.Fprintln(stdout, buildinfo.Get().String())
		return exitOK
	case o.probe: // before anything else is read or opened: the installer runs this as root
		return runEligibility(stdout, stderr)
	case o.spike || probeByName(argv[0]):
		return runProbe(o.driver, stdout, stderr)
	}
	logs := startLog(argv[1:]) // before the configuration, so that its errors are logged too
	defer func() { _ = logs.Close() }()
	exe := executable(logs.Logger())
	if exe != "" {
		// What an earlier update left behind (update.md section 4). Not before the modes above: it writes a lock
		// file, and --probe, which the installer runs as root, writes nothing (ports.md section 5.2).
		update.CleanupOld(filepath.Dir(exe), logs.Logger())
	}
	return runGUI(o, logs, stderr)
}
