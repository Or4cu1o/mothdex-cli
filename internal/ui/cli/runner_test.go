package cli_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// shellRig is a Runner over the fake service, as the Shell uses it.
type shellRig struct {
	r        *cli.Runner
	out, err bytes.Buffer
	prompts  []string
}

func newShellRig(t *testing.T, f *fake, interactive bool, answers ...string) *shellRig {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	rig := &shellRig{}
	d := cli.Deps{
		Translator: func(string) *i18n.Translator { return tr },
		Version:    func() buildinfo.Info { return buildinfo.Info{Version: "1.2.3"} },
		Stdin:      strings.NewReader(""), Stdout: &rig.out, Stderr: &rig.err,
		IsTerminal: func(any) bool { return false }, Width: func() int { return 80 }, Getenv: func(string) string { return "" },
	}
	rig.r = cli.NewRunner(d, &cli.Session{Svc: f, T: tr, PageSize: 10, Color: "never"}, interactive)
	rig.r.SetPrompt(func(label string) (string, error) {
		rig.prompts = append(rig.prompts, label)
		if len(answers) == 0 {
			return "", cli.ErrPromptAborted
		}
		a := answers[0]
		answers = answers[1:]
		if a == "<abort>" {
			return "", cli.ErrPromptAborted
		}
		return a, nil
	})
	return rig
}

// manyPages answers every search with a page n of `of`, echoing the query it was asked.
func manyPages(n, of int) *fake {
	return &fake{search: func(q app.SearchQuery) (app.SearchPage, error) {
		p := page("Zelda", "Mario")
		p.Query, p.Page, p.TotalPages, p.TotalResults = q, n, of, 20
		return p, nil
	}}
}

func TestRunnerRemembersTheLastPage(t *testing.T) {
	f := manyPages(2, 4)
	rig := newShellRig(t, f, false)
	if _, ok := rig.r.LastPage(); ok {
		t.Fatal("a page before any search")
	}
	if code := rig.r.Run(context.Background(), []string{"search", "mario", "-p", "nes"}); code != 0 {
		t.Fatalf("exit %d: %s", code, rig.err.String())
	}
	got, ok := rig.r.LastPage()
	if !ok || got.Page != 2 || got.Query.Text != "mario" || !reflect.DeepEqual(got.Query.Platforms, []string{"nes"}) {
		t.Fatalf("last page %+v, %v", got, ok)
	}

	q := got.Query
	q.Page = 3
	if code := rig.r.RunSearch(context.Background(), q); code != 0 {
		t.Fatalf("exit %d: %s", code, rig.err.String())
	}
	if got, _ := rig.r.LastPage(); got.Query.Page != 3 {
		t.Fatalf("RunSearch did not update the last page: %+v", got.Query)
	}
	if !strings.Contains(rig.out.String(), "Zelda") {
		t.Fatalf("RunSearch did not render the table: %q", rig.out.String())
	}
}

func TestRunnerSearchFooterInsideTheShell(t *testing.T) {
	tests := []struct {
		name         string
		page, of     int
		want, absent string
	}{
		{"middle page", 2, 4, "Page 2/4 · 20 results · n next · p prev · <#> info · d <#> download", ""},
		{"first page", 1, 4, "Page 1/4 · 20 results · n next · <#> info · d <#> download", "p prev"},
		{"last page", 4, 4, "Page 4/4 · 20 results · p prev · <#> info · d <#> download", "n next"},
		{"a single page has no shortcuts", 1, 1, "Page 1/1 · 20 results", "n next"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newShellRig(t, manyPages(tc.page, tc.of), false)
			rig.r.Run(context.Background(), []string{"search", "mario"})
			if !strings.Contains(rig.err.String(), tc.want) || (tc.absent != "" && strings.Contains(rig.err.String(), tc.absent)) {
				t.Fatalf("footer %q, want %q without %q", rig.err.String(), tc.want, tc.absent)
			}
			if strings.Contains(rig.err.String(), "mothdex search") {
				t.Fatalf("the CLI hint leaked into the shell: %q", rig.err.String())
			}
		})
	}
}

