package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// fake is a Service with one hook per call the tests care about.
type fake struct {
	search   func(app.SearchQuery) (app.SearchPage, error)
	entry    func(app.Ref) (app.Result[mothdex.Entry], error)
	links    func(app.Ref, app.LinkSelector) ([]app.LinkChoice, error)
	enqueue  func([]app.DownloadRequest) ([]app.JobID, error)
	wait     func() (app.BatchResult, error)
	reqs     []app.DownloadRequest
	selector app.LinkSelector
	boxart   func(app.Ref, string) (string, error)
}

func (f *fake) Search(_ context.Context, q app.SearchQuery) (app.SearchPage, error) {
	return f.search(q)
}

func (f *fake) Entry(_ context.Context, r app.Ref) (app.Result[mothdex.Entry], error) {
	return f.entry(r)
}

func (f *fake) Random(context.Context, app.RandomQuery) (app.Result[[]mothdex.Entry], error) {
	return app.Result[[]mothdex.Entry]{}, nil
}

func (f *fake) Platforms(context.Context, app.PlatformFilter) (app.Result[[]mothdex.Platform], error) {
	return app.Result[[]mothdex.Platform]{}, nil
}

func (f *fake) Regions(context.Context) (app.Result[[]mothdex.Region], error) {
	return app.Result[[]mothdex.Region]{}, nil
}

func (f *fake) DBInfo(context.Context) (app.Result[mothdex.DBInfo], error) {
	return app.Result[mothdex.DBInfo]{Value: mothdex.DBInfo{TotalEntries: 7}}, nil
}

func (f *fake) Links(_ context.Context, r app.Ref, s app.LinkSelector) ([]app.LinkChoice, error) {
	f.selector = s
	if f.links != nil {
		return f.links(r, s)
	}
	return []app.LinkChoice{{Index: 1, Available: true, Link: mothdex.Link{Size: 10}}}, nil
}

func (f *fake) Cover(context.Context, app.Ref) (io.ReadCloser, string, error) { return nil, "", nil }

func (f *fake) SaveBoxart(_ context.Context, r app.Ref, d string) (string, error) {
	if f.boxart != nil {
		return f.boxart(r, d)
	}
	return "", nil
}

func (f *fake) SaveJobBoxart(_ context.Context, j app.JobSnapshot, d string) (string, error) {
	if f.boxart != nil {
		return f.boxart(app.Ref{Slug: j.Slug}, d)
	}
	return "", nil
}

func (f *fake) Enqueue(_ context.Context, r []app.DownloadRequest) ([]app.JobID, error) {
	f.reqs = r
	if f.enqueue != nil {
		return f.enqueue(r)
	}
	ids := make([]app.JobID, len(r))
	for i := range r {
		ids[i] = app.JobID(string(rune('a' + i)))
	}
	return ids, nil
}

func (f *fake) Events(ctx context.Context) <-chan app.Event {
	ch := make(chan app.Event)
	go func() { <-ctx.Done(); close(ch) }()
	return ch
}

func (f *fake) Cancel(app.JobID) error { return nil }

func (f *fake) Wait(context.Context) (app.BatchResult, error) { return f.wait() }

func (f *fake) History(context.Context, app.HistoryFilter) ([]app.HistoryRecord, error) {
	return nil, nil
}
func (f *fake) CacheStats() (app.CacheStats, error)   { return app.CacheStats{}, nil }
func (f *fake) CacheClear(...string) error            { return nil }
func (f *fake) ConfigGet(string) (app.Setting, error) { return app.Setting{}, nil }
func (f *fake) ConfigList() ([]app.Setting, error)    { return nil, nil }
func (f *fake) ConfigSchema() []app.SettingSpec       { return nil }
func (f *fake) ConfigSet(string, string) error        { return nil }
func (f *fake) ConfigReset(string) error              { return nil }
func (f *fake) ActiveProfile() app.ProfileInfo        { return app.ProfileInfo{} }
func (f *fake) DetectProfile(context.Context) (app.ProfileInfo, error) {
	return app.ProfileInfo{}, nil
}
func (f *fake) Close() error { return nil }

type result struct {
	out, err bytes.Buffer
	code     int
}

