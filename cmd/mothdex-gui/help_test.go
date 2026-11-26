//go:build gui

package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

// isolate keeps the machine's configuration and language out of a test.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "LOCALAPPDATA", "APPDATA", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	for _, k := range []string{"MOTHDEX_CONFIG", "MOTHDEX_INTERFACE_LANGUAGE", "MOTHDEX_LOG_LEVEL", "LC_ALL", "LC_MESSAGES", "LANGUAGE"} {
		t.Setenv(k, "")                        // registers the restore
		if err := os.Unsetenv(k); err != nil { // an empty MOTHDEX_ variable is a value, and an invalid one
			t.Fatal(err)
		}
	}
	t.Setenv("LANG", "C")
	return home
}

func configFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The numbers are the contract (cli-output.md 4.1), written here so the test does not move with the constants.
func TestExitCodesAreTheOnesOfTheContract(t *testing.T) {
	if exitOK != 0 || exitInternal != 1 || exitConfig != 2 || exitSDL != 8 {
		t.Fatalf("exit codes ok=%d internal=%d config=%d sdl=%d, want 0 1 2 8", exitOK, exitInternal, exitConfig, exitSDL)
	}
	for _, c := range []struct {
		name      string
		got, want int
	}{
		{"invalid config", configExitCode(&app.Error{Code: app.CodeInvalidConfig}), 2},
		{"usage", configExitCode(&app.Error{Code: app.CodeUsage}), 2},
		{"internal", configExitCode(&app.Error{Code: app.CodeInternal}), 1},
		{"not an app error", configExitCode(errors.New("x")), 1},
		{"no SDL2", sdlExitCode(sdl.ErrNotFound), 8},
		{"old SDL2", sdlExitCode(sdl.ErrTooOld), 8},
		{"no display", sdlExitCode(sdl.ErrNoDisplay), 8},
		{"anything else", sdlExitCode(errors.New("x")), 1},
	} {
		if c.got != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestEveryFlagHasEnglishAndPortugueseHelp(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, f := range helpFlags {
		listed[f.name] = f.value
	}
	var o options
	declared := 0
	newFlags(&o).VisitAll(func(f *flag.Flag) {
		if f.Name == "h" || f.Name == "help" {
			return
		}
		declared++
		value, ok := listed[f.Name]
		if !ok {
			t.Errorf("flag %s is not in helpFlags", f.Name)
		}
		keys := []string{helpKey + "flags." + strings.ReplaceAll(f.Name, "-", "_")}
		if value != "" && value != "xx_yy" {
			keys = append(keys, helpKey+"values."+value)
		}
		for _, locale := range []string{"en_us", "pt_br"} {
			for _, key := range keys {
				if bundle.Catalog(locale)[key] == "" {
					t.Errorf("%s lacks %s", locale, key)
				}
			}
		}
	})
	if declared != len(helpFlags) {
		t.Errorf("%d flags declared, %d in the help", declared, len(helpFlags))
	}
}

func TestHelpIsPrintedInTheChosenLanguageAndExitsZero(t *testing.T) {
	isolate(t)
	portuguese := configFile(t, "interface:\n  language: pt_br\n")
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"default", []string{"--help"}, "Exit codes:"},
		{"-h", []string{"-h"}, "Exit codes:"},
		{"--lang after --help", []string{"--help", "--lang", "pt_br"}, "Códigos de saída:"},
		{"interface.language", []string{"--config", portuguese, "--help"}, "Códigos de saída:"},
		{"--lang beats the configuration", []string{"--config", portuguese, "--lang", "en_us", "-help"}, "Exit codes:"},
		{"another flag's bad value", []string{"--window", "nope", "--help"}, "Exit codes:"},
		{"a broken configuration", []string{"--config", configFile(t, "interface: ["), "--help"}, "Exit codes:"},
	} {
		var out, errOut bytes.Buffer
		if code := run(append([]string{"mothdex-gui"}, c.args...), &out, &errOut); code != exitOK || errOut.Len() != 0 {
			t.Errorf("%s: exit %d, stderr %q", c.name, code, errOut.String())
		}
		got := out.String()
		for _, want := range []string{c.want, "--config", "--log-level", "MOTHDEX_LOG_LEVEL", "  8  "} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: the help lacks %q:\n%s", c.name, want, got)
			}
		}
		if strings.Contains(got, helpKey) {
			t.Errorf("%s: a message key reached the help:\n%s", c.name, got)
		}
	}
}

func TestABadCommandLineExitsTwoAndPointsAtTheHelp(t *testing.T) {
	isolate(t)
	for _, args := range [][]string{{"-window", "nope"}, {"-window", "10x10"}, {"-nosuchflag"}, {"--config"}} {
		var out, errOut bytes.Buffer
		if code := run(append([]string{"mothdex-gui"}, args...), &out, &errOut); code != exitConfig {
			t.Errorf("%v: exit %d, want %d (%s)", args, code, exitConfig, errOut.String())
		}
		if out.Len() != 0 || !strings.Contains(errOut.String(), "mothdex-gui --help") {
			t.Errorf("%v: stdout %q, stderr %q", args, out.String(), errOut.String())
		}
	}
}

// The language holds for the error wherever --lang stands, and the two errors a person can meet are translated.
func TestABadCommandLineIsReportedInTheChosenLanguage(t *testing.T) {
	isolate(t)
	portuguese := configFile(t, "interface:\n  language: pt_br\n")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--lang", "pt_br", "--bogus"}, "Opção desconhecida: -bogus"},
		{[]string{"--bogus", "--lang", "pt_br"}, "Opção desconhecida: -bogus"},
		{[]string{"--bogus", "-lang=pt_br"}, "Opção desconhecida: -bogus"},
		{[]string{"--bogus", "--config", portuguese}, "Opção desconhecida: -bogus"},
		{[]string{"--lang=pt_br", "--profile"}, "A opção -profile precisa de um valor."},
		{[]string{"--bogus"}, "Unknown flag: -bogus"},
		{[]string{"--probe=maybe"}, "invalid boolean value"},
		{[]string{"extra", "--probe"}, "Unexpected argument: extra"},
		{[]string{"--probe", "extra"}, "Unexpected argument: extra"},
		{[]string{"extra", "--lang", "pt_br"}, "Argumento inesperado: extra"},
		{[]string{"--", "-x"}, "Unexpected argument: -x"},
	} {
		var out, errOut bytes.Buffer
		if code := run(append([]string{"mothdex-gui"}, c.args...), &out, &errOut); code != exitConfig || !strings.Contains(errOut.String(), c.want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", c.args, code, errOut.String(), c.want)
		}
	}
}

// --log-level is validated as log.level, before SDL2 is loaded.
func TestAnUnknownLogLevelIsAConfigurationError(t *testing.T) {
	isolate(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "--log-level", "loud"}, &out, &errOut); code != exitConfig || !strings.Contains(errOut.String(), "log.level") {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
}

// Go ends a crashed process with 2, which the contract keeps for configuration errors.
func TestAPanicOutsideTheNormalModeExitsOne(t *testing.T) {
	defer func(old func() (sdl.Found, error)) { checkSDL = old }(checkSDL)
	checkSDL = func() (sdl.Found, error) { panic("boom\nsecond line") }
	var out, errOut bytes.Buffer
	if code := run([]string{"mothdex-gui", "--probe"}, &out, &errOut); code != exitInternal || strings.Count(errOut.String(), "\n") != 1 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
}