func TestRunnerSaysWhenIndexesChange(t *testing.T) {
	rig := newShellRig(t, manyPages(1, 4), false)
	ctx := context.Background()
	rig.r.Run(ctx, []string{"search", "mario"})
	rig.r.Run(ctx, []string{"search", "mario", "--page", "2"}) // same search, other page
	if strings.Contains(rig.err.String(), "Indexes now refer") {
		t.Fatalf("paging is not a new search: %q", rig.err.String())
	}
	rig.r.Run(ctx, []string{"search", "zelda"})
	if !strings.Contains(rig.err.String(), "Indexes now refer to this search.") {
		t.Fatalf("a new search must say the indexes changed: %q", rig.err.String())
	}
}

func TestRunnerRejectsWhatOnlyStartupCanSet(t *testing.T) {
	tests := [][]string{
		{"search", "x", "--lang", "pt_br"},
		{"search", "x", "--config", "/tmp/c.yaml"},
		{"download", "1", "--allow-executable"},
		{"download", "1", "-j", "2"},
		{"download", "1", "--jobs", "2"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := manyPages(1, 1)
			f.wait = noop
			rig := newShellRig(t, f, false)
			if code := rig.r.Run(context.Background(), args); code != 2 {
				t.Fatalf("exit %d, want 2 (usage)", code)
			}
			if !strings.Contains(rig.err.String(), "not available in the Shell; run the command outside it.") {
				t.Fatalf("stderr %q", rig.err.String())
			}
			if f.reqs != nil {
				t.Fatal("the flag was refused but the download was queued")
			}
		})
	}
}

func TestRunnerLinesDoNotShareFlagState(t *testing.T) {
	var queries []app.SearchQuery
	f := &fake{search: func(q app.SearchQuery) (app.SearchPage, error) {
		queries = append(queries, q)
		return page("Zelda"), nil
	}}
	rig := newShellRig(t, f, false)
	ctx := context.Background()
	rig.r.Run(ctx, []string{"search", "a", "-p", "snes", "--sort", "title", "--json"})
	rig.out.Reset()
	rig.r.Run(ctx, []string{"search", "b"})
	if len(queries) != 2 || queries[1].Platforms != nil || queries[1].Sort != "relevance" {
		t.Fatalf("second query leaked the first one's flags: %+v", queries[1])
	}
	if strings.Contains(rig.out.String(), `"schema_version"`) {
		t.Fatal("--json leaked into the next line")
	}
}

func TestRunnerCommandsForHelpAndCompletion(t *testing.T) {
	rig := newShellRig(t, manyPages(1, 1), false)
	cmds := rig.r.Commands()
	byName := map[string]cli.CommandInfo{}
	for _, c := range cmds {
		byName[c.Name] = c
	}
	for _, gone := range []string{"shell", "tui", "gui"} {
		if _, ok := byName[gone]; ok {
			t.Errorf("%s must not be a Shell command", gone)
		}
	}
	for _, want := range []string{"search", "info", "download", "boxart", "config", "cache", "history", "version"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing command %s", want)
		}
	}
	has := func(c cli.CommandInfo, name string) bool {
		for _, f := range c.Flags {
			if f.Name == name {
				return true
			}
		}
		return false
	}
	if !has(byName["search"], "platform") || !has(byName["search"], "json") {
		t.Errorf("search flags (own and global): %+v", byName["search"].Flags)
	}
	for _, refused := range []string{"lang", "config", "allow-executable", "jobs"} {
		if has(byName["download"], refused) || has(byName["search"], refused) {
			t.Errorf("--%s is start-up only and must not be offered", refused)
		}
	}
	if len(byName["config"].Subs) == 0 || len(byName["cache"].Subs) == 0 {
		t.Error("config and cache must list their subcommands")
	}
}