// exec runs the command line against the fake; sess may adjust the session the commands see.
func exec(t *testing.T, f *fake, sess func(*cli.Session), args ...string) *result {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	r := &result{}
	d := cli.Deps{
		Open: func(_ context.Context, _ cli.Globals) (*cli.Session, error) {
			s := &cli.Session{Svc: f, T: tr, PageSize: 10, Color: "never"}
			if sess != nil {
				sess(s)
			}
			return s, nil
		},
		Translator: func(string) *i18n.Translator { return tr },
		Version:    func() buildinfo.Info { return buildinfo.Info{Version: "1.2.3", Go: "go1", OS: "linux", Arch: "amd64"} },
		Stdin:      strings.NewReader(""), Stdout: &r.out, Stderr: &r.err,
		IsTerminal: func(any) bool { return false }, Width: func() int { return 80 }, Getenv: func(string) string { return "" },
	}
	r.code = cli.Execute(context.Background(), d, args)
	return r
}

func page(titles ...string) app.SearchPage {
	p := app.SearchPage{Page: 1, TotalPages: 1, TotalResults: len(titles), Meta: app.ResultMeta{FetchedAt: time.Now()}}
	for i, title := range titles {
		p.Results = append(p.Results, app.IndexedEntry{Index: i + 1, Entry: mothdex.Entry{Slug: "s" + title[:1], Title: title, Platform: "nes", Regions: []string{"us"},
			Links: []mothdex.Link{{Type: "Game", SizeStr: "1 KB", Size: 1024, Filename: "f.zip"}}}})
	}
	return p
}

func searchWith(p app.SearchPage) *fake {
	return &fake{search: func(app.SearchQuery) (app.SearchPage, error) { return p, nil }}
}

func noop() (app.BatchResult, error) { return app.BatchResult{}, nil }

