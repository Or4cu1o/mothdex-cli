package shell

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// fakeRunner records what the Shell asks the CLI to run.
type fakeRunner struct {
	runs     [][]string
	searches []app.SearchQuery
	page     *app.SearchPage
	entry    *mothdex.Entry
	codes    map[string]int // exit code by command name
	run      func(ctx context.Context, args []string) int
	prompt   func(string) (string, error)
}

func (f *fakeRunner) Run(ctx context.Context, args []string) int {
	f.runs = append(f.runs, args)
	if f.run != nil {
		return f.run(ctx, args)
	}
	return f.codes[args[0]]
}

func (f *fakeRunner) RunSearch(_ context.Context, q app.SearchQuery) int {
	f.searches = append(f.searches, q)
	return 0
}

func (f *fakeRunner) LastPage() (app.SearchPage, bool) {
	if f.page == nil {
		return app.SearchPage{}, false
	}
	return *f.page, true
}

func (f *fakeRunner) LastEntry() (mothdex.Entry, bool) {
	if f.entry == nil {
		return mothdex.Entry{}, false
	}
	return *f.entry, true
}

func (f *fakeRunner) Commands() []cli.CommandInfo {
	c := testCommands()
	return append(c, cli.CommandInfo{Name: "version", Short: "Show the version"})
}

func (f *fakeRunner) SetPrompt(fn func(string) (string, error)) { f.prompt = fn }

// step is one answer of the fake editor: a line, or an error; advance moves the fake clock first.
type step struct {
	line    string
	err     error
	advance time.Duration
}

type fakeEditor struct {
	script  []step
	labels  []string
	history []string
	clock   *time.Time
	block   chan struct{} // when set, Prompt waits on it once the script is empty
}

func (e *fakeEditor) Prompt(label string) (string, error) {
	e.labels = append(e.labels, label)
	if len(e.script) == 0 {
		if e.block != nil {
			<-e.block
		}
		return "", io.EOF
	}
	s := e.script[0]
	e.script = e.script[1:]
	if e.clock != nil {
		*e.clock = e.clock.Add(s.advance)
	}
	return s.line, s.err
}

func (e *fakeEditor) AppendHistory(l string)                                    { e.history = append(e.history, l) }
func (e *fakeEditor) SetCompleter(func(string, int) (string, []string, string)) {}
func (e *fakeEditor) Close() error                                              { return nil }

type fakeService struct {
	total   int64
	infoErr error
}

func (f fakeService) DBInfo(context.Context) (app.Result[mothdex.DBInfo], error) {
	return app.Result[mothdex.DBInfo]{Value: mothdex.DBInfo{TotalEntries: f.total}}, f.infoErr
}
func (fakeService) Platforms(context.Context, app.PlatformFilter) (app.Result[[]mothdex.Platform], error) {
	return app.Result[[]mothdex.Platform]{}, nil
}
func (fakeService) Regions(context.Context) (app.Result[[]mothdex.Region], error) {
	return app.Result[[]mothdex.Region]{}, nil
}
func (fakeService) ConfigList() ([]app.Setting, error)  { return nil, nil }
func (fakeService) CacheStats() (app.CacheStats, error) { return app.CacheStats{}, nil }
func (fakeService) ActiveProfile() app.ProfileInfo      { return app.ProfileInfo{ID: "generic"} }

type rig struct {
	sh     *Shell
	run    *fakeRunner
	ed     *fakeEditor
	out    bytes.Buffer
	errs   bytes.Buffer
	now    time.Time
	signal chan os.Signal
}

func newRig(t *testing.T, interactive bool, script ...step) *rig {
	t.Helper()
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{run: &fakeRunner{codes: map[string]int{}}, now: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), signal: make(chan os.Signal, 1)}
	r.ed = &fakeEditor{script: script, clock: &r.now}
	r.sh = New(Options{
		Runner: r.run, Service: fakeService{total: 1284019}, Editor: r.ed, Out: &r.out, Err: &r.errs,
		T: bundle.New("en_us"), Now: func() time.Time { return r.now }, Interactive: interactive, Version: "0.2.0",
		Interrupts: func() (<-chan os.Signal, func()) { return r.signal, func() {} },
	})
	return r
}

func lines(ls ...string) []step {
	out := make([]step, len(ls))
	for i, l := range ls {
		out[i] = step{line: l}
	}
	return out
}

