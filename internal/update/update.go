// Package update discovers, verifies and transactionally replaces managed release installations.
// All interfaces share this package; no update is applied automatically.
package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// Options supplies process identity and replaceable I/O dependencies.
type Options struct {
	Getenv                         func(string) string
	Client                         *http.Client
	Exe, Current, Channel, DataDir string
	Log                            *slog.Logger
	Now                            func() time.Time
	Verifier                       Verifier
	LookPath                       func(string) (string, error)
}

// Release is validated public metadata, with no unverified release notes.
type Release struct {
	Version, Tag, PageURL string
	Prerelease            bool
}

// Updater serializes Apply calls; Check is safe to call separately.
type Updater struct {
	o          Options
	mu         sync.Mutex
	startTimer func(time.Duration, func()) idleTimer
}

// New builds an updater with a pinned HTTPS origin and validates its channel.
func New(o Options) (*Updater, error) {
	if o.Channel == "" {
		o.Channel = "stable"
	}
	if o.Channel != "stable" && o.Channel != "beta" {
		return nil, failure(app.CodeUsage, "channel", nil)
	}
	if o.Exe == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, unavailable("no_state", err)
		}
		o.Exe = exe
	}
	resolved, err := filepath.EvalSymlinks(o.Exe)
	if err != nil {
		return nil, unavailable("no_state", err)
	}
	o.Exe = resolved
	if !filepath.IsAbs(o.Exe) {
		return nil, unavailable("bad_state", nil)
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	o.Client = newClient(o.Client)
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.Verifier == nil {
		o.Verifier = DefaultVerifier(o.LookPath)
	}
	return &Updater{o: o, startTimer: func(d time.Duration, f func()) idleTimer { return time.AfterFunc(d, f) }}, nil
}
func (u *Updater) probe(ctx context.Context, path, version string, gui, eligibility bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"--version", "--json"}
	if gui {
		args = []string{"--version"}
	}
	if eligibility {
		args = []string{"--probe"}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = filepath.Dir(path)
	cmd.Env = probeEnv()
	out := &limitedOutput{remaining: 64 << 10}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return err
	}
	if gui {
		return nil
	}
	var response struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := decode(out.bytes, &response, false); err != nil {
		return err
	}
	if response.Data.Version != version {
		return errors.New("new binary version mismatch")
	}
	return nil
}

type limitedOutput struct {
	bytes     []byte
	remaining int
}

func (w *limitedOutput) Write(b []byte) (int, error) {
	if len(b) > w.remaining {
		return 0, errors.New("probe output limit")
	}
	w.bytes = append(w.bytes, b...)
	w.remaining -= len(b)
	return len(b), nil
}
func probeEnv() []string {
	// Prevent config, loader and language injection. Staged Windows DLL lookup is sibling-only.
	var env []string
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "TMPDIR", "TEMP", "TMP"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}