// twoLinks is an entry with two downloadable links and one that is not.
func twoLinks() *fake {
	f := &fake{wait: noop}
	f.entry = func(app.Ref) (app.Result[mothdex.Entry], error) {
		return app.Result[mothdex.Entry]{Value: mothdex.Entry{Slug: "zelda", Title: "Zelda DX"}}, nil
	}
	f.links = func(_ app.Ref, _ app.LinkSelector) ([]app.LinkChoice, error) {
		return []app.LinkChoice{
			{Index: 2, Available: true, Link: mothdex.Link{Type: "Game", Format: "gbc", SizeStr: "1.0 MB", Size: 1 << 20, Extras: []string{"En"}, SourceName: "Myrient"}},
			{Index: 1, Available: true, Link: mothdex.Link{Type: "Demo", Format: "gbc", SizeStr: "0.2 MB", Size: 200 << 10, SourceName: "Archive\x1b[31m"}},
			{Index: 3, Available: false, Reasons: []string{"host not allowed: evil.example"}, Link: mothdex.Link{Type: "Game"}},
		}, nil
	}
	return f
}

func TestLinkChooser(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		answers []string
		wantIdx int  // Link.Index of the request
		queued  bool // whether a download was queued
		code    int
	}{
		{"Enter takes the top-ranked link", []string{"download", "1"}, []string{""}, 2, true, 0},
		{"a number picks that link", []string{"download", "1"}, []string{"1"}, 1, true, 0},
		{"a wrong answer is asked again", []string{"download", "1"}, []string{"9", "x", "1"}, 1, true, 0},
		{"a link that cannot be downloaded is not a choice", []string{"download", "1"}, []string{"3", "2"}, 2, true, 0},
		{"q cancels", []string{"download", "1"}, []string{"q"}, 0, false, 130},
		{"Ctrl+C cancels", []string{"download", "1"}, []string{"<abort>"}, 0, false, 130},
		{"--link skips the chooser", []string{"download", "1", "--link", "1"}, nil, 1, true, 0},
		{"--prefer skips the chooser", []string{"download", "1", "--prefer", "lang=En"}, nil, 0, true, 0},
		{"several entries never ask", []string{"download", "1", "2"}, nil, 0, true, 0},
		{"-q never asks", []string{"download", "1", "-q"}, nil, 0, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := twoLinks()
			rig := newShellRig(t, f, true, tc.answers...)
			code := rig.r.Run(context.Background(), tc.args)
			if code != tc.code {
				t.Fatalf("exit %d, want %d; stderr %q", code, tc.code, rig.err.String())
			}
			if tc.queued != (len(f.reqs) > 0) {
				t.Fatalf("queued %v, requests %+v", tc.queued, f.reqs)
			}
			if tc.queued && f.reqs[0].Link.Index != tc.wantIdx {
				t.Fatalf("request selector %+v, want index %d", f.reqs[0].Link, tc.wantIdx)
			}
			if tc.answers == nil && len(rig.prompts) != 0 {
				t.Fatalf("asked %q though it must not", rig.prompts)
			}
		})
	}
}

func TestLinkChooserScreen(t *testing.T) {
	f := twoLinks()
	rig := newShellRig(t, f, true, "")
	rig.r.Run(context.Background(), []string{"download", "1"})
	shown := rig.err.String()
	for _, want := range []string{"Zelda DX · 3 links", "2*", "<- default", "Myrient", "host not allowed: evil.example"} {
		if !strings.Contains(shown, want) {
			t.Errorf("chooser lacks %q:\n%s", want, shown)
		}
	}
	if len(rig.prompts) != 1 || rig.prompts[0] != "Link [2]: " {
		t.Errorf("prompts %q", rig.prompts)
	}
	if strings.Contains(shown, "\x1b") { // case 49: server text never reaches the terminal raw
		t.Errorf("escape in the chooser: %q", shown)
	}
}

func TestChooserNeverAppearsOutsideAnInteractiveShell(t *testing.T) {
	f := twoLinks()
	rig := newShellRig(t, f, false) // scripted input
	if code := rig.r.Run(context.Background(), []string{"download", "1"}); code != 0 {
		t.Fatalf("exit %d: %s", code, rig.err.String())
	}
	if len(rig.prompts) != 0 || len(f.reqs) != 1 || f.reqs[0].Link.Index != 0 {
		t.Fatalf("prompts %q, requests %+v: the default must be taken in silence", rig.prompts, f.reqs)
	}
}

