// Package tui is the full-screen terminal UI (docs/ux/tui.md) on Bubble Tea v2. It is a thin layer
// over the app service: the screens draw and map keys, and every rule that is not about that lives
// below it (docs/contracts/cli/ui.md section 3).
package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/cli"
)

const exitInterrupted = 130

// Run is the launcher the composition root injects as cli.Deps.TUI.
func Run(ctx context.Context, tenv cli.TUIEnv) int {
	e := newEnvFrom(ctx, tenv.Session)
	m := newModel(e, newSearch(readState(e.dataDir)))
	p := tea.NewProgram(guard{m, e}, tea.WithContext(ctx), tea.WithInput(tenv.Stdin), tea.WithOutput(tenv.Stdout), tea.WithoutSignalHandler())
	if tenv.Interrupts != nil {
		// Ctrl+C is a key in raw mode. A SIGINT sent from outside (kill -INT, a timeout wrapper) takes
		// the same road, so it asks to quit like Ctrl+C does instead of being swallowed.
		sig, release := tenv.Interrupts()
		defer release()
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			for {
				select {
				case <-sig:
					p.Send(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
				case <-stop:
					return
				}
			}
		}()
	}
	if _, err := p.Run(); err != nil {
		if ctx.Err() != nil || errors.Is(err, tea.ErrProgramKilled) {
			return exitInterrupted
		}
		_, _ = fmt.Fprintln(tenv.Stderr, "mothdex:", err)
		f := e.failureOf(&app.Error{Code: app.CodeInternal, Message: "the TUI stopped", Cause: err})
		_, _ = fmt.Fprintln(tenv.Stderr, " ", cli.CodeLine(e.t, e.st.utf8, f.code, f.ref))
		if f.log != "" {
			_, _ = fmt.Fprintln(tenv.Stderr, " ", cli.LogLine(e.t, f.log))
		}
		return 1
	}
	return 0
}

// guard writes the crash file of a panic in Update or View while the stack is still there, and
// lets Bubble Tea recover and restore the terminal.
type guard struct {
	m tea.Model
	e *env
}

func (g guard) Init() tea.Cmd { return g.m.Init() }

func (g guard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer g.crash()
	next, cmd := g.m.Update(msg)
	return guard{next, g.e}, cmd
}

func (g guard) View() tea.View {
	defer g.crash()
	return g.m.View()
}

func (g guard) crash() {
	v := recover()
	if v == nil {
		return
	}
	if g.e.rep != nil && g.e.rep.Logs != nil {
		ref := g.e.rep.Ref("tui", &app.Error{Code: app.CodeInternal, Message: "panic", Cause: fmt.Errorf("%v", v)},
			slog.String("stack", string(debug.Stack())))
		_, _ = g.e.rep.Logs.WriteCrash(v, debug.Stack(), ref) // the log line already holds the stack
	}
	panic(v)
}

func newEnvFrom(ctx context.Context, s *cli.Session) *env {
	return &env{
		ctx: ctx, svc: s.Svc, t: s.T, st: newStyles(s.NoColor, s.UTF8), prefer: s.Prefer, confirm: s.Confirm, boxart: s.Boxart, pageSize: s.PageSize,
		dataDir: s.DataDir, logPath: s.LogPath, log: s.Log, rep: &cli.Reporter{Logs: s.Logs}, now: time.Now, after: realAfter, seq: &atomic.Int64{}, num: printer(s.T),
	}
}
