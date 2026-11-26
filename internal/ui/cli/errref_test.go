package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

// logged runs the command line against the fake with a real session log in a temporary folder,
// attached only when an error or a command asks for it.
func logged(t *testing.T, f *fake, args ...string) (*result, string) {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	dir := filepath.Join(t.TempDir(), "logs")
	logs := mlog.StartSession(mlog.SessionOptions{Binary: "cli", Args: args, Build: buildinfo.Info{Version: "1.2.3"}})
	t.Cleanup(func() { _ = logs.Close() })
	r := &result{}
	d := cli.Deps{
		Open: func(context.Context, cli.Globals) (*cli.Session, error) {
			return &cli.Session{Svc: f, T: tr, PageSize: 10, Color: "never", Logs: logs}, nil
		},
		Translator: func(string) *i18n.Translator { return tr },
		Logs:       logs,
		Diagnostics: func(cli.Globals) (mlog.BundleOptions, error) {
			err := logs.Attach(mlog.AttachOptions{Dir: dir})
			return mlog.BundleOptions{Dir: dir}, err
		},
		Stdin: strings.NewReader(""), Stdout: &r.out, Stderr: &r.err,
		IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" },
	}
	r.code = cli.Execute(context.Background(), d, args)
	return r, dir
}

var shownRef = regexp.MustCompile(`ref: ([0-9A-Z]{6})`)

// findOne returns the single log line of ref.
func findOne(t *testing.T, dir, ref string) string {
	t.Helper()
	matches, err := mlog.Find(dir, "", ref)
	if err != nil || len(matches) != 1 {
		t.Fatalf("ref %s: %d matches, %v", ref, len(matches), err)
	}
	for _, line := range matches[0].Context {
		if strings.Contains(line, "ref="+ref) {
			return line
		}
	}
	t.Fatalf("no line for %s", ref)
	return ""
}

func failingSearch() *fake {
	return &fake{search: func(app.SearchQuery) (app.SearchPage, error) {
		return app.SearchPage{}, &app.Error{Code: app.CodeNetwork, Message: "cannot reach the server", Cause: errors.New("no such host")}
	}}
}

func TestAShownErrorHasARefThatLeadsToItsLogLine(t *testing.T) {
	r, dir := logged(t, failingSearch(), "search", "mario")
	out := r.err.String()
	m := shownRef.FindStringSubmatch(out)
	if r.code != 4 || m == nil || !strings.Contains(out, "code: network - ref: ") || !strings.Contains(out, "log: ") || !strings.Contains(out, "latest.log") {
		t.Fatalf("exit %d: %q", r.code, out)
	}
	line := findOne(t, dir, m[1])
	for _, want := range []string{"level=ERROR", "code=network", "cmd=search", `cause="no such host"`} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
}

func TestTheJSONErrorCarriesTheSameRef(t *testing.T) {
	r, dir := logged(t, failingSearch(), "search", "mario", "--json")
	var env struct {
		Error struct{ Ref string } `json:"error"`
	}
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil {
		t.Fatal(err, r.out.String())
	}
	if m := shownRef.FindStringSubmatch(r.err.String()); m == nil || m[1] != env.Error.Ref {
		t.Fatalf("json %q, stderr %q", env.Error.Ref, r.err.String())
	}
	findOne(t, dir, env.Error.Ref)
}

func TestEachFailedJobHasItsOwnRef(t *testing.T) {
	batch := func() *fake {
		first := &app.Error{Code: app.CodeNetwork, Message: "first"}
		second := &app.Error{Code: app.CodeUpstream, Message: "second"}
		return &fake{wait: func() (app.BatchResult, error) {
			return app.BatchResult{Failed: 2, Jobs: []app.JobSnapshot{job("a", "failed", first), job("b", "failed", second)}}, nil
		}}
	}
	r, dir := logged(t, batch(), "download", "1", "2", "--force", "--json")
	var env struct {
		Error struct{ Ref string } `json:"error"`
		Data  struct {
			Jobs []struct {
				Error struct{ Ref string } `json:"error"`
			} `json:"jobs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil || len(env.Data.Jobs) != 2 {
		t.Fatal(err, r.out.String())
	}
	a, b := env.Data.Jobs[0].Error.Ref, env.Data.Jobs[1].Error.Ref
	if a == b || env.Error.Ref != a {
		t.Errorf("refs %q %q, envelope %q", a, b, env.Error.Ref)
	}
	findOne(t, dir, a)
	findOne(t, dir, b)

	human, dir := logged(t, batch(), "download", "1", "2", "--force")
	refs := shownRef.FindAllStringSubmatch(human.err.String(), -1)
	if len(refs) != 2 || strings.Count(human.err.String(), "log: ") != 1 {
		t.Fatalf("%q", human.err.String())
	}
	findOne(t, dir, refs[0][1])
	findOne(t, dir, refs[1][1])
}

func TestAnErrorBeforeTheServiceIsLoggedToo(t *testing.T) {
	r, dir := logged(t, &fake{}, "search", "--no-such-flag")
	m := shownRef.FindStringSubmatch(r.err.String())
	if r.code != 2 || m == nil {
		t.Fatalf("exit %d: %q", r.code, r.err.String())
	}
	if line := findOne(t, dir, m[1]); !strings.Contains(line, "code=usage") || !strings.Contains(line, "cmd=search") {
		t.Errorf("line = %s", line)
	}
}

func TestAPanicLeavesACrashFileAndARef(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "logs")
	logs := mlog.StartSession(mlog.SessionOptions{Binary: "cli"})
	t.Cleanup(func() { _ = logs.Close() })
	if err := logs.Attach(mlog.AttachOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	d := cli.Deps{Logs: logs, Stdout: &out, Stderr: &errOut, Translator: func(string) *i18n.Translator { return bundle.New("en_us") }}
	code := cli.ReportPanic(d, []string{"search"}, "boom https://h.example/x?token=S3CRET", []byte("goroutine 1 [running]:\nmain.boom()"))
	m := shownRef.FindStringSubmatch(errOut.String())
	if code != 1 || m == nil {
		t.Fatalf("exit %d: %q", code, errOut.String())
	}
	if line := findOne(t, dir, m[1]); !strings.Contains(line, "code=internal") || !strings.Contains(line, "main.boom") || strings.Contains(line, "S3CRET") {
		t.Errorf("line = %s", line)
	}
	crashes, err := filepath.Glob(filepath.Join(dir, mlog.CrashDir, "*.txt"))
	if err != nil || len(crashes) != 1 {
		t.Fatalf("crash files %v, %v", crashes, err)
	}
	body, err := os.ReadFile(crashes[0])
	if err != nil || !strings.Contains(string(body), "ref: "+m[1]) || !strings.Contains(string(body), "main.boom") || strings.Contains(string(body), "S3CRET") {
		t.Errorf("%v %s", err, body)
	}
}