// Case 49 (CR-14): ANSI escapes, bidi controls and zero-width characters in API text never reach
// a terminal in human output; --json keeps the value, escaped by the encoder.
func TestHostileTextIsStrippedFromHumanOutputButKeptInJSON(t *testing.T) {
	hostile := "Evil\x1b]0;pwn\x07 Game" + string(rune(0x202E)) + "\u200bTitle"
	f := searchWith(page(hostile))
	r := exec(t, f, nil, "search", "evil")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.err.String())
	}
	for _, bad := range []string{"\x1b", "\x07", string(rune(0x202E)), "\u200b"} {
		if strings.Contains(r.out.String()+r.err.String(), bad) {
			t.Fatalf("control rune %q reached the terminal:\n%q", bad, r.out.String())
		}
	}
	j := exec(t, f, nil, "search", "evil", "--json")
	var env struct {
		Data struct {
			Results []struct{ Title string } `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(j.out.Bytes(), &env); err != nil || len(env.Data.Results) != 1 || env.Data.Results[0].Title != hostile {
		t.Fatalf("--json must keep the raw value: %v %q", err, j.out.String())
	}
	if strings.Contains(j.out.String(), "\x1b") {
		t.Fatal("the encoder must escape the ESC byte")
	}
}

// A hostile error message cannot smuggle escapes through the error block either, nor -v's cause.
func TestHostileErrorTextIsCleaned(t *testing.T) {
	f := &fake{search: func(app.SearchQuery) (app.SearchPage, error) {
		return app.SearchPage{}, &app.Error{Code: app.CodeNetwork, Message: "boom\x1b[31m red", Cause: errors.New("cause\x1b[0m")}
	}}
	r := exec(t, f, nil, "search", "x", "-v")
	if r.code != 4 || strings.Contains(r.err.String(), "\x1b") {
		t.Fatalf("exit %d, stderr %q", r.code, r.err.String())
	}
}

func TestExitCodesAndErrorBlock(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		want string
	}{
		{"not found", &app.Error{Code: app.CodeNotFound, Args: map[string]any{"ref": "zzz"}}, 3, "No entry found for zzz."},
		{"network", &app.Error{Code: app.CodeNetwork}, 4, "Cannot reach the Mothdex server."},
		{"internal", &app.Error{Code: app.CodeInternal}, 1, "code: internal"},
		{"invalid config", &app.Error{Code: app.CodeInvalidConfig}, 2, "code: invalid_config"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{search: func(app.SearchQuery) (app.SearchPage, error) { return app.SearchPage{}, c.err }}
			r := exec(t, f, nil, "search", "x")
			if r.code != c.code || !strings.Contains(r.err.String(), c.want) || r.out.Len() != 0 {
				t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.out.String(), r.err.String())
			}
		})
	}
	if r := exec(t, &fake{}, nil, "search", "--nope"); r.code != 2 || !strings.Contains(r.err.String(), "code: usage") {
		t.Fatalf("unknown flag: exit %d %q", r.code, r.err.String())
	}
}

func TestJSONErrorEnvelope(t *testing.T) {
	f := &fake{search: func(app.SearchQuery) (app.SearchPage, error) {
		return app.SearchPage{}, &app.Error{Code: app.CodeRateLimited, Message: "slow down", Retryable: true}
	}}
	r := exec(t, f, nil, "search", "x", "--json")
	var env map[string]any
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil {
		t.Fatalf("stdout must be one JSON document: %v\n%s", err, r.out.String())
	}
	e := env["error"].(map[string]any)
	if r.code != 4 || env["ok"] != false || env["data"] != nil || e["code"] != "rate_limited" || e["exit_code"] != float64(4) || e["retryable"] != true || env["schema_version"] != float64(1) {
		t.Fatalf("exit %d envelope %v", r.code, env)
	}
}

func TestPreferParsing(t *testing.T) {
	bad := [][]string{{"--prefer", "lng=En"}, {"--prefer", "lang"}, {"--prefer", "lang="}, {"--prefer", "size=tiny"}, {"--prefer", "size=smallest,largest"}}
	for _, b := range bad {
		args := append([]string{"download", "1"}, b...)
		if r := exec(t, &fake{}, nil, args...); r.code != 2 || !strings.Contains(r.err.String(), "code: usage") {
			t.Errorf("%v: exit %d %q", b, r.code, r.err.String())
		}
	}
	if r := exec(t, &fake{}, nil, "download", "1", "--link", "2", "--prefer", "lang=En"); r.code != 2 {
		t.Fatalf("--link with --prefer must be usage: exit %d", r.code)
	}
	f := &fake{wait: noop}
	base := app.Preference{Languages: []string{"En"}, Formats: []string{"chd"}}
	exec(t, f, func(s *cli.Session) { s.Prefer = base }, "download", "1", "--prefer", "lang=Pt,Ja", "--prefer", "lang=Es", "--prefer", "size=smallest")
	got := f.selector.Prefer
	if strings.Join(got.Languages, ",") != "Pt,Ja,Es" || strings.Join(got.Formats, ",") != "chd" || got.Size != app.SizeSmallest {
		t.Fatalf("--prefer replaces a key's list, keeps the others: %+v", got)
	}
}

func TestDownloadPassesOutputAndRefs(t *testing.T) {
	f := &fake{wait: noop}
	dir := t.TempDir()
	exec(t, f, nil, "download", "mario-slug", "7", "-o", dir)
	if len(f.reqs) != 2 || f.reqs[0].Ref.Slug != "mario-slug" || f.reqs[1].Ref.Index != 7 || f.reqs[0].DestDir != dir {
		t.Fatalf("%+v", f.reqs)
	}
}

func job(id, state string, err *app.Error) app.JobSnapshot {
	return app.JobSnapshot{ID: app.JobID(id), Slug: "s" + id, Title: "Game " + id, State: app.State(state), Path: "/roms/" + id + ".zip", Err: err, Started: time.Now(), Finished: time.Now()}
}

func TestBatchExitCodes(t *testing.T) {
	failure := &app.Error{Code: "size_mismatch", Message: "wrong size"}
	cases := []struct {
		name string
		res  app.BatchResult
		code int
	}{
		{"all done", app.BatchResult{Done: 2, Jobs: []app.JobSnapshot{job("a", "done", nil), job("b", "done", nil)}}, 0},
		{"skipped is success", app.BatchResult{Skipped: 1, Jobs: []app.JobSnapshot{job("a", "skipped", nil)}}, 0},
		{"partial is 7", app.BatchResult{Done: 1, Failed: 1, Jobs: []app.JobSnapshot{job("a", "done", nil), job("b", "failed", failure)}}, 7},
		{"one failure keeps its code", app.BatchResult{Failed: 1, Jobs: []app.JobSnapshot{job("a", "failed", failure)}}, 5},
		{"canceled is 130", app.BatchResult{Cancel: 1, Jobs: []app.JobSnapshot{job("a", "canceled", &app.Error{Code: "canceled"})}}, 130},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{wait: func() (app.BatchResult, error) { return c.res, nil }}
			if r := exec(t, f, nil, "download", "1", "2"); r.code != c.code {
				t.Fatalf("exit %d, want %d; stderr %q", r.code, c.code, r.err.String())
			}
			j := exec(t, f, nil, "download", "1", "2", "--json")
			var env struct {
				OK   bool `json:"ok"`
				Data struct {
					Jobs []any `json:"jobs"`
				} `json:"data"`
			}
			if err := json.Unmarshal(j.out.Bytes(), &env); err != nil || len(env.Data.Jobs) != len(c.res.Jobs) || env.OK != (c.code == 0) {
				t.Fatalf("a batch keeps its partial data in the envelope: %v %s", err, j.out.String())
			}
		})
	}
}

func TestQuietDownloadPrintsOnlyPathsOnStdout(t *testing.T) {
	f := &fake{wait: func() (app.BatchResult, error) {
		return app.BatchResult{Done: 1, Jobs: []app.JobSnapshot{job("a", "done", nil)}}, nil
	}}
	r := exec(t, f, nil, "download", "1", "-q")
	if r.out.String() != "/roms/a.zip\n" || r.err.Len() != 0 {
		t.Fatalf("stdout %q stderr %q", r.out.String(), r.err.String())
	}
}

// Confirmation (cli-conventions 6): never prompts with --json, -q or a piped stdin; fails with
// confirmation_required unless --force.
func TestConfirmationNeverPromptsWhenNonInteractive(t *testing.T) {
	confirm := func(s *cli.Session) { s.Confirm = true }
	for _, args := range [][]string{{"download", "1", "2"}, {"download", "1", "2", "--json"}, {"download", "1", "2", "-q"}} {
		f := &fake{wait: noop}
		r := exec(t, f, confirm, args...)
		if r.code != 2 || !strings.Contains(r.err.String(), "code: confirmation_required") || f.reqs != nil {
			t.Fatalf("%v: exit %d, queued=%v %q", args, r.code, f.reqs != nil, r.err.String())
		}
	}
	f := &fake{wait: noop}
	if r := exec(t, f, confirm, "download", "1", "2", "--force"); r.code != 0 || len(f.reqs) != 2 {
		t.Fatalf("--force skips the prompt: exit %d", r.code)
	}
	if r := exec(t, &fake{wait: noop}, confirm, "download", "1"); r.code != 0 {
		t.Fatalf("one small file needs no confirmation: exit %d", r.code)
	}
}

func TestDownloadSearchUsesTheFirstHitBySlug(t *testing.T) {
	f := &fake{search: func(app.SearchQuery) (app.SearchPage, error) { return page("Zelda", "Zelda II"), nil }, wait: noop}
	exec(t, f, nil, "download", "--search", "zelda")
	if len(f.reqs) != 1 || f.reqs[0].Ref.Slug != "sZ" {
		t.Fatalf("%+v", f.reqs)
	}
	if r := exec(t, searchWith(app.SearchPage{Page: 1}), nil, "download", "--search", "nothing"); r.code != 3 {
		t.Fatalf("no hit is not_found: exit %d", r.code)
	}
}

// Export (cli-output 5): csv guards formulas, and an existing file is never replaced without --force.
func TestSearchExportCSVGuardsFormulasAndRefusesToOverwrite(t *testing.T) {
	f := searchWith(page("=HYPERLINK(1)", "Safe"))
	dir := t.TempDir()
	target := filepath.Join(dir, "out.csv")
	if r := exec(t, f, nil, "search", "x", "-e", target); r.code != 0 {
		t.Fatalf("exit %d %s", r.code, r.err.String())
	}
	b, _ := os.ReadFile(target) //nolint:gosec // reads a file the test wrote under t.TempDir
	if !strings.Contains(string(b), "'=HYPERLINK(1)") || !strings.HasPrefix(string(b), "index,slug,title,platform,regions,serial,links,best_link_filename,best_link_size\n") {
		t.Fatalf("%s", b)
	}
	if r := exec(t, f, nil, "search", "x", "-e", target); r.code == 0 {
		t.Fatalf("an existing file must not be replaced without --force")
	}
	if got, _ := os.ReadFile(target); !bytes.Equal(got, b) { //nolint:gosec // reads a file the test wrote under t.TempDir
		t.Fatal("the existing export was modified")
	}
	if r := exec(t, f, nil, "search", "x", "-e", target, "--force"); r.code != 0 {
		t.Fatalf("--force replaces: exit %d", r.code)
	}
	if r := exec(t, f, nil, "search", "x", "-e", filepath.Join(dir, "noext")); r.code != 2 {
		t.Fatalf("an export format that cannot be inferred is usage: exit %d", r.code)
	}
}

func TestVersionOutput(t *testing.T) {
	r := exec(t, &fake{}, nil, "version")
	if r.code != 0 || !strings.HasPrefix(r.out.String(), "mothdex 1.2.3") {
		t.Fatalf("%d %q", r.code, r.out.String())
	}
	j := exec(t, &fake{}, nil, "--version", "--json")
	var env struct {
		Data buildinfo.Info `json:"data"`
		Meta struct {
			V string `json:"cli_version"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(j.out.Bytes(), &env); err != nil || env.Data.Version != "1.2.3" || env.Meta.V != "1.2.3" {
		t.Fatalf("%v %s", err, j.out.String())
	}
}

func TestTablesAreAlignedAndStdoutIsPipeClean(t *testing.T) {
	r := exec(t, searchWith(page("Mario", "日本語のゲーム")), nil, "search", "x")
	lines := strings.Split(strings.TrimRight(r.out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "#") || strings.Contains(r.out.String(), "Page 1/1") {
		t.Fatalf("stdout carries only the table:\n%s", r.out.String())
	}
	if !strings.Contains(r.err.String(), "Page 1/1") {
		t.Fatalf("the footer goes to stderr: %q", r.err.String())
	}
}

func TestSoonCommandsAndZeroResults(t *testing.T) {
	if r := exec(t, &fake{}, nil, "tui"); r.code != 0 || !strings.Contains(r.out.String(), "not available yet") {
		t.Fatalf("%d %q", r.code, r.out.String())
	}
	r := exec(t, searchWith(app.SearchPage{Page: 1}), nil, "search", "xyz")
	if r.code != 0 || r.out.Len() != 0 || !strings.Contains(r.err.String(), `No results for "xyz"`) {
		t.Fatalf("%d out=%q err=%q", r.code, r.out.String(), r.err.String())
	}
}

func TestBoxartCommandPrintsPath(t *testing.T) {
	f := &fake{entry: func(app.Ref) (app.Result[mothdex.Entry], error) {
		return app.Result[mothdex.Entry]{Value: mothdex.Entry{Slug: "mario"}}, nil
	}, boxart: func(_ app.Ref, d string) (string, error) { return filepath.Join(d, "mario.png"), nil }}
	dir := t.TempDir()
	if r := exec(t, f, nil, "boxart", "mario", "-o", dir); r.code != 0 || strings.TrimSpace(r.out.String()) != filepath.Join(dir, "mario.png") {
		t.Fatalf("%d %q %q", r.code, r.out.String(), r.err.String())
	}
}

// A translated message with a placeholder the service gave no value for never prints "{key}":
// the service's English text is shown instead and the broken hint is dropped.
func TestUnfilledPlaceholdersFallBackToTheServiceText(t *testing.T) {
	f := &fake{search: func(app.SearchQuery) (app.SearchPage, error) {
		return app.SearchPage{}, &app.Error{Code: app.CodeInvalidConfig, Message: "download.on_exists=overwrite cannot be stored"}
	}}
	r := exec(t, f, nil, "search", "x")
	if strings.ContainsAny(r.err.String(), "{}") || !strings.Contains(r.err.String(), "cannot be stored") || r.code != 2 {
		t.Fatalf("exit %d %q", r.code, r.err.String())
	}
}

// Sentinela (case 49): encoding/json writes C1 controls raw; U+009B is CSI and U+009D is OSC on
// xterm-class terminals. --json must escape them and still round-trip the value.
func TestJSONEscapesC1ControlsAndRoundTrips(t *testing.T) {
	title := "A" + string(rune(0x9b)) + "31mB" + string(rune(0x9d)) + "0;pwnC"
	r := exec(t, searchWith(page(title)), nil, "search", "x", "--json")
	for _, c1 := range []rune{0x9b, 0x9d, 0x85} {
		if strings.ContainsRune(r.out.String(), c1) {
			t.Fatalf("raw C1 rune %U in the JSON output", c1)
		}
	}
	if !strings.Contains(r.out.String(), `\u009b`) {
		t.Fatalf("the C1 rune must be a \\u escape: %s", r.out.String())
	}
	var env struct {
		Data struct {
			Results []struct{ Title string } `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil || env.Data.Results[0].Title != title {
		t.Fatalf("the value must round-trip: %v", err)
	}
}

// Alicerce m4 and m3: error.exit_code is the process exit code, and warnings reach the envelope.
func TestEnvelopeCarriesThePartialExitCodeAndWarnings(t *testing.T) {
	failure := &app.Error{Code: "network", Message: "down"}
	f := &fake{wait: func() (app.BatchResult, error) {
		return app.BatchResult{Done: 1, Failed: 1, RefreshWarning: "refresh skipped", Jobs: []app.JobSnapshot{job("a", "done", nil), job("b", "failed", failure)}}, nil
	}}
	r := exec(t, f, nil, "download", "1", "2", "--json")
	var env struct {
		Error struct {
			Code     string
			ExitCode int `json:"exit_code"`
		} `json:"error"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(r.out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if r.code != 7 || env.Error.ExitCode != 7 || env.Error.Code != "network" || len(env.Warnings) != 1 {
		t.Fatalf("exit %d envelope %+v", r.code, env)
	}
}

// An interrupted batch is 130 even when some jobs failed; a partial batch prints the per-job lines
// once and no second error block.
func TestInterruptedBatchWinsAndPartialBatchHasNoSecondBlock(t *testing.T) {
	failure := &app.Error{Code: "size_mismatch", Message: "wrong size"}
	f := &fake{wait: func() (app.BatchResult, error) {
		return app.BatchResult{Failed: 1, Cancel: 1, Jobs: []app.JobSnapshot{job("a", "failed", failure), job("b", "canceled", &app.Error{Code: "canceled"})}}, nil
	}}
	if r := exec(t, f, nil, "download", "1", "2"); r.code != 130 {
		t.Fatalf("exit %d", r.code)
	}
	f = &fake{wait: func() (app.BatchResult, error) {
		return app.BatchResult{Done: 1, Failed: 1, Jobs: []app.JobSnapshot{job("a", "done", nil), job("b", "failed", failure)}}, nil
	}}
	r := exec(t, f, nil, "download", "1", "2")
	if r.code != 7 || strings.Count(r.err.String(), "[size_mismatch]") != 1 || strings.Contains(r.err.String(), "code: size_mismatch") {
		t.Fatalf("exit %d, stderr:\n%s", r.code, r.err.String())
	}
}

// Crivo m2: a flag error at the root says "mothdex --help", not "mothdex mothdex --help".
func TestRootUsageHint(t *testing.T) {
	r := exec(t, &fake{}, nil, "--bogus")
	if r.code != 2 || !strings.Contains(r.err.String(), `Run "mothdex --help".`) || strings.Contains(r.err.String(), "mothdex mothdex") {
		t.Fatalf("exit %d %q", r.code, r.err.String())
	}
}

// Crivo m3: an export that cannot be written is a filesystem error (exit 6) with the path and no
// temporary name or Go text; an existing file gets the --force hint, not download.on_exists.
func TestExportWriteFailuresAreFilesystemErrors(t *testing.T) {
	r := exec(t, &fake{}, nil, "history", "-e", filepath.Join(t.TempDir(), "missing", "x.json"))
	if r.code != 6 || !strings.Contains(r.err.String(), "code: path_rejected") || strings.Contains(r.err.String(), ".mothdex-export") {
		t.Fatalf("exit %d %q", r.code, r.err.String())
	}
	target := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(target, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	r = exec(t, &fake{}, nil, "history", "-e", target)
	if r.code != 6 || !strings.Contains(r.err.String(), "--force") || strings.Contains(r.err.String(), "on_exists") {
		t.Fatalf("exit %d %q", r.code, r.err.String())
	}
}

// Crivo m1: a real downloader failure prints its localized message with the values filled in, never
// a raw {placeholder}.
func TestFailureLineNeverShowsRawPlaceholders(t *testing.T) {
	failure := &app.Error{Code: "permission_denied", Message: "cannot write"}
	f := &fake{wait: func() (app.BatchResult, error) {
		return app.BatchResult{Failed: 2, Jobs: []app.JobSnapshot{job("a", "failed", failure), job("b", "failed", failure)}}, nil
	}}
	r := exec(t, f, nil, "download", "1", "2")
	if strings.ContainsAny(r.err.String(), "{}") || !strings.Contains(r.err.String(), "[permission_denied]") {
		t.Fatalf("%q", r.err.String())
	}
}
