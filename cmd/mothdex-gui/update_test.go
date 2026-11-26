//go:build gui

package main

import (
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

// With the check off the model gets no updater at all, so there is no notice, no action and no request.
func TestNoUpdaterWhenTheCheckIsOff(t *testing.T) {
	off := newUpdates(&update.Updater{}, false)
	if off.model() != nil || newUpdates(nil, true).model() != nil {
		t.Error("the GUI got an updater")
	}
	off.wait() // a nil updater has nothing to wait for
	if newUpdates(&update.Updater{}, true).model() == nil {
		t.Error("no updater with the check on")
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