func TestLinesAreRoutedToTheCLI(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"search zelda -p snes", []string{"search", "zelda", "-p", "snes"}},
		{`search "super mario"`, []string{"search", "super mario"}},
		{"3", []string{"info", "3"}},
		{"d 2-4", []string{"download", "2", "3", "4"}},
		{"download 1 3 5 --prefer lang=En", []string{"download", "1", "3", "5", "--prefer", "lang=En"}},
		{"download 2-3 -o 2020-21", []string{"download", "2", "3", "-o", "2020-21"}},
		{"version", []string{"version"}},
		{"config set interface.color never", []string{"config", "set", "interface.color", "never"}},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			r := newRig(t, false, lines(tc.line)...)
			r.sh.Loop(context.Background())
			if len(r.run.runs) != 1 || !reflect.DeepEqual(r.run.runs[0], tc.want) {
				t.Fatalf("runs %q, want %q", r.run.runs, tc.want)
			}
		})
	}
}

// info and boxart take one entry, so a list or a range runs them once for each entry, with the same flags
// (T-43 S1: `info 2-3` used to end with "accepts 1 arg(s), received 2"). download keeps taking the whole list.
func TestInfoAndBoxartRunOncePerEntry(t *testing.T) {
	tests := []struct {
		line string
		want [][]string
	}{
		{"info 2-3", [][]string{{"info", "2"}, {"info", "3"}}},
		{"info 1 3", [][]string{{"info", "1"}, {"info", "3"}}},
		{"boxart 1-2", [][]string{{"boxart", "1"}, {"boxart", "2"}}},
		{"boxart 4 zelda-slug", [][]string{{"boxart", "4"}, {"boxart", "zelda-slug"}}},
		{"info 5", [][]string{{"info", "5"}}},
		{"info 1-2 --prefer lang=En", [][]string{{"info", "--prefer", "lang=En", "1"}, {"info", "--prefer", "lang=En", "2"}}},
		{"boxart 1-2 --json", [][]string{{"boxart", "--json", "1"}, {"boxart", "--json", "2"}}},
		{"download 2-3", [][]string{{"download", "2", "3"}}},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			r := newRig(t, false, lines(tc.line)...)
			r.sh.Loop(context.Background())
			if !reflect.DeepEqual(r.run.runs, tc.want) {
				t.Fatalf("runs %q, want %q", r.run.runs, tc.want)
			}
		})
	}
}

// The first failure is the exit code, and the entries after it still run.
func TestAFailingEntryDoesNotStopTheRestOfTheList(t *testing.T) {
	r := newRig(t, false, lines("info 1-3")...)
	r.run.run = func(_ context.Context, args []string) int {
		if args[len(args)-1] == "2" {
			return 4
		}
		return 0
	}
	code := r.sh.Loop(context.Background()) // a piped session exits with the code of its last command
	if len(r.run.runs) != 3 {
		t.Fatalf("runs %q: all three entries must run", r.run.runs)
	}
	if code != 4 {
		t.Fatalf("exit code %d, want the first failure, 4", code)
	}
}

func TestMistakesDoNotReachTheCLI(t *testing.T) {
	tests := []struct {
		name, line, said string
	}{
		{"unknown command with a suggestion", "serch zelda", `Unknown command "serch". Did you mean "search"?`},
		{"unknown command, nothing close", "frobnicate", `Unknown command "frobnicate".`},
		{"shell is not a command inside the shell", "shell", `Unknown command "shell".`},
		{"a descending range", "download 4-2", `"4-2" is not a valid range`},
		{"an open quote", `search "mario`, "A quote is not closed."},
		{"hostile text is cleaned", "x\x1b[2Jy", `Unknown command "x[2Jy"`}, // the escape byte is removed
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, false, lines(tc.line)...)
			code := r.sh.Loop(context.Background())
			if code != exitUsage || len(r.run.runs) != 0 {
				t.Fatalf("code %d runs %q", code, r.run.runs)
			}
			got := strings.ReplaceAll(r.errs.String(), "\x1b", "")
			if !strings.Contains(got, strings.ReplaceAll(tc.said, "x[2Jy", "x[2Jy")) || strings.Contains(r.errs.String(), "\x1b[2J") {
				t.Fatalf("stderr %q does not say %q", r.errs.String(), tc.said)
			}
			if !strings.Contains(r.errs.String(), "Type help.") {
				t.Fatalf("stderr %q lacks the hint", r.errs.String())
			}
		})
	}
}

