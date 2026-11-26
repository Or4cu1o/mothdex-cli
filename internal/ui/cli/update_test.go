package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

type countingTransport struct{ calls atomic.Int64 }

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	panic("automatic check should be disabled")
}
func TestAutomaticUpdateOutputOptOut(t *testing.T) {
	for _, tc := range []struct {
		name             string
		json, quiet, tty bool
		cmd              string
	}{{"json", true, false, true, "search"}, {"quiet", false, true, true, "search"}, {"pipe", false, false, false, "search"}, {"version", false, false, true, "version"}, {"upgrade", false, false, true, "upgrade"}, {"completion", false, false, true, "__complete"}, {"logs", false, false, true, "logs"}} {
		t.Run(tc.name, func(t *testing.T) {
			exe := filepath.Join(t.TempDir(), "mothdex")
			if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			transport := new(countingTransport)
			u, err := update.New(update.Options{Exe: exe, Current: "1.0.0", Client: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			s := &Session{Updater: u, UpdateCheck: true}
			r := &runtime{ctx: context.Background(), sess: s, cmd: tc.cmd, g: Globals{JSON: tc.json, Quiet: tc.quiet}, d: Deps{Stdout: &out, Stderr: &out, IsTerminal: func(any) bool { return tc.tty }}}
			r.startUpdateCheck(s)
			r.finishUpdateCheck()
			if r.updateResult != nil || transport.calls.Load() != 0 || out.Len() != 0 {
				t.Fatalf("opt-out started check or printed notice: %s", out.String())
			}
		})
	}
}

// upgradeExampleTransport serves public release fixtures in memory; it cannot reach the network.
type upgradeExampleTransport struct {
	archive []byte
	target  string
}

func (r upgradeExampleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	status := 200
	var body []byte
	switch {
	case strings.HasSuffix(req.URL.Path, "/latest"):
		status = 302
		header.Set("Location", "https://github.com/Or4cu1o/mothdex-cli/releases/tag/v0.2.0")
	case strings.HasSuffix(req.URL.Path, ".atom"):
		body = []byte(`<feed/>`)
	case strings.HasSuffix(req.URL.Path, "checksums.txt"):
		ext := ".tar.gz"
		if goruntime.GOOS == "windows" {
			ext = ".zip"
		}
		sum := sha256.Sum256(r.archive)
		body = fmt.Appendf(nil, "%x  mothdex_0.2.0_%s%s\n", sum, r.target, ext)
	case strings.HasSuffix(req.URL.Path, ".sigstore.json"):
		body = []byte("fixture")
	default:
		body = r.archive
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
}

type exampleVerifier struct{}

func (exampleVerifier) Verify(context.Context, update.VerifyInput) (string, error) {
	return "cosign", nil
}
func testUpgradeExamples(t *testing.T, example string) {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "probe")
	if goruntime.GOOS == "windows" {
		probe += ".exe"
	}
	cmd := exec.CommandContext(context.Background(), "go", "build", "-ldflags=-X main.version=0.2.0", "-o", probe, "../../update/testdata/probe") //nolint:gosec // private fixture output, fixed build arguments.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe fixture: %v %s", err, out)
	}
	binary, err := os.ReadFile(probe) //nolint:gosec // generated test binary in private t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	name := "mothdex"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	files := map[string][]byte{name: binary, "LICENSE": []byte("fixture"), "README.md": []byte("fixture"), "CHANGELOG.md": []byte("## [0.2.0]\nUpdate\n## [0.1.0]"), "THIRD_PARTY/go-modules-mothdex.txt": []byte("fixture")}
	var archive bytes.Buffer
	if goruntime.GOOS == "windows" {
		z := zip.NewWriter(&archive)
		for n, b := range files {
			w, e := z.Create(n)
			if e != nil {
				t.Fatal(e)
			}
			_, _ = w.Write(b)
		}
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		g := gzip.NewWriter(&archive)
		tarball := tar.NewWriter(g)
		for n, b := range files {
			if err = tarball.WriteHeader(&tar.Header{Name: n, Size: int64(len(b)), Mode: 0755}); err != nil {
				t.Fatal(err)
			}
			_, _ = tarball.Write(b)
		}
		if err = tarball.Close(); err != nil {
			t.Fatal(err)
		}
		if err = g.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, line := range strings.Split(example, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		t.Run(line, func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, name)
			if err := os.WriteFile(exe, []byte("old"), 0700); err != nil { //nolint:gosec // executable installation fixture.
				t.Fatal(err)
			} //nolint:gosec // executable installation fixture.
			target := goruntime.GOOS + "_" + goruntime.GOARCH
			if goruntime.GOARCH == "arm" {
				t.Skip("GOARM target covered in internal/update")
			}
			state := update.State{SchemaVersion: 1, Source: "installer", Installer: "install.sh", Dir: dir, Version: "0.1.0", Target: target, Profile: "generic", InstalledAt: "2026-11-26T14:00:00Z", UpdatedAt: "2026-11-26T14:00:00Z", UpdatedBy: "installer"}
			if goruntime.GOOS == "windows" {
				state.Installer = "install.ps1"
			}
			b, _ := json.Marshal(state)
			if err := os.WriteFile(filepath.Join(dir, ".mothdex-install.json"), b, 0600); err != nil {
				t.Fatal(err)
			}
			makeUpdater := func(channel string) (*update.Updater, error) {
				return update.New(update.Options{Exe: exe, Current: "0.1.0", Channel: channel, DataDir: t.TempDir(), Client: &http.Client{Transport: upgradeExampleTransport{archive.Bytes(), target}}, Verifier: exampleVerifier{}})
			}
			u, err := makeUpdater("stable")
			if err != nil {
				t.Fatal(err)
			}
			d, out := helpDeps(t)
			d.ProgramName = "mdex"
			d.Stdin = strings.NewReader("yes\n")
			d.IsTerminal = func(any) bool { return true }
			d.Open = func(context.Context, Globals) (*Session, error) {
				return &Session{T: d.Translator("en_us"), Updater: u, NewUpdater: makeUpdater, UpdateChannel: "stable"}, nil
			}
			if line == "cancel_prompt" {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				rd, wr := io.Pipe()
				defer func() { _ = rd.Close() }()
				defer func() { _ = wr.Close() }()
				readerDone := make(chan struct{})
				d.Stdin = cancelPromptReader{rd, cancel, readerDone}
				if code := Execute(ctx, d, []string{"upgrade"}); code != 130 {
					t.Fatalf("exit %d: %s", code, out)
				}
				_ = wr.Close()
				<-readerDone
				stages, e := filepath.Glob(filepath.Join(dir, ".mothdex-update-*"))
				if e != nil || len(stages) != 0 {
					t.Fatalf("staging retained: %v %v", stages, e)
				}
				old, e := os.ReadFile(exe) //nolint:gosec // private fixture path.
				if e != nil || string(old) != "old" {
					t.Fatalf("install changed: %q %v", old, e)
				}
				return
			}
			if line == "shell_abort" {
				session, e := d.Open(context.Background(), Globals{})
				if e != nil {
					t.Fatal(e)
				}
				runner := NewRunner(d, session, true)
				calls := 0
				runner.SetPrompt(func(string) (string, error) { calls++; return "", ErrPromptAborted })
				if code := runner.Run(context.Background(), []string{"upgrade"}); code != 130 {
					t.Fatalf("abort exit %d: %s", code, out)
				}
				if code := runner.Run(context.Background(), []string{"version"}); code != 0 || calls != 1 {
					t.Fatalf("next command exit=%d prompts=%d", code, calls)
				}
				return
			}
			fields := strings.Fields(line)
			if code := Execute(context.Background(), d, fields[1:]); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
		})
	}
}

