//go:build gui

package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

// The limit of the contract is 200 bytes, written here as a number so the test does not move with the constant it checks.
func TestProbeReasonIsCutAtTwoHundredBytes(t *testing.T) {
	defer func(old func() (sdl.Found, error)) { checkSDL = old }(checkSDL)
	checkSDL = func() (sdl.Found, error) { return sdl.Found{}, errors.New(strings.Repeat("x", 500)) }
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "--probe"}, &out, &errOut); code != 8 {
		t.Fatalf("exit %d", code)
	}
	if line := strings.TrimSuffix(errOut.String(), "\n"); len(line) != 200 {
		t.Fatalf("the reason is %d bytes, want 200", len(line))
	}
}

// The eligible answer is one line and nothing else, whatever SDL the machine running the test has.
func TestProbeEligibleOutputWithAStubbedCheck(t *testing.T) {
	defer func(old func() (sdl.Found, error)) { checkSDL = old }(checkSDL)
	checkSDL = func() (sdl.Found, error) { return sdl.Found{Version: sdl.Version{Major: 2, Minor: 32, Patch: 4}}, nil }
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "--probe"}, &out, &errOut); code != 0 || out.String() != "sdl 2.32.4\n" || errOut.Len() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

// The probe must give up before the installer does (its bound is 20 s, install.sh and install.ps1).
func TestProbeBudgetEndsBeforeTheInstallerBound(t *testing.T) {
	if probeBudget < 5*time.Second || probeBudget >= 20*time.Second {
		t.Fatalf("probeBudget %s: it must be below the installer's 20 s and long enough for a slow disk", probeBudget)
	}
}

// IC-13: a value that is not a plain word (a path, a home directory, a user name with a separator) is never copied.
func TestDiagnosticsNeverCopyAPathValue(t *testing.T) {
	for _, bad := range []string{"/home/alice", `C:\Users\alice`, "~/alice", "alice/x", "a b", strings.Repeat("a", 41)} {
		env := map[string]string{"SDL_VIDEODRIVER": bad, "LANG": bad, "LC_ALL": bad}
		d := diagnostics{SDLVersion: "2.32.4", Driver: bad, Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok }}
		if got := wordOrNot(bad); got != "not shown" {
			t.Errorf("value %q is shown as %q", bad, got)
		}
		line := ""
		for _, a := range d.attrs() {
			line += " " + strings.TrimSpace(toString(a))
		}
		for _, leak := range []string{"alice", "/home", `\Users`, "~/"} {
			if strings.Contains(line, leak) {
				t.Errorf("value %q leaks %q: %s", bad, leak, line)
			}
		}
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// The probe writes nothing: no file in the working directory, HOME, the XDG folders or the temp folder.
func TestProbeWritesNoFile(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"cwd", "home", "xdg", "tmp"} {
		if err := os.Mkdir(root+"/"+name, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root + "/cwd")
	t.Setenv("HOME", root+"/home")
	t.Setenv("USERPROFILE", root+"/home")
	t.Setenv("XDG_CONFIG_HOME", root+"/xdg")
	t.Setenv("XDG_DATA_HOME", root+"/xdg")
	t.Setenv("XDG_CACHE_HOME", root+"/xdg")
	t.Setenv("TMPDIR", root+"/tmp")
	t.Setenv("TEMP", root+"/tmp")
	defer func(old func() (sdl.Found, error)) { checkSDL = old }(checkSDL)
	for _, check := range []func() (sdl.Found, error){
		checkSDL, // the real check: eligible or not, depending on the machine
		func() (sdl.Found, error) { return sdl.Found{Version: sdl.Version{Major: 2, Minor: 32, Patch: 4}}, nil },
		func() (sdl.Found, error) { return sdl.Found{}, errors.New("no SDL2") },
	} {
		checkSDL = check
		var out, errOut bytes.Buffer
		run([]string{"mothdex-gui", "--probe"}, &out, &errOut)
		for _, name := range []string{"cwd", "home", "xdg", "tmp"} {
			entries, err := os.ReadDir(root + "/" + name)
			if err != nil || len(entries) != 0 {
				t.Errorf("%s holds %d entries after --probe (err %v)", name, len(entries), err)
			}
		}
	}
}
