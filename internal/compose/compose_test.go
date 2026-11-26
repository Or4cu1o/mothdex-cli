package compose

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
)

func isolated(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "LOCALAPPDATA", "APPDATA", "USERPROFILE"} {
		t.Setenv(k, home)
	}
	return home
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func errCode(err error) string {
	if ae, ok := err.(*app.Error); ok { //nolint:errorlint // Open returns the *app.Error itself
		return ae.Code
	}
	return ""
}

// ui.md 2.1: a session (Shell, TUI, GUI) never applies a start-up -f or --allow-executable.
func TestStartupGrants(t *testing.T) {
	for _, c := range []struct {
		o           Options
		force, exec bool
	}{
		{Options{Force: true, AllowExecutable: true}, true, true},
		{Options{Force: true, AllowExecutable: true, Session: true}, false, false},
		{Options{}, false, false},
	} {
		if f, e := startupGrants(c.o); f != c.force || e != c.exec {
			t.Errorf("%+v: force=%v exec=%v", c.o, f, e)
		}
	}
}

// A stored download.on_exists=overwrite is only valid with --force on the command line: a session
// reports it once at start-up, where a one-shot command with -f accepts it.
func TestOpenForceAndSession(t *testing.T) {
	isolated(t)
	cfg := writeConfig(t, "download:\n  on_exists: overwrite\n")
	if s, err := Open(context.Background(), Options{ConfigFile: cfg, Force: true}); err != nil {
		t.Fatalf("one-shot -f: %v", err)
	} else {
		s.Close()
	}
	if s, err := Open(context.Background(), Options{ConfigFile: cfg, Force: true, Session: true}); err == nil {
		s.Close()
		t.Fatal("a session with -f must not accept a stored overwrite")
	} else if errCode(err) != app.CodeInvalidConfig {
		t.Fatalf("%v", err)
	}
}

// config set and config reset must be able to repair a broken file: Repair turns the failure into a warning.
func TestOpenRepairToleratesABrokenFile(t *testing.T) {
	isolated(t)
	cfg := writeConfig(t, "download: [not, a, mapping\n")
	if s, err := Open(context.Background(), Options{ConfigFile: cfg}); err == nil {
		s.Close()
		t.Fatal("a broken file must fail without Repair")
	}
	s, err := Open(context.Background(), Options{ConfigFile: cfg, Repair: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Warnings) == 0 {
		t.Fatal("Repair must warn that defaults are in use")
	}
}

// T-100: compose must wire both the filesystem probe and the resolved data directory.
func TestDetectProfileWiresMemoryBackedStorage(t *testing.T) {
	if runtime.GOOS != "linux" || !proxy.IsMemoryBacked("/dev/shm") {
		t.Skip("requires Linux tmpfs /dev/shm")
	}
	for _, tc := range []struct {
		name, rom, data string
		want            []string
	}{
		{"ROM root", "/dev/shm", "", []string{"Games folder is on a RAM disk: downloaded games are lost at reboot."}},
		{"data directory", "", "/dev/shm", []string{"Data folder is on a RAM disk: history, cache and settings are lost at reboot."}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var warnings []string
			info, detect := detectProfile(Options{Profile: "generic"}, config.Config{}, config.ResolvedPaths{ROMRoot: tc.rom, Data: tc.data}, filepath.Join(t.TempDir(), "config.yaml"), &warnings)
			if !slices.Equal(info.StorageWarnings, tc.want) {
				t.Fatalf("startup warnings %v, want %v", info.StorageWarnings, tc.want)
			}
			again, err := detect(context.Background())
			if err != nil || !slices.Equal(again.StorageWarnings, tc.want) {
				t.Fatalf("redetection %+v, %v", again, err)
			}
		})
	}
}

// --log-level is the shortcut of MOTHDEX_LOG_LEVEL: it wins over the variable, and takes the same values.
func TestOpenLogLevelFlagBeatsTheEnvironment(t *testing.T) {
	isolated(t)
	t.Setenv("MOTHDEX_LOG_LEVEL", "error")
	for _, c := range []struct {
		flag  string
		debug bool
	}{{"", false}, {"debug", true}} {
		sess, err := Open(context.Background(), Options{LogLevel: c.flag})
		if err != nil {
			t.Fatal(err)
		}
		if got := sess.Log.Enabled(context.Background(), slog.LevelDebug); got != c.debug {
			t.Errorf("LogLevel %q: debug enabled %v, want %v", c.flag, got, c.debug)
		}
		sess.Close()
	}
	if _, err := Open(context.Background(), Options{LogLevel: "loud"}); errCode(err) != app.CodeInvalidConfig {
		t.Errorf("an unknown level: %v", err)
	}
}