func TestUpgradeJSONUnavailableIncludesReason(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "mothdex")
	if err := os.WriteFile(exe, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	transport := new(countingTransport)
	u, err := update.New(update.Options{Exe: exe, Current: "1.0.0", Client: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	d, out := helpDeps(t)
	var stderr bytes.Buffer
	d.Stderr = &stderr
	d.IsTerminal = func(any) bool { return true }
	d.Open = func(context.Context, Globals) (*Session, error) {
		return &Session{Updater: u, UpdateCheck: true, T: d.Translator("en_us")}, nil
	}
	if code := Execute(context.Background(), d, []string{"upgrade", "--check", "--json"}); code != 9 {
		t.Fatalf("exit=%d %s %s", code, out, stderr.String())
	}
	var doc struct {
		Error struct {
			Code    string         `json:"code"`
			Exit    int            `json:"exit_code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Error.Code != "update_unavailable" || doc.Error.Exit != 9 || doc.Error.Details["reason"] != "no_state" || transport.calls.Load() != 0 {
		t.Fatalf("unexpected error: %+v", doc)
	}
	if strings.Contains(stderr.String(), "{install_command}") || !strings.Contains(stderr.String(), "releases/latest/download/install.") {
		t.Fatalf("installer hint missing: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "New version available") {
		t.Fatal("JSON run printed update notice")
	}
}

func TestCleanupSkipsBinaryOnlyModes(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}, {"--help"}, {"completion", "bash"}, {"__complete", ""}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			d, out := helpDeps(t)
			d.CleanupOld = func() { t.Error("read-only mode invoked cleanup") }
			d.Open = func(context.Context, Globals) (*Session, error) {
				t.Fatal("read-only mode opened service")
				return nil, nil
			}
			if code := Execute(context.Background(), d, args); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
		})
	}
}

func TestVerificationFailureHintIncludesReinstallCommand(t *testing.T) {
	d, out := helpDeps(t)
	r := &runtime{d: d}
	err := &app.Error{Code: "integrity", Message: "release signature failed", Args: map[string]any{"hint_key": "update.verification_hint", "install_command": update.InstallCommand()}}
	if code := r.fail(err); code != 5 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), update.InstallCommand()) || strings.Contains(out.String(), "{install_command}") {
		t.Fatalf("reinstall hint missing: %s", out.String())
	}
}

// Cancels only once the command is actually waiting on stdin; Close releases its reader.
type cancelPromptReader struct {
	reader io.Reader
	cancel context.CancelFunc
	done   chan struct{}
}

func (r cancelPromptReader) Read(b []byte) (int, error) {
	r.cancel()
	defer close(r.done)
	return r.reader.Read(b)
}
func TestUpgradePromptCancellation(t *testing.T) {
	testUpgradeExamples(t, "cancel_prompt\nshell_abort")
}
