package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// The test executable doubles as a GUI child on every OS. Arguments are captured before
// testing parses them, so this exercises the real subprocess and forwarding path.
func TestMain(m *testing.M) {
	if target := os.Getenv("MOTHDEX_TEST_GUI_ARGS"); target != "" && strings.HasPrefix(filepath.Base(os.Args[0]), "mothdex-gui") {
		if os.Getenv("MOTHDEX_TEST_GUI_WAIT") == "1" {
			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt)
			_, _ = fmt.Fprintln(os.Stdout, "ready")
			<-signals
			if err := os.WriteFile(target, []byte("interrupted"), 0o600); err != nil { //nolint:gosec // Test-only child output path.
				os.Exit(1)
			}
			os.Exit(0)
		}
		data, _ := json.Marshal(os.Args[1:])
		if err := os.WriteFile(target, data, 0o600); err != nil { //nolint:gosec // Test-only child output path set by guiFixture.
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func guiFixture(t *testing.T, d *Deps) string {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe) //nolint:gosec // The running test executable.
	if err != nil {
		t.Fatal(err)
	}
	name := "mothdex-gui"
	if strings.HasSuffix(exe, ".exe") {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil { //nolint:gosec // Test child must be executable.
		t.Fatal(err)
	}
	captured := filepath.Join(dir, "args.json")
	t.Setenv("MOTHDEX_TEST_GUI_ARGS", captured)
	d.Executable = func() (string, error) { return filepath.Join(dir, "mothdex"), nil }
	return captured
}

func helpDeps(t *testing.T) (Deps, *bytes.Buffer) {
	t.Helper()
	b, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	out := new(bytes.Buffer)
	return Deps{Stdin: strings.NewReader(""), Stdout: out, Stderr: out, Translator: func(lang string) *i18n.Translator { return b.New(lang) }}, out
}

func TestHelpAliasGroupsAndTopics(t *testing.T) {
	d, out := helpDeps(t)
	d.ProgramName = filepath.Join("bin", "mdex.exe")
	d.HelpTopics = []HelpTopic{{Name: "logs", Text: func() string { return "session logs reference" }}}
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"--help"}, []string{"Usage:\n  mdex", "Catalog:", "Downloads:", "Interfaces:", "System:", "Global Flags:", "Modes:"}},
		{[]string{"download", "--help"}, []string{"mdex download", "mothdex download 1", "Flags:", "Global Flags:"}},
		{[]string{"help", "config"}, []string{"interface.page_size", "MOTHDEX_", "Default"}},
		{[]string{"help", "exit-codes"}, []string{"130", "partial", "canceled"}},
		{[]string{"help", "errors"}, []string{"network", "Check your connection"}},
		{[]string{"help", "logs"}, []string{"session logs reference"}},
	} {
		out.Reset()
		if code := Execute(context.Background(), d, tc.args); code != 0 {
			t.Fatalf("%v: %d %s", tc.args, code, out)
		}
		for _, want := range tc.want {
			if !strings.Contains(strings.ToLower(out.String()), strings.ToLower(want)) {
				t.Errorf("%v missing %q: %s", tc.args, want, out)
			}
		}
	}
	out.Reset()
	if code := Execute(context.Background(), d, []string{"help", "unknown-topic"}); code != 2 {
		t.Fatalf("unknown topic: %d %s", code, out)
	}
}

func TestGUIForwardingAndMissingSibling(t *testing.T) {
	d, out := helpDeps(t)
	file := guiFixture(t, &d)
	args := []string{"gui", "--config", "with spaces.yaml", "--profile", "retrobat", "--lang", "pt_br"}
	if code := Execute(context.Background(), d, args); code != 0 {
		t.Fatalf("launch %d %s", code, out)
	}
	data, err := os.ReadFile(file) //nolint:gosec // Captured child arguments in the test temp directory.
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "--config|with spaces.yaml|--profile|retrobat|--lang|pt_br" {
		t.Fatalf("forwarded %v", got)
	}
	missing := filepath.Join(t.TempDir(), "mothdex")
	d.Executable = func() (string, error) { return missing, nil }
	out.Reset()
	if code := Execute(context.Background(), d, []string{"gui"}); code != 2 || !strings.Contains(out.String(), "mothdex tui") || !strings.Contains(out.String(), "-NoGui") {
		t.Fatalf("missing GUI: %d %s", code, out)
	}
}

