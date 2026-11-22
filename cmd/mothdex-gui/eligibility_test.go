//go:build gui

package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

func TestOneLineIsSafeToPrint(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "libSDL2-2.0.so.0: cannot open shared object file", "libSDL2-2.0.so.0: cannot open shared object file"},
		{"newlines and tabs become one space", "a\n\n b\t\tc\r\n", "a b c"},
		{"escape sequences cannot move the cursor", "x\x1b[2J\x1b]0;pwned\x07y", "x [2J ]0;pwned y"},
		{"a bidi override is dropped", "evil\u202egnp.exe", "evil gnp.exe"},
		{"empty", "", "unknown reason"},
		{"only control characters", "\n\x00\x1b", "unknown reason"},
		{"invalid UTF-8", "bad\xff\xfebytes", "bad bytes"},
	}
	for _, tc := range tests {
		if got := oneLine(tc.in, reasonMax); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestOneLineCutsToTheLimitOnACharacterBoundary(t *testing.T) {
	for _, in := range []string{strings.Repeat("a", 1000), strings.Repeat("é", 1000), strings.Repeat("日", 1000), strings.Repeat("a", 199) + "é"} {
		got := oneLine(in, reasonMax)
		if len(got) > reasonMax || !utf8.ValidString(got) || got == "" {
			t.Errorf("%d bytes in: %d bytes out, valid %v", len(in), len(got), utf8.ValidString(got))
		}
	}
	if got := oneLine(strings.Repeat("a", 200), reasonMax); len(got) != 200 {
		t.Errorf("exactly the limit is kept whole: %d", len(got))
	}
}

// Whatever SDL is or is not on the machine running the test, --probe keeps its contract: exit 0 with one line
// "sdl X.Y.Z" on stdout and nothing on stderr, or exit 1 with nothing on stdout and one clean line of reason.
func TestProbeKeepsItsContract(t *testing.T) {
	for _, flag := range []string{"--probe", "-probe"} { // Go takes both dashes
		var out, errOut bytes.Buffer
		code := run([]string{"mothdex-gui", flag}, &out, &errOut)
		switch code {
		case exitOK:
			if !regexp.MustCompile(`^sdl [0-9]+\.[0-9]+\.[0-9]+\n$`).MatchString(out.String()) || errOut.Len() != 0 {
				t.Errorf("%s eligible: stdout %q stderr %q", flag, out.String(), errOut.String())
			}
		case exitSDL:
			reason := errOut.String()
			line := strings.TrimSuffix(reason, "\n")
			if out.Len() != 0 || line == "" || strings.ContainsAny(line, "\n\r") || len(line) > reasonMax ||
				strings.IndexFunc(line, unicode.IsControl) >= 0 {
				t.Errorf("%s not eligible: stdout %q stderr %q", flag, out.String(), reason)
			}
		default:
			t.Errorf("%s: exit %d, want 0 or 1", flag, code)
		}
	}
}

func TestProbeSaysWhyInOneCleanLineWhenTheCheckFails(t *testing.T) {
	defer func(old func() (sdl.Version, error)) { checkSDL = old }(checkSDL)
	hostile := "libSDL2.so: wrong ELF class\n\x1b[31mERROR\x1b[0m " + strings.Repeat("x", 500)
	for _, err := range []error{errors.New(hostile), sdl.ErrTooOld, sdl.ErrNotFound} {
		checkSDL = func() (sdl.Version, error) { return sdl.Version{Major: 2, Minor: 0, Patch: 4}, err }
		var out, errOut bytes.Buffer
		if code := run([]string{"mothdex-gui", "--probe"}, &out, &errOut); code != exitSDL || out.Len() != 0 {
			t.Fatalf("exit %d, stdout %q", code, out.String())
		}
		line := strings.TrimSuffix(errOut.String(), "\n")
		if line == "" || len(line) > reasonMax || strings.IndexFunc(line, unicode.IsControl) >= 0 {
			t.Errorf("reason %q", errOut.String())
		}
	}
}

// A library that blocks must not hang the installer: the probe gives up by itself and says why.
func TestProbeGivesUpByItselfWhenSDLDoesNotAnswer(t *testing.T) {
	oldCheck, oldBudget, oldExit := checkSDL, probeBudget, watchdogExit
	defer func() { checkSDL, probeBudget, watchdogExit = oldCheck, oldBudget, oldExit }()
	release := make(chan struct{})
	var mu sync.Mutex
	var exited []int
	checkSDL = func() (sdl.Version, error) { <-release; return sdl.Version{}, errors.New("late") }
	probeBudget = 20 * time.Millisecond
	watchdogExit = func(code int) {
		mu.Lock()
		exited = append(exited, code)
		mu.Unlock()
		close(release) // the real exit would end the process; the test lets the blocked check return
	}
	var out bytes.Buffer
	errOut := &lockedBuffer{}
	run([]string{"mothdex-gui", "--probe"}, &out, errOut)
	mu.Lock()
	defer mu.Unlock()
	if len(exited) != 1 || exited[0] != exitSDL || !strings.Contains(errOut.String(), "did not answer") {
		t.Fatalf("exits %v, stderr %q", exited, errOut.String())
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuffer) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// The diagnostics line is what a tester pastes into a public issue: no value of a path list or a mapping, no
// path at all, and LD_PRELOAD and SDL_DYNAMIC_API only as set or unset (T-70 IC-13).
func TestDiagnosticsNameVariablesWithoutValuesAndLogNoPath(t *testing.T) {
	env := map[string]string{
		"LD_LIBRARY_PATH": "/home/alice/lib:/media/share", "LD_PRELOAD": `C:\Users\alice\evil.so`,
		"SDL_DYNAMIC_API": "/home/alice/libSDL2.so", "SDL_GAMECONTROLLERCONFIG": "03000000,Alice Pad,a:b0,",
		"SDL_GAMECONTROLLERCONFIG_FILE": "/home/alice/gamecontrollerdb.txt",
		"SDL_VIDEODRIVER":               "KMSDRM", "LANG": "pt_BR.UTF-8", "LC_ALL": "/home/alice/locale\n",
	}
	d := diagnostics{
		SDLVersion: "2.32.4", Driver: "KMSDRM", Getenv: func(k string) string { return env[k] },
		Mappings: input.MappingCounts{Embedded: 1200, User: 2, File: 0, Env: 1},
		Pads:     []input.ControllerType{input.TypeXbox360, input.TypePS5},
	}
	attrs := d.attrs()
	got := map[string]any{}
	var all strings.Builder
	for i := 0; i+1 < len(attrs); i += 2 {
		got[attrs[i].(string)] = attrs[i+1]
		fmt.Fprintf(&all, "%s=%v ", attrs[i], attrs[i+1])
	}
	for _, name := range setOnly {
		if got[name] != "set" {
			t.Errorf("%s is %v, want set", name, got[name])
		}
	}
	for _, leak := range []string{"alice", "/home", `\Users`, "evil", "media", "Pad,a:b0", "\n"} {
		if strings.Contains(all.String(), leak) {
			t.Errorf("the line leaks %q: %s", leak, all.String())
		}
	}
	want := map[string]any{
		"sdl": "2.32.4", "driver": "KMSDRM", "SDL_VIDEODRIVER_value": "KMSDRM", "LANG": "pt_BR.UTF-8", "LC_ALL": "not shown",
		"mappings_embedded": 1200, "mappings_user": 2, "mappings_file": 0, "mappings_env": 1, "controller_types": "1,7",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s is %v, want %v", k, got[k], v)
		}
	}
	for k := range map[string]bool{"LD_LIBRARY_PATH": true, "LD_PRELOAD": true, "SDL_DYNAMIC_API": true} {
		env[k] = ""
	}
	if after := d.attrs(); !contains(after, "LD_PRELOAD", "unset") || !contains(after, "SDL_DYNAMIC_API", "unset") {
		t.Errorf("unset variables are named unset: %v", after)
	}
	if got := (diagnostics{Getenv: func(string) string { return "" }}).attrs(); !contains(got, "controller_types", "none") {
		t.Errorf("no pad: %v", got)
	}
}

func contains(attrs []any, key, value string) bool {
	for i := 0; i+1 < len(attrs); i += 2 {
		if attrs[i] == key && attrs[i+1] == value {
			return true
		}
	}
	return false
}
