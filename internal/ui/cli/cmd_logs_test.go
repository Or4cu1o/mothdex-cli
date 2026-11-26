package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

const fixtureSession = "20260101-000000-cli-1"

// logFixture writes one earlier session holding an error with the ref of the help examples, and
// wires d to it.
func logFixture(t *testing.T, d *Deps) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "logs")
	d.Diagnostics = func(Globals) (mlog.BundleOptions, error) {
		sessions := filepath.Join(dir, mlog.SessionsDir)
		if err := os.MkdirAll(sessions, 0o700); err != nil {
			return mlog.BundleOptions{}, err
		}
		line := `time=2026-01-01T00:00:00Z level=ERROR msg="cannot reach the server" ref=7F3K2Q code=network cmd=search` + "\n"
		err := os.WriteFile(filepath.Join(sessions, fixtureSession+".log"), []byte(line), 0o600)
		return mlog.BundleOptions{Dir: dir}, err
	}
	d.OpenFolder = func(string) error { return nil }
	return dir
}

func TestLogsCommands(t *testing.T) {
	d, out := helpDeps(t)
	dir := logFixture(t, &d)
	run := func(want int, args ...string) map[string]any {
		t.Helper()
		out.Reset()
		var stdout bytes.Buffer
		jd := d
		jd.Stdout = &stdout
		if code := Execute(context.Background(), jd, append(args, "--json")); code != want {
			t.Fatalf("%v: exit %d, want %d\n%s%s", args, code, want, stdout.String(), out.String())
		}
		var env struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
			t.Fatalf("%v: %v %s", args, err, stdout.String())
		}
		return env.Data
	}
	if got := run(0, "logs", "path"); got["dir"] != dir || got["latest"] != filepath.Join(dir, mlog.LatestName) {
		t.Errorf("path = %v", got)
	}
	if got := run(0, "logs", "last", "--tail", "1"); got["session"] != fixtureSession {
		t.Errorf("last = %v", got)
	}
	if got := run(0, "logs", "list"); len(got["sessions"].([]any)) != 1 {
		t.Errorf("list = %v", got)
	}
	if got := run(0, "logs", "show", "--ref", "7f3k2q"); got["session"] != fixtureSession || len(got["matches"].([]any)) != 1 {
		t.Errorf("show = %v", got)
	}
	run(3, "logs", "show", "--ref", "000000")
	run(2, "logs", "show", "--ref", "nope")
	run(2, "logs", "show")
	run(2, "logs", "show", "../../etc/passwd")
	run(2, "logs", "last", "--tail", "-1")
	run(2, "logs", "bundle", "--sessions", "0")
	run(2, "logs", "bundle", "-o", filepath.Join(dir, "missing"))
	target := t.TempDir()
	if got := run(0, "logs", "bundle", "-o", target); filepath.Dir(got["path"].(string)) != target {
		t.Errorf("bundle = %v", got)
	}
	run(2, "logs", "clear") // --json never prompts
	if _, err := os.Stat(filepath.Join(dir, mlog.SessionsDir, fixtureSession+".log")); err != nil {
		t.Fatal("clear without a confirmation deleted:", err)
	}
	if got := run(0, "logs", "clear", "-f"); got["removed"].(map[string]any)["sessions"] != float64(1) {
		t.Errorf("clear = %v", got)
	}
}

func TestLogsClearAsksAPerson(t *testing.T) {
	d, out := helpDeps(t)
	dir := logFixture(t, &d)
	d.IsTerminal = func(any) bool { return true }
	file := filepath.Join(dir, mlog.SessionsDir, fixtureSession+".log")
	for answer, code := range map[string]int{"n\n": 130, "\n": 130, "y\n": 0} {
		out.Reset()
		d.Stdin = strings.NewReader(answer)
		if got := Execute(context.Background(), d, []string{"logs", "clear"}); got != code {
			t.Errorf("answer %q: exit %d, want %d: %s", answer, got, code, out)
		}
		if _, err := os.Stat(file); (code == 0) != os.IsNotExist(err) {
			t.Errorf("answer %q: file %v", answer, err)
		}
	}
}

func TestHelpLogsTopicAndFolderOpener(t *testing.T) {
	d, out := helpDeps(t)
	for _, lang := range []string{"en_us", "pt_br"} {
		out.Reset()
		if code := Execute(context.Background(), d, []string{"--lang", lang, "help", "logs"}); code != 0 || !strings.Contains(out.String(), "logs bundle") {
			t.Errorf("%s: %d %s", lang, code, out)
		}
	}
	dir := filepath.Join(t.TempDir(), "a & b; rm")
	display := func(k string) string { return map[string]string{"DISPLAY": ":0"}[k] }
	for goos, program := range map[string]string{"windows": "explorer.exe", "darwin": "open", "linux": "xdg-open"} {
		name, args, err := openerFor(goos, display, dir)
		if err != nil || name != program || !slices.Equal(args, []string{dir}) {
			t.Errorf("%s: %s %q %v", goos, name, args, err)
		}
	}
	if _, _, err := openerFor("linux", func(string) string { return "" }, dir); err == nil {
		t.Error("a folder was opened without a display")
	}
	if _, _, err := openerFor("darwin", display, "--args"); err == nil {
		t.Error("a relative path became an argument")
	}
}
