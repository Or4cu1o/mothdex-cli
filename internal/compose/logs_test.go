package compose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

// A configuration that cannot be read never chooses where the log goes (TM-27).
func TestDiagnosticsOfABrokenConfigurationUsesTheDefaultFolder(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the default folders are redirected through XDG variables")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	file := filepath.Join(home, "config.yaml")
	planted := filepath.Join(home, "planted")
	if err := os.WriteFile(file, []byte("paths:\n  logs: "+planted+"\nlog:\n  sessions_keep: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := mlog.StartSession(mlog.SessionOptions{Binary: "cli", Build: buildinfo.Get()})
	defer func() { _ = logs.Close() }()
	o, err := Diagnostics(logs, file, "", "")
	if err == nil {
		t.Fatal("the broken configuration was not reported")
	}
	if o.Dir == planted || !strings.HasPrefix(o.Dir, home) || !strings.HasPrefix(logs.SessionPath(), o.Dir) {
		t.Errorf("dir %q, session %q", o.Dir, logs.SessionPath())
	}
	if _, err := os.Stat(planted); err == nil {
		t.Error("the folder of the broken configuration was created")
	}
}