func TestPagingShortcuts(t *testing.T) {
	q := app.SearchQuery{Text: "mario", Platforms: []string{"nes"}, Page: 2, Sort: app.SortTitle}
	at := func(page, total int) *app.SearchPage {
		qq := q
		qq.Page = page
		return &app.SearchPage{Query: qq, Page: page, TotalPages: total}
	}
	want := func(page int, sort app.SortKey) *app.SearchQuery {
		w := q
		w.Page, w.Sort = page, sort
		return &w
	}
	tests := []struct {
		name  string
		page  *app.SearchPage
		line  string
		query *app.SearchQuery // the search the Shell asks for, nil for none
		said  string
		code  int
	}{
		{"n goes forward", at(2, 4), "n", want(3, app.SortTitle), "", 0},
		{"p goes back", at(2, 4), "p", want(1, app.SortTitle), "", 0},
		{"n on the last page", at(4, 4), "n", nil, "This is the last page.", 0},
		{"p on the first page", at(1, 4), "p", nil, "This is the first page.", 0},
		{"n with no results", at(1, 0), "n", nil, "This is the last page.", 0},
		{"page jumps", at(2, 4), "page 4", want(4, app.SortTitle), "", 0},
		{"page past the end is the service's to clamp", at(2, 4), "page 9", want(9, app.SortTitle), "", 0},
		{"page needs a number", at(2, 4), "page x", nil, "page takes one number", exitUsage},
		{"page zero", at(2, 4), "page 0", nil, "page takes one number", exitUsage},
		{"sort re-sorts the current page", at(2, 4), "sort size", want(2, app.SortSize), "", 0},
		{"sort needs a key", at(2, 4), "sort colour", nil, "sort takes one of", exitUsage},
		{"last prints the page again", at(2, 4), "last", want(2, app.SortTitle), "", 0},
		{"n with no search", nil, "n", nil, "No search yet in this shell.", 3},
		{"last with no search", nil, "last", nil, "No search yet in this shell.", 3},
		{"page with no search", nil, "page 2", nil, "No search yet in this shell.", 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, false, lines(tc.line)...)
			r.run.page = tc.page
			code := r.sh.Loop(context.Background())
			switch {
			case tc.query == nil && len(r.run.searches) != 0:
				t.Fatalf("unexpected searches %v", r.run.searches)
			case tc.query != nil && (len(r.run.searches) != 1 || !reflect.DeepEqual(r.run.searches[0], *tc.query)):
				t.Fatalf("searches %v, want %v", r.run.searches, *tc.query)
			}
			if !strings.Contains(r.errs.String(), tc.said) || code != tc.code {
				t.Fatalf("code %d (want %d), stderr %q (want %q)", code, tc.code, r.errs.String(), tc.said)
			}
		})
	}
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		script      []step
		codes       map[string]int
		want        int
	}{
		{"piped input exits with the last command's code", false, lines("download 1"), map[string]int{"download": 7}, 7},
		{"a later success replaces it", false, lines("download 1", "version"), map[string]int{"download": 7}, 0},
		{"a blank line keeps it", false, lines("download 1", ""), map[string]int{"download": 7}, 7},
		{"exit is 0 even after a failure", false, lines("download 1", "exit"), map[string]int{"download": 7}, 0},
		{"quit too", false, lines("download 1", "quit"), map[string]int{"download": 7}, 0},
		{"Ctrl+D at a terminal is 0", true, lines("download 1"), map[string]int{"download": 7}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.interactive, tc.script...)
			r.run.codes = tc.codes
			if got := r.sh.Loop(context.Background()); got != tc.want {
				t.Fatalf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCtrlCAtThePromptNeverLeaves(t *testing.T) {
	tests := []struct {
		name   string
		script []step
		hints  int
	}{
		{"one only hints", []step{{err: errAborted}}, 1},
		{"two in a row still only hint", []step{{err: errAborted}, {err: errAborted}}, 2},
		{"many, with commands between", []step{{err: errAborted}, {line: "version"}, {err: errAborted}, {err: errAborted}, {err: errAborted}}, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, true, tc.script...)
			if got := r.sh.Loop(context.Background()); got != 0 { // the script ends with Ctrl+D
				t.Fatalf("exit %d: only exit, quit and Ctrl+D leave", got)
			}
			if n := strings.Count(r.errs.String(), "Type exit or press Ctrl+D to leave."); n != tc.hints {
				t.Fatalf("hint shown %d times, want %d", n, tc.hints)
			}
		})
	}
}

