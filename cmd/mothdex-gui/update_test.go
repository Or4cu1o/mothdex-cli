//go:build gui

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

// With the check off, or an installation the installer does not manage, the model gets no updater at all, so
// there is no notice, no action and no request. The client of this updater fails the test if it is used.
func TestNoUpdaterWhenTheCheckIsOffOrTheInstallationIsNotManaged(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "mothdex-gui")
	if err := os.WriteFile(exe, nil, 0o700); err != nil { //nolint:gosec // an empty stand-in for the program
		t.Fatal(err)
	}
	u, err := update.New(update.Options{Exe: exe, Current: "0.2.0", DataDir: t.TempDir(), Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		enabled bool
		env     map[string]string
	}{
		"update.check false":      {false, nil},
		"MOTHDEX_NO_UPDATE_CHECK": {true, map[string]string{"MOTHDEX_NO_UPDATE_CHECK": "1"}},
		"CI":                      {true, map[string]string{"CI": "true"}},
		"no install state":        {true, nil},
	} {
		got := newUpdates(u, c.enabled, func(k string) string { return c.env[k] })
		if got.model() != nil {
			t.Errorf("%s: the GUI got an updater", name)
		}
		got.wait() // a nil updater has nothing to wait for
	}
	if newUpdates(nil, true, os.Getenv).model() != nil {
		t.Error("no updater at all")
	}
}

// Leaving waits for an installation that is running, and gives up after the budget.
func TestLeavingWaitsForAnInstallation(t *testing.T) {
	defer func(old time.Duration) { installBudget = old }(installBudget)
	installBudget = time.Hour
	g := &guiUpdates{}
	g.running.Add(1)
	order := make(chan string, 2)
	done := make(chan struct{})
	go func() {
		g.wait()
		order <- "left"
		close(done)
	}()
	order <- "installed"
	g.running.Done()
	<-done
	if first := <-order; first != "installed" {
		t.Errorf("%q came first", first)
	}
	installBudget = time.Millisecond
	g.running.Add(1)
	defer g.running.Done()
	g.wait() // a stuck installation does not keep the program for ever
}
