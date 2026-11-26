//go:build gui

package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

// reasonMax is the most bytes of reason --probe prints.
const reasonMax = 200

// These are variables only so a test can replace them.
var (
	// probeBudget is how long --probe waits for SDL2 before it gives up. The installer bounds the run too
	// (`timeout 20`), but a library that blocks in a constructor must not depend on that alone (T-70 IC-10).
	probeBudget = 15 * time.Second
	// watchdogExit ends the process when the probe ran out of its budget.
	watchdogExit = os.Exit
	// checkSDL is the check itself.
	checkSDL = sdl.Check
)

// runEligibility is `mothdex-gui --probe`, the check the installer runs on the staged binary before it installs
// the GUI (docs/contracts/cli/ports.md section 5.2, gap PRT-G3). It loads SDL2 as a normal start does, calls
// SDL_GetVersion, applies the version rule and says one line, sanitized and cut to reasonMax bytes like the reason:
//
//	exit 0, stdout "sdl X.Y.Z word" eligible; on Linux the word says where SDL2 was loaded from: system,
//	                                ld_library_path, other or unknown (never a path; PRT-G9)
//	exit 8, stderr the reason       not eligible (SDL2 missing, too old, or lacking a function; also a timeout)
//
// It opens no window, starts no video or input subsystem, reads no configuration, writes no file and no log:
// nothing runs before it but flag parsing. The reason is one line without control characters and at most
// reasonMax bytes, because the loader's own text (dlerror, a Windows error) is not ours to trust.
func runEligibility(stdout, stderr io.Writer) int {
	watchdog := time.AfterFunc(probeBudget, func() {
		_, _ = fmt.Fprintln(stderr, oneLine(fmt.Sprintf("SDL2 did not answer within %d seconds", int(probeBudget.Seconds())), reasonMax))
		watchdogExit(exitSDL)
	})
	defer watchdog.Stop()

	found, err := checkSDL()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, oneLine(err.Error(), reasonMax))
		return exitSDL
	}
	line := "sdl " + found.Version.String()
	if found.Origin != "" { // Linux: where the loader found it, one word and never a path (PRT-G9)
		line += " " + found.Origin
	}
	_, _ = fmt.Fprintln(stdout, oneLine(line, reasonMax))
	return exitOK
}

// oneLine makes text safe to print as one line: control and format characters (a newline, an escape sequence,
// a bidi override) become spaces, runs of spaces collapse, and the result is cut to at most max bytes on a
// character boundary. Empty text becomes "unknown reason", so the installer always has something to show.
func oneLine(s string, max int) string {
	var b strings.Builder
	space := true // drops the leading spaces
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) || r == unicode.ReplacementChar {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}
	out := strings.TrimRight(b.String(), " ")
	if out == "" {
		return "unknown reason"
	}
	if len(out) > max {
		cut := max
		for cut > 0 && !isRuneStart(out[cut]) { // never split a character
			cut--
		}
		out = strings.TrimRight(out[:cut], " ")
	}
	return out
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