func TestConfirmationInsideTheShellUsesTheEditor(t *testing.T) {
	f := &fake{wait: noop}
	for _, tc := range []struct {
		name        string
		interactive bool
		answer      string
		want        int
	}{
		{"yes", true, "y", 0},
		{"no", true, "n", 130},
		{"scripted input fails instead of asking", false, "y", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newShellRig(t, f, tc.interactive, tc.answer)
			rig.r = cli.NewRunner(cli.Deps{
				Translator: func(string) *i18n.Translator { return nil }, Stdin: strings.NewReader(""), Stdout: &rig.out, Stderr: &rig.err,
				IsTerminal: func(any) bool { return false }, Getenv: func(string) string { return "" },
			}, confirmSession(t, f), tc.interactive)
			var asked []string
			rig.r.SetPrompt(func(label string) (string, error) { asked = append(asked, label); return tc.answer, nil })
			if got := rig.r.Run(context.Background(), []string{"download", "1", "2"}); got != tc.want {
				t.Fatalf("exit %d, want %d; stderr %q", got, tc.want, rig.err.String())
			}
			if tc.interactive && (len(asked) != 1 || !strings.Contains(asked[0], "Download 2 files")) {
				t.Fatalf("asked %q", asked)
			}
			if !tc.interactive && len(asked) != 0 {
				t.Fatalf("asked %q", asked)
			}
		})
	}
}

func confirmSession(t *testing.T, f *fake) *cli.Session {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	return &cli.Session{Svc: f, T: bundle.New("en_us"), PageSize: 10, Color: "never", Confirm: true}
}

// With stdin on a terminal and stdout redirected, the Runner must not be interactive: no prompt
// could be seen, so a confirmation fails with confirmation_required instead of waiting (PR-53 m2).
func TestShellOnATerminalStdinWithRedirectedStdoutNeverPrompts(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	f := &fake{wait: noop}
	stdin := strings.NewReader("")
	var out, errOut bytes.Buffer
	var ran int
	d := cli.Deps{
		Open: func(context.Context, cli.Globals) (*cli.Session, error) {
			return &cli.Session{Svc: f, T: tr, PageSize: 10, Color: "never", Confirm: true}, nil
		},
		Translator: func(string) *i18n.Translator { return tr },
		Version:    func() buildinfo.Info { return buildinfo.Info{Version: "1"} },
		Stdin:      stdin, Stdout: &out, Stderr: &errOut,
		IsTerminal: func(v any) bool { return v == stdin }, // stdin only
		Width:      func() int { return 80 }, Getenv: func(string) string { return "" },
		Shell: func(ctx context.Context, env cli.ShellEnv) int {
			if env.IsTerminal {
				t.Error("IsTerminal must need stdout too")
			}
			env.Runner.SetPrompt(func(string) (string, error) { t.Error("prompted with stdout redirected"); return "y", nil })
			ran = env.Runner.Run(ctx, []string{"download", "1", "2"})
			return 0
		},
	}
	if code := cli.Execute(context.Background(), d, []string{"shell"}); code != 0 {
		t.Fatalf("shell exit %d: %s", code, errOut.String())
	}
	if ran != 2 || !strings.Contains(errOut.String(), "confirmation_required") {
		t.Fatalf("download exit %d, stderr %q: want confirmation_required (2)", ran, errOut.String())
	}
}

func TestTUICommandNeedsATerminalAndRefusesJSON(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	tests := []struct {
		name     string
		args     []string
		terminal bool
		wantExit int
		wantErr  string
		launched bool
	}{
		{"a terminal starts it", []string{"tui"}, true, 0, "", true},
		{"its exit code is the program's", []string{"tui", "--no-color"}, true, 0, "", true},
		{"no terminal is a usage error", []string{"tui"}, false, 2, "The TUI needs an interactive terminal.", false},
		{"--json is a usage error", []string{"tui", "--json"}, true, 2, "--json cannot be used with the TUI", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			launched := false
			d := cli.Deps{
				Open: func(_ context.Context, g cli.Globals) (*cli.Session, error) {
					if !g.Shell || !g.Downloads {
						t.Errorf("the TUI opens the service as an interactive UI with downloads: %+v", g)
					}
					return &cli.Session{Svc: &fake{}, T: tr, PageSize: 10, Color: "never"}, nil
				},
				Translator: func(string) *i18n.Translator { return tr },
				Version:    func() buildinfo.Info { return buildinfo.Info{Version: "1"} },
				Stdin:      strings.NewReader(""), Stdout: &out, Stderr: &errOut,
				IsTerminal: func(any) bool { return tc.terminal }, Width: func() int { return 80 }, Getenv: func(string) string { return "" },
				TUI: func(context.Context, cli.TUIEnv) int { launched = true; return 0 },
			}
			if code := cli.Execute(context.Background(), d, tc.args); code != tc.wantExit {
				t.Fatalf("exit %d, want %d: %s", code, tc.wantExit, errOut.String())
			}
			if launched != tc.launched || !strings.Contains(errOut.String(), tc.wantErr) {
				t.Fatalf("launched %v, stderr %q (want %q)", launched, errOut.String(), tc.wantErr)
			}
		})
	}
}