func TestCtrlCStopsTheRunningCommandButNotTheShell(t *testing.T) {
	r := newRig(t, true, lines("download 1", "version")...)
	started := make(chan struct{})
	r.run.run = func(ctx context.Context, args []string) int {
		if args[0] != "download" {
			return 0
		}
		close(started)
		<-ctx.Done() // the download ends when Ctrl+C cancels its context
		return exitInterrupted
	}
	go func() {
		<-started
		r.signal <- os.Interrupt
	}()
	if code := r.sh.Loop(context.Background()); code != 0 {
		t.Fatalf("exit %d: the shell must go on to the next line", code)
	}
	if len(r.run.runs) != 2 {
		t.Fatalf("runs %q: the line after the canceled command was not run", r.run.runs)
	}
}

func TestACtrlCThatArrivedWhileIdleDoesNotCancelTheNextCommand(t *testing.T) {
	r := newRig(t, true, lines("version")...)
	r.signal <- os.Interrupt // stale: nobody was running
	var canceled bool
	r.run.run = func(ctx context.Context, _ []string) int {
		canceled = ctx.Err() != nil
		return 0
	}
	r.sh.Loop(context.Background())
	if canceled {
		t.Fatal("a stale interrupt canceled the next command")
	}
}

func TestTerminationEndsTheShellEvenWhileItWaitsForAKey(t *testing.T) {
	r := newRig(t, false)
	r.ed.block = make(chan struct{})
	defer close(r.ed.block)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := r.sh.Loop(ctx); got != exitInterrupted {
		t.Fatalf("exit %d", got)
	}
}

func TestHistoryIsKeptForPeopleAndFilteredFromSecrets(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "data", historyFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("search old\n\nsearch old\nconfig set api.token x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, true, lines("search zelda", " search hidden", "config set api.key y", "search zelda", "version")...)
	r.sh.o.DataDir = filepath.Join(dir, "data")
	r.sh.hist = newHistoryStore(r.sh.o.DataDir, nil)
	r.sh.Loop(context.Background())

	got, err := os.ReadFile(file) //nolint:gosec // a path inside t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := "search old\nsearch zelda\nversion\n"; string(got) != want {
		t.Fatalf("history file %q, want %q", got, want)
	}
	if want := []string{"search old", "search zelda", "version"}; !reflect.DeepEqual(r.ed.history, want) {
		t.Fatalf("editor history %q, want %q", r.ed.history, want)
	}
}

func TestPipedScriptsLeaveNoHistory(t *testing.T) {
	dir := t.TempDir()
	r := newRig(t, false, lines("search zelda")...)
	r.sh.hist = newHistoryStore(dir, nil)
	r.sh.Loop(context.Background())
	if _, err := os.Stat(filepath.Join(dir, historyFile)); !os.IsNotExist(err) {
		t.Fatalf("a piped run wrote a history file (%v)", err)
	}
}

func TestAHistoryThatCannotBeWrittenNeverStopsTheShell(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, true, lines("version")...)
	r.sh.hist = newHistoryStore(filepath.Join(blocker, "sub"), nil) // a folder under a regular file
	if code := r.sh.Loop(context.Background()); code != 0 || len(r.run.runs) != 1 {
		t.Fatalf("exit %d runs %q", code, r.run.runs)
	}
}

func TestBanner(t *testing.T) {
	r := newRig(t, true)
	r.sh.Loop(context.Background())
	if want := `mothdex 0.2.0 · 1,284,019 entries · profile: generic · type "help" or Tab`; !strings.Contains(r.out.String(), want) {
		t.Fatalf("banner %q", r.out.String())
	}
	off := newRig(t, true)
	off.sh.o.Service = fakeService{infoErr: io.ErrUnexpectedEOF}
	off.sh.Loop(context.Background())
	if !strings.Contains(off.out.String(), "· offline ·") || off.errs.Len() != 0 {
		t.Fatalf("offline banner %q, stderr %q", off.out.String(), off.errs.String())
	}
	piped := newRig(t, false)
	piped.sh.Loop(context.Background())
	if piped.out.Len() != 0 {
		t.Fatalf("a piped shell printed %q", piped.out.String())
	}
}

