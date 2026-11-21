package cli

import (
	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// startupOnly are the flags a Shell line cannot change: the service reads them once when it opens
// (ui.md section 2.1). --allow-executable and --jobs would be accepted and ignored otherwise.
var startupOnly = []string{"config", "lang", "allow-executable", "jobs"}

// rejectStartupFlags turns a start-up-only flag typed inside the Shell into a usage error.
func (r *runtime) rejectStartupFlags(c *cobra.Command) error {
	if r.run == nil {
		return nil
	}
	for _, name := range startupOnly {
		if f := c.Flags().Lookup(name); f != nil && f.Changed {
			return usageErr(r.cmd, "%s", r.printer().T("ui.shell.flag_startup_only", i18n.Args{"flag": "--" + name}))
		}
	}
	return nil
}

func (r *runtime) isTTY(stream any) bool { return r.d.IsTerminal != nil && r.d.IsTerminal(stream) }

// shellOnBareRoot is true for `mothdex` with no arguments on a terminal: it starts the Shell.
func (r *runtime) shellOnBareRoot() bool {
	return r.run == nil && r.d.Shell != nil && !r.g.JSON && r.isTTY(r.d.Stdin) && r.isTTY(r.d.Stdout)
}

func (r *runtime) shellCmd() *cobra.Command {
	return &cobra.Command{
		Use: "shell", Short: "Start the interactive shell", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if r.d.Shell == nil {
				return r.soon("shell")
			}
			if r.g.JSON {
				return usageErr("shell", "--json cannot be used with the shell")
			}
			return r.runShell()
		},
	}
}

// runShell opens the service once and hands the session to the Shell launcher. The Shell opens it
// with the downloads capability, and with Force and AllowExecutable off even when -f was given at
// start-up: overwriting for a whole session must not hang on one flag (ui.md section 2.1).
func (r *runtime) runShell() error {
	r.cmd, r.g.Command = "shell", "shell"
	r.g.Downloads, r.g.Shell = true, true
	s, err := r.open()
	if err != nil {
		return err
	}
	stdin := r.isTTY(r.d.Stdin)
	runner := NewRunner(r.d, s, stdin)
	code := r.d.Shell(r.ctx, ShellEnv{
		Runner: runner, Session: s, Stdin: r.d.Stdin, Stdout: r.d.Stdout, Stderr: r.d.Stderr,
		IsTerminal: stdin && r.isTTY(r.d.Stdout), Interrupts: r.d.Interrupts,
	})
	if code == 0 {
		return nil
	}
	return &exitError{err: &app.Error{Code: app.CodeCanceled, Message: "the shell ended"}, code: code, silent: true}
}

func (r *runtime) tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use: "tui", Short: "Start the full-screen terminal UI", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if r.d.TUI == nil {
				return r.soon("tui")
			}
			if r.g.JSON {
				return usageErr("tui", "--json cannot be used with the TUI")
			}
			if !r.isTTY(r.d.Stdin) || !r.isTTY(r.d.Stdout) {
				return usageErr("tui", "%s", r.printer().T("ui.tui.needs_terminal", nil))
			}
			return r.runTUI()
		},
	}
}

// runTUI opens the service once (like the Shell: Force and AllowExecutable stay off) and hands the
// session to the TUI launcher.
func (r *runtime) runTUI() error {
	r.cmd, r.g.Command = "tui", "tui"
	r.g.Downloads, r.g.Shell = true, true
	s, err := r.open()
	if err != nil {
		return err
	}
	code := r.d.TUI(r.ctx, TUIEnv{Session: s, Stdin: r.d.Stdin, Stdout: r.d.Stdout, Stderr: r.d.Stderr, Interrupts: r.d.Interrupts})
	if code == 0 {
		return nil
	}
	return &exitError{err: &app.Error{Code: app.CodeCanceled, Message: "the TUI ended"}, code: code, silent: true}
}