func TestTUICommandReturnsTheLauncherExitCode(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	tr := bundle.New("en_us")
	var errOut bytes.Buffer
	d := cli.Deps{
		Open: func(context.Context, cli.Globals) (*cli.Session, error) {
			return &cli.Session{Svc: &fake{}, T: tr, PageSize: 10, Color: "never"}, nil
		},
		Translator: func(string) *i18n.Translator { return tr },
		Stdin:      strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &errOut,
		IsTerminal: func(any) bool { return true }, Getenv: func(string) string { return "" },
		TUI: func(context.Context, cli.TUIEnv) int { return 130 },
	}
	if code := cli.Execute(context.Background(), d, []string{"tui"}); code != 130 || errOut.Len() != 0 {
		t.Fatalf("exit %d, stderr %q: the launcher's code is final and silent", code, errOut.String())
	}
}

// A batch in the Shell says which link each entry with several links takes, with no preference set;
// an entry with one link has nothing to say, and the plain CLI keeps its silence (T-44).
func TestShellBatchNamesTheLinkOfEveryEntryWithSeveralLinks(t *testing.T) {
	links := func(r app.Ref, _ app.LinkSelector) ([]app.LinkChoice, error) {
		if r.Index == 2 { // one link: nothing to choose from
			return []app.LinkChoice{{Index: 1, Available: true, Link: mothdex.Link{Size: 10}}}, nil
		}
		return []app.LinkChoice{
			{Index: 3, Available: true, Link: mothdex.Link{Size: 10}},
			{Index: 1, Available: true, Link: mothdex.Link{Size: 10}},
		}, nil
	}
	f := &fake{wait: noop, links: links}
	rig := newShellRig(t, f, false)
	if code := rig.r.Run(context.Background(), []string{"download", "1", "2", "3"}); code != 0 {
		t.Fatalf("exit %d: %s", code, rig.err.String())
	}
	if n := strings.Count(rig.err.String(), "Link 3 chosen"); n != 2 { // entries 1 and 3 have two links
		t.Fatalf("Link n chosen printed %d times, want 2:\n%s", n, rig.err.String())
	}
	if strings.Contains(rig.err.String(), "Link 1 chosen") {
		t.Fatalf("an entry with one link prints nothing:\n%s", rig.err.String())
	}

	single := newShellRig(t, &fake{wait: noop, links: links}, false)
	single.r.Run(context.Background(), []string{"download", "1"})
	if strings.Contains(single.err.String(), "chosen") {
		t.Fatalf("one entry has the chooser, not this line:\n%s", single.err.String())
	}

	plain := exec(t, &fake{wait: noop, links: links}, nil, "download", "1", "3")
	if strings.Contains(plain.err.String(), "chosen") {
		t.Fatalf("the CLI stays silent without a preference:\n%s", plain.err.String())
	}
}

