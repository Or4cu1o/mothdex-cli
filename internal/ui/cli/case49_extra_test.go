package cli_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const hostile = "Ev\x1b]52;c;ZXZpbA==\x07il\x1b[2J‮​X"

func noCtl(t *testing.T, what string, r *result) {
	t.Helper()
	for _, bad := range []string{"\x1b", "\x07", "‮", "​"} {
		if strings.Contains(r.out.String()+r.err.String(), bad) {
			t.Fatalf("%s: %q reached the terminal: out=%q err=%q", what, bad, r.out.String(), r.err.String())
		}
	}
}

func TestCase49DownloadAndInfoPaths(t *testing.T) {
	failed := &app.Error{Code: "x_unknown", Message: hostile}
	bad := job("a", "failed", failed)
	bad.Title = hostile
	good := job("b", "done", nil)
	good.Title, good.Path = hostile, "/roms/"+hostile+".zip"
	skip := job("c", "skipped", nil)
	skip.Path = "/roms/" + hostile
	res := app.BatchResult{Done: 1, Failed: 1, Skipped: 1, Jobs: []app.JobSnapshot{bad, good, skip}}
	f := &fake{wait: func() (app.BatchResult, error) { return res, nil }}
	noCtl(t, "download", exec(t, f, nil, "download", "1", "2", "3"))
	noCtl(t, "download -q", exec(t, f, nil, "download", "1", "2", "3", "-q"))
	info := &fake{entry: func(app.Ref) (app.Result[mothdex.Entry], error) {
		return app.Result[mothdex.Entry]{Value: mothdex.Entry{Slug: "s", Title: hostile, Platform: hostile, Serial: hostile, Regions: []string{hostile}}}, nil
	}}
	noCtl(t, "info", exec(t, info, nil, "info", "s"))
}

func execTTY(t *testing.T, f *fake, stdin string, args ...string) *result {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	r := &result{}
	d := cli.Deps{
		Open: func(_ context.Context, _ cli.Globals) (*cli.Session, error) {
			return &cli.Session{Svc: f, T: tr, PageSize: 10, Color: "never", Confirm: true}, nil
		},
		Translator: func(string) *i18n.Translator { return tr },
		Version:    func() buildinfo.Info { return buildinfo.Info{Version: "1.2.3"} },
		Stdin:      strings.NewReader(stdin), Stdout: &r.out, Stderr: &r.err,
		IsTerminal: func(any) bool { return true }, Width: func() int { return 80 }, Getenv: func(string) string { return "" },
	}
	r.code = cli.Execute(context.Background(), d, args)
	return r
}

func TestInteractiveConfirmation(t *testing.T) {
	for _, no := range []string{"n\n", "\n", "nope\n", ""} {
		f := &fake{wait: noop}
		if r := execTTY(t, f, no, "download", "1", "2"); r.code == 0 || f.reqs != nil {
			t.Fatalf("answer %q must cancel: exit %d queued=%v", no, r.code, f.reqs != nil)
		}
	}
	for _, yes := range []string{"y\n", "yes\n", "Y\n"} {
		f := &fake{wait: noop}
		if r := execTTY(t, f, yes, "download", "1", "2"); r.code != 0 || len(f.reqs) != 2 {
			t.Fatalf("answer %q must proceed: exit %d", yes, r.code)
		}
	}
}