func TestCompletionRegistersAlias(t *testing.T) {
	for shell, want := range map[string]string{"bash": "-F __start_mothdex mdex", "zsh": "compdef _mothdex mdex", "fish": "complete -c mdex -w mothdex", "powershell": "Register-ArgumentCompleter -CommandName mothdex,mdex"} {
		d, out := helpDeps(t)
		if code := Execute(context.Background(), d, []string{"completion", shell}); code != 0 || !strings.Contains(out.String(), want) {
			t.Errorf("%s: %d missing %s", shell, code, want)
		}
	}
}

func TestErrorReferenceCoversContractCodes(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/contracts/cli/cli-output.md")
	if err != nil {
		t.Fatal(err)
	}
	section := strings.Split(strings.Split(string(raw), "## 4.")[1], "## 5.")[0]
	doc, err := os.ReadFile("../../../docs/errors.md")
	if err != nil {
		t.Fatal(err)
	}
	b, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}

	tableStarted := false
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "|") {
			if tableStarted {
				break
			}
			continue
		}
		tableStarted = true
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			continue
		}
		for _, code := range regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(cells[3], -1) {
			for _, lang := range []string{"en_us", "pt_br"} {
				pattern := `(?m)^` + regexp.QuoteMeta(strings.TrimSpace(cells[1])) + `\s+.*\b` + regexp.QuoteMeta(code[1]) + `\b`
				if !regexp.MustCompile(pattern).MatchString(b.New(lang).T("ui.cli.template.exit_codes", nil)) {
					t.Errorf("%s exit table misses %s at exit %s", lang, code[1], strings.TrimSpace(cells[1]))
				}
			}
			if !strings.Contains(string(doc), "`"+code[1]+"`") || !b.New("en_us").Has("errors."+code[1]+".message") || !strings.Contains(b.New("en_us").T("ui.cli.template.exit_codes", nil), code[1]) {
				t.Errorf("missing documented error %s", code[1])
			}
		}
	}

}

type exampleClock struct{}

func (exampleClock) Now() time.Time { return time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC) }

// Catalog operations use the real HTTP client and app service. Transfers are a fake
// adapter to the same server, keeping downloader trust policy out of help acceptance.
type exampleService struct {
	*app.Service
	client   *http.Client
	url      string
	requests []app.DownloadRequest
}

func (s *exampleService) Enqueue(ctx context.Context, reqs []app.DownloadRequest) ([]app.JobID, error) {
	s.requests = reqs
	for _, req := range reqs {
		if _, err := s.Entry(ctx, req.Ref); err != nil {
			return nil, err
		}
	}
	return []app.JobID{"example"}, nil
}
func (s *exampleService) Events(context.Context) <-chan app.Event {
	ch := make(chan app.Event)
	close(ch)
	return ch
}
func (s *exampleService) Wait(ctx context.Context) (app.BatchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url+"/file", nil)
	if err != nil {
		return app.BatchResult{}, err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return app.BatchResult{}, err
	}
	defer func() { _ = res.Body.Close() }()
	_, err = io.Copy(io.Discard, res.Body)
	return app.BatchResult{Done: 1, Jobs: []app.JobSnapshot{{ID: "example", State: app.StateDone}}}, err
}
func (s *exampleService) SaveBoxart(ctx context.Context, _ app.Ref, _ string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url+"/cover", nil)
	if err != nil {
		return "", err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = res.Body.Close() }()
	_, err = io.Copy(io.Discard, res.Body)
	return "cover.png", err
}
func (s *exampleService) SaveJobBoxart(ctx context.Context, _ app.JobSnapshot, dir string) (string, error) {
	return s.SaveBoxart(ctx, app.Ref{}, dir)
}

