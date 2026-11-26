//go:build gui

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

// These are variables only so that a test can replace them.
var (
	// checkBudget bounds the automatic check (docs/contracts/cli/update.md section 5).
	checkBudget = 3 * time.Second
	// installBudget is how long a run that is leaving waits for an installation: its own steps are bounded, and
	// leaving in the middle of the swap would strand it without its check or rollback.
	installBudget = time.Minute
	// restartProcess replaces this run with the program at path (TM-40).
	restartProcess = restart
)

// guiUpdates gives the model the updater the CLI uses (internal/update); the rules are all there.
type guiUpdates struct {
	u       *update.Updater
	running sync.WaitGroup // an Apply that has not returned
}

// newUpdates is nil when the automatic check is off: compose.Session.UpdateCheck holds the result of every off
// switch (update.check, the environment, CI, a development build, an unmanaged installation) and logged why. The
// GUI then offers nothing and opens no connection.
func newUpdates(u *update.Updater, check bool) *guiUpdates {
	if u == nil || !check {
		return nil
	}
	return &guiUpdates{u: u}
}

// model is the value for gui.Options: a nil *guiUpdates must be a nil interface.
func (g *guiUpdates) model() gui.Updates {
	if g == nil {
		return nil
	}
	return g
}

func (g *guiUpdates) Check(ctx context.Context) (gui.Release, error) {
	ctx, cancel := context.WithTimeout(ctx, checkBudget)
	defer cancel()
	res, err := g.u.Check(ctx, false)
	if err != nil || !res.Available {
		return gui.Release{}, err
	}
	return gui.Release{Version: res.Latest, PageURL: res.PageURL, Notify: res.Notify}, nil
}

func (g *guiUpdates) MarkNotified(version string) error {
	g.u.MarkNotified(version)
	return nil
}

func (g *guiUpdates) Apply(ctx context.Context, progress func(gui.UpdateProgress), confirm func(gui.Staged) bool) error {
	g.running.Add(1)
	defer g.running.Done()
	_, err := g.u.Apply(ctx, update.ApplyOptions{
		Progress: func(p update.Progress) {
			progress(gui.UpdateProgress{Step: p.Step, Done: p.Done, Total: p.Total})
		},
		Confirm: func(s update.Staged) bool {
			return confirm(gui.Staged{From: s.From, To: s.To, Signature: s.Signature, Notes: s.Notes})
		},
	})
	return err
}

// wait lets a running Apply end, within installBudget.
func (g *guiUpdates) wait() {
	if g != nil {
		closeWithin(installBudget, g.running.Wait)
	}
}

// executable is the path of this program with links resolved. It is read before any update: afterwards the
// system may name the replaced file.
func executable(log *slog.Logger) string {
	path, err := os.Executable()
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	if err != nil {
		log.Warn("gui: the program path is unknown", "err", err)
		return ""
	}
	return path
}