// A download that stopped on its real size is not a plain failure (T-56): inside an interactive Shell the
// question comes after the batch, names both sizes and the folder, and a yes queues it again with the
// consent for exactly that size; a no leaves the result as the batch had it, with no second error block.
func TestShellAsksDownloadAnywayAfterTheBatch(t *testing.T) {
	const real, catalog = int64(700 << 20), int64(40 << 20)
	stopped := func(id app.JobID) app.JobSnapshot {
		return app.JobSnapshot{ID: id, Slug: "zelda", Title: "Zelda", State: app.StateFailed, Expected: real, CatalogSize: catalog,
			Err: &app.Error{Code: "confirmation_required", Message: "needs confirmation", Args: map[string]any{"size": real, "path": "/roms/gbc"}}}
	}
	// yes
	var queued [][]app.DownloadRequest
	calls := 0
	f := &fake{
		enqueue: func(r []app.DownloadRequest) ([]app.JobID, error) {
			queued = append(queued, r)
			return []app.JobID{"a"}, nil
		},
		wait: func() (app.BatchResult, error) {
			calls++
			if calls == 1 {
				return app.BatchResult{Jobs: []app.JobSnapshot{stopped("a")}, Failed: 1}, nil
			}
			return app.BatchResult{Jobs: []app.JobSnapshot{{ID: "a", Slug: "zelda", Title: "Zelda", State: app.StateDone, Path: "/roms/gbc/z.gb", Bytes: real}}, Done: 1}, nil
		},
	}
	rig := newShellRig(t, f, true, "y")
	if code := rig.r.Run(context.Background(), []string{"download", "1"}); code != 0 {
		t.Fatalf("exit %d: %s", code, rig.err.String())
	}
	wantAsk := "Zelda is 700.0 MB, not 40.0 MB. Download it anyway to /roms/gbc? [y/N] "
	if len(rig.prompts) != 1 || rig.prompts[0] != wantAsk {
		t.Fatalf("prompts %q, want %q", rig.prompts, wantAsk)
	}
	if len(queued) != 2 || queued[0][0].SizeConsent != 0 || queued[1][0].SizeConsent != real {
		t.Fatalf("the second batch carries the consent for %d: %+v", real, queued)
	}
	if strings.Contains(rig.err.String(), "confirmation_required") {
		t.Fatalf("a yes leaves no error behind:\n%s", rig.err.String())
	}

	// no
	queued, calls = nil, 0
	rig = newShellRig(t, f, true, "n")
	code := rig.r.Run(context.Background(), []string{"download", "1"})
	if code == 0 || len(queued) != 1 || strings.Contains(rig.err.String(), "Error:") {
		t.Fatalf("a no queues nothing and prints no second error block: exit %d, queued %d\n%s", code, len(queued), rig.err.String())
	}

	// no terminal: the row and the way out are printed, nothing is asked
	queued, calls = nil, 0
	rig = newShellRig(t, f, false, "y")
	_ = rig.r.Run(context.Background(), []string{"download", "1"})
	if len(rig.prompts) != 0 || len(queued) != 1 || !strings.Contains(rig.err.String(), "-f") {
		t.Fatalf("without a terminal nothing is asked and -f is the way out: %q\n%s", rig.prompts, rig.err.String())
	}
}

// In a batch the stopped job is a warning line with its code and the way out, not a failure.
func TestBatchLineOfAJobThatWaitsForItsOK(t *testing.T) {
	f := &fake{wait: func() (app.BatchResult, error) {
		return app.BatchResult{Failed: 1, Done: 1, Jobs: []app.JobSnapshot{
			{ID: "a", Slug: "mario", Title: "Mario", State: app.StateDone, Path: "/roms/m.nes", Bytes: 10},
			{ID: "b", Slug: "zelda", Title: "Zelda", State: app.StateFailed, Expected: 700 << 20, CatalogSize: 40 << 20,
				Err: &app.Error{Code: "confirmation_required", Message: "needs confirmation", Args: map[string]any{"size": int64(700 << 20), "path": "/roms/gbc"}}},
		}}, nil
	}}
	rig := newShellRig(t, f, false)
	rig.r.Run(context.Background(), []string{"download", "1", "2"})
	got := rig.err.String()
	for _, want := range []string{"! Zelda: Needs your OK: the file is 700.0 MB, listed as 40.0 MB. [confirmation_required]", "Add -f to download it anyway."} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[x] Zelda") || strings.Contains(got, "✘ Zelda") {
		t.Fatalf("it is not drawn as a failure:\n%s", got)
	}
}