func TestEveryExampleAgainstFakeServer(t *testing.T) {
	entry := mothdex.Entry{Slug: "super-mario-world-snes", Title: "Super Mario World", Platform: "snes", Links: []mothdex.Link{{URL: "https://proxy.mothdex.net/file/mario", Filename: "mario.zip", Size: 10}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var data any
		switch req.URL.Path {
		case "/search":
			data = map[string]any{"results": []mothdex.Entry{entry}, "current_page": 1, "total_pages": 1, "total_results": 1, "current_results": 1}
		case "/entry", "/entry/random":
			data = map[string]any{"entry": entry}
		case "/platforms":
			data = map[string]any{"platforms": map[string]any{"snes": map[string]string{"name": "Super Nintendo", "brand": "Nintendo", "class": "Home consoles"}}}
		case "/regions":
			data = map[string]any{"regions": map[string]string{"us": "USA"}}
		case "/info":
			data = mothdex.DBInfo{TotalEntries: 1}
		case "/file", "/cover":
			_, _ = w.Write([]byte("fixture"))
			return
		default:
			t.Errorf("unexpected endpoint %s", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"info": map[string]any{}, "data": data})
	}))
	defer server.Close()
	client, err := mothdex.New(mothdex.WithBaseURL(server.URL), mothdex.WithHTTPClient(server.Client()), mothdex.WithRateLimit(10, 1000))
	if err != nil {
		t.Fatal(err)
	}
	d, out := helpDeps(t)
	guiFixture(t, &d)
	d.IsTerminal = func(any) bool { return true }
	d.Shell = func(context.Context, ShellEnv) int { return 0 }
	d.TUI = func(context.Context, TUIEnv) int { return 0 }
	root := (&runtime{d: d}).rootCommand()
	localizeHelp(root, d.Translator("en_us"))
	walkCommands(root, func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		if c.Long == "" || c.Example == "" {
			t.Errorf("%s lacks long description/example", c.CommandPath())
			return
		}
		t.Run(helpID(c), func(t *testing.T) {
			if c.Name() == "upgrade" {
				testUpgradeExamples(t, c.Example)
				return
			}
			store, err := cache.Open(t.TempDir(), cache.Config{}, exampleClock{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			hist, err := history.Open(filepath.Join(t.TempDir(), "history.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			svc, err := app.New(app.Options{Client: client, Cache: store, Clock: exampleClock{}, AllowedHosts: []string{"proxy.mothdex.net"}, History: hist, Config: app.ConfigSource{File: filepath.Join(t.TempDir(), "config.yaml")}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = svc.Close() }()
			fake := &exampleService{Service: svc, client: server.Client(), url: server.URL}
			d.Open = func(context.Context, Globals) (*Session, error) {
				return &Session{Svc: fake, T: d.Translator("en_us"), PageSize: 10}, nil
			}
			for _, line := range strings.Split(c.Example, "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 || fields[0] != "mothdex" {
					t.Fatalf("unsupported example %q", line)
				}
				out.Reset()
				if code := Execute(context.Background(), d, fields[1:]); code != 0 {
					t.Fatalf("%s: exit %d\n%s", line, code, out)
				}
			}
		})
	})
}

// Cancel after the child has installed its handler; no clock or polling is involved.
type cancelOnReady struct{ cancel context.CancelFunc }

func (w cancelOnReady) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "ready") {
		w.cancel()
	}
	return len(p), nil
}
func TestGUIInterruptIsGraceful(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows console Ctrl+C is broadcast by the console host")
	}
	d, _ := helpDeps(t)
	file := guiFixture(t, &d)
	t.Setenv("MOTHDEX_TEST_GUI_WAIT", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Stdout = cancelOnReady{cancel: cancel}
	if code := Execute(ctx, d, []string{"gui"}); code != 130 {
		t.Fatalf("canceled GUI exit %d, want 130", code)
	}
	data, err := os.ReadFile(file) //nolint:gosec // Child fixture output.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "interrupted" {
		t.Fatalf("child did not clean up: %s", data)
	}
}

func TestHelpTopicCompletion(t *testing.T) {
	d, out := helpDeps(t)
	if code := Execute(context.Background(), d, []string{"__complete", "help", "co"}); code != 0 || !strings.Contains(out.String(), "config") || !strings.Contains(out.String(), "completion") {
		t.Fatalf("help completion: %d %s", code, out)
	}
}
func TestCompletionIncludesFallbackAndInvokedAlias(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		d, out := helpDeps(t)
		d.ProgramName = "mydex"
		if code := Execute(context.Background(), d, []string{"completion", shell}); code != 0 {
			t.Fatal(code)
		}
		for _, alias := range []string{"mthd", "mydex"} {
			if !strings.Contains(out.String(), alias) {
				t.Errorf("%s lacks %s", shell, alias)
			}
		}
	}
}
