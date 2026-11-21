package gui

import (
	"context"
	"log/slog"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// Exit budget of docs/contracts/cli/gui.md section 5: Cancel, Wait (with the refresh bounded) and Close
// together take at most two seconds, and the process exits when they are used up.
const (
	exitBudget  = 2 * time.Second
	closeReserv = 300 * time.Millisecond // kept for Close, so a stuck Wait cannot starve the history flush
)

// QuitRequest is how the person asked to quit.
type QuitRequest int

// Ways to ask.
const (
	QuitFromBack QuitRequest = iota // B on Home
	QuitFromMenu                    // Start menu, Quit
)

// QuitStep is what happens next, by the table of docs/ux/gui.md section 5.13.
type QuitStep int

// Steps.
const (
	QuitConfirm QuitStep = iota // "Quit and return to <frontend>?" with Stay (default) and Quit
	QuitNow                     // no dialog: start the exit sequence
	QuitAskBusy                 // "N downloads are still running." with three choices
)

// DecideQuit is the quit decision table: any quit with active or queued downloads asks; with none, B on
// Home confirms and the Start menu quits at once.
func DecideQuit(from QuitRequest, unfinished int) QuitStep {
	switch {
	case unfinished > 0:
		return QuitAskBusy
	case from == QuitFromBack:
		return QuitConfirm
	}
	return QuitNow
}

// exitService is what the exit sequence calls on the app service.
type exitService interface {
	Jobs() []app.JobSnapshot
	Cancel(id app.JobID) error
	Wait(ctx context.Context) (app.BatchResult, error)
	Close() error
}

// Clock is time as the model and the exit sequence see it; tests replace it.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time                         { return time.Now() }
func (systemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// SystemClock is the real clock.
func SystemClock() Clock { return systemClock{} }

// exitResult is what the exit sequence learned, for the exit screen.
type exitResult struct {
	Batch    app.BatchResult
	WaitErr  error
	CloseErr error
	TimedOut bool // the budget ran out before Wait or Close answered
	Canceled int  // jobs the sequence asked to cancel
}

// runExit is the exit sequence: optionally cancel every job that has not finished, Wait for the queue and
// the profile refresh, then Close, all inside the budget. It never blocks past the budget: a Wait that does
// not return is abandoned (its context is canceled) and Close still runs with what is left.
func runExit(svc exitService, clk Clock, cancelJobs bool, budget time.Duration, log *slog.Logger) exitResult {
	var res exitResult
	deadline := clk.Now().Add(budget)
	remaining := func() time.Duration { return max(deadline.Sub(clk.Now()), 0) }

	if cancelJobs {
		for _, j := range svc.Jobs() {
			if j.Terminal() {
				continue
			}
			res.Canceled++
			if err := svc.Cancel(j.ID); err != nil {
				log.Debug("exit: cancel", "job", j.ID, "err", err) // the job may have just finished
			}
		}
	}

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	type waited struct {
		batch app.BatchResult
		err   error
	}
	waitCh := make(chan waited, 1)
	go func() {
		batch, err := svc.Wait(ctx)
		waitCh <- waited{batch, err}
	}()
	select {
	case w := <-waitCh:
		res.Batch, res.WaitErr = w.batch, w.err
	case <-clk.After(max(remaining()-closeReserv, 0)):
		stop()
		res.TimedOut = true
		log.Warn("exit: Wait did not finish inside the budget")
	}

	closeCh := make(chan error, 1)
	go func() { closeCh <- svc.Close() }()
	select {
	case err := <-closeCh:
		res.CloseErr = err
	case <-clk.After(remaining()):
		res.TimedOut = true
		log.Warn("exit: Close did not finish inside the budget")
	}
	if res.WaitErr != nil {
		log.Warn("exit: Wait", "err", res.WaitErr)
	}
	if res.CloseErr != nil {
		log.Warn("exit: Close", "err", res.CloseErr)
	}
	return res
}