func TestPromptsFollowTheLastSearch(t *testing.T) {
	r := newRig(t, true, lines("version")...)
	r.run.page = &app.SearchPage{Query: app.SearchQuery{Platforms: []string{"snes"}}, Results: make([]app.IndexedEntry, 2)}
	r.sh.Loop(context.Background())
	if len(r.ed.labels) < 1 || r.ed.labels[0] != "mothdex[snes:2]> " {
		t.Fatalf("labels %q", r.ed.labels)
	}
}

func TestPromptsOfCommandsGoThroughTheEditor(t *testing.T) {
	r := newRig(t, true)
	r.ed.script = lines("2")
	r.sh.Loop(context.Background())
	_ = r.run.prompt // set by Loop
	r.ed.script = lines("q")
	got, err := r.run.prompt("Link [1]: ")
	if err != nil || got != "q" || r.ed.labels[len(r.ed.labels)-1] != "Link [1]: " {
		t.Fatalf("got %q, %v, labels %q", got, err, r.ed.labels)
	}
}

func TestHelp(t *testing.T) {
	r := newRig(t, false, lines("help", "help search", "help clear", "help nope")...)
	r.sh.Loop(context.Background())
	for _, want := range []string{"search", "Show the version", "help [command]", "exit", "page <n>", "d <#>"} {
		if !strings.Contains(r.out.String(), want) {
			t.Errorf("help list lacks %q:\n%s", want, r.out.String())
		}
	}
	if len(r.run.runs) != 1 || !reflect.DeepEqual(r.run.runs[0], []string{"search", "--help"}) {
		t.Errorf("runs %q: help search must show the command's own help", r.run.runs)
	}
	if !strings.Contains(r.out.String(), "Clear the screen") || !strings.Contains(r.errs.String(), `Unknown command "nope"`) {
		t.Errorf("out %q err %q", r.out.String(), r.errs.String())
	}
}

func TestClear(t *testing.T) {
	r := newRig(t, true, lines("clear")...)
	r.sh.o.Clear = clearScreen
	r.sh.Loop(context.Background())
	if !strings.Contains(r.out.String(), clearScreen) {
		t.Fatalf("out %q", r.out.String())
	}
	piped := newRig(t, false, lines("clear")...)
	piped.sh.o.Clear = clearScreen
	piped.sh.Loop(context.Background())
	if strings.Contains(piped.out.String(), "\x1b") {
		t.Fatal("clear wrote an escape sequence to a pipe")
	}
}

// ui.md section 1: the new keys exist in en_us only for some locales, and the others must fall back.
func TestShellKeysFallBackToEnglish(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"hi_in", "ja_jp", "zh_cn"} {
		tr := bundle.New(id)
		if got := tr.T("ui.shell.no_page", nil); got != "No search yet in this shell. Run search first." {
			t.Errorf("%s: %q", id, got)
		}
		if got := tr.N("ui.shell.chooser.title", 3, i18n.Args{"title": "Zelda"}); got != "Zelda · 3 links" {
			t.Errorf("%s plural: %q", id, got)
		}
	}
}

func TestAScriptDoesNotTakeSIGINTOverAndStopsWhenItIsCanceled(t *testing.T) {
	r := newRig(t, false, lines("download 1", "version")...)
	taken := 0
	r.sh.o.Interrupts = func() (<-chan os.Signal, func()) { taken++; return r.signal, func() {} }
	ctx, cancel := context.WithCancel(context.Background())
	r.run.run = func(context.Context, []string) int {
		cancel() // the first SIGINT cancels the root context (the default when nobody takes it over)
		return exitInterrupted
	}
	if code := r.sh.Loop(ctx); code != exitInterrupted {
		t.Fatalf("exit %d, want 130", code)
	}
	if taken != 0 {
		t.Fatal("a piped script must keep the default SIGINT handling")
	}
	if len(r.run.runs) != 1 {
		t.Fatalf("runs %q: the line after the canceled command must not run", r.run.runs)
	}
}

func TestAPersonAtATerminalDoesTakeSIGINTOver(t *testing.T) {
	r := newRig(t, true, lines("version")...)
	taken := 0
	r.sh.o.Interrupts = func() (<-chan os.Signal, func()) { taken++; return r.signal, func() {} }
	r.sh.Loop(context.Background())
	if taken != 1 {
		t.Fatalf("Interrupts taken %d times, want 1", taken)
	}
}
