package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// exitError carries an exit code that differs from the one of its error code (batch downloads:
// 7 when some jobs failed, 130 when interrupted).
type exitError struct {
	err    error
	code   int
	silent bool // the per-job lines already said it: no second error block
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// runtime is the state of one command-line run.
type runtime struct {
	d    Deps
	g    Globals
	p    *printer
	sess *Session
	ctx  context.Context
	cmd  string // command name used in the --json envelope

	jsonDone bool // the envelope was already written (a partial batch result)

	// run is set when the line comes from the Shell: the session is already open and belongs to the
	// Shell, the command tree has no shell, tui or gui, and some flags are start-up only.
	run *Runner
}

// Execute runs the command line and returns the process exit code. It prints every error itself.
func Execute(ctx context.Context, d Deps, args []string) int {
	r := &runtime{d: d, ctx: ctx}
	root := r.rootCommand()
	root.SetArgs(args)
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	err := root.ExecuteContext(ctx)
	if r.sess != nil && r.sess.Close != nil {
		defer r.sess.Close()
	}
	if err == nil {
		return 0
	}
	r.noteCommand(root, args)
	return r.fail(err)
}

// noteCommand names the command that failed argument validation, which cobra reports before any hook
// runs, so that the usage hint points at "mothdex <command> --help" rather than the root.
func (r *runtime) noteCommand(root *cobra.Command, args []string) {
	if r.cmd != "" {
		return
	}
	if c, _, err := root.Find(args); err == nil && c != root {
		r.cmd = commandName(c)
	}
}

// fail prints err and returns its exit code. Errors are never silenced, not even by -q.
func (r *runtime) fail(err error) int {
	var ee *exitError
	code := 0
	inner := err
	if errors.As(err, &ee) {
		code, inner = ee.code, ee.err
	}
	ae := asAppError(inner)
	if !isAppError(inner) {
		ae = usageFromCobra(inner)
	}
	if ae.Code == app.CodeUsage {
		if _, ok := ae.Args["command"]; !ok {
			if ae.Args == nil {
				ae.Args = map[string]any{}
			}
			ae.Args["command"] = r.cmd // "" for the root: the hint then says "mothdex --help"
		}
	}
	if ee != nil && ee.silent {
		return code
	}
	if code == 0 {
		code = ae.ExitCode()
	}
	p := r.printer()
	if p.g.JSON && !r.jsonDone {
		_ = p.writeJSON(r.cmd, nil, ae)
	}
	p.renderError(ae)
	return code
}

func isAppError(err error) bool {
	var ae *app.Error
	return errors.As(err, &ae)
}

// usageFromCobra turns a cobra parse error (unknown flag, wrong argument count) into a usage error.
func usageFromCobra(err error) *app.Error {
	return &app.Error{Code: app.CodeUsage, Message: err.Error(), Args: map[string]any{"detail": err.Error()}}
}

func usageErr(command, format string, a ...any) *app.Error {
	d := fmt.Sprintf(format, a...)
	return &app.Error{Code: app.CodeUsage, Message: d, Args: map[string]any{"detail": d, "command": command}}
}

// printer returns the printer, creating one with the best translator available when the service
// has not opened yet (errors that happen before Open).
func (r *runtime) printer() *printer {
	if r.p == nil {
		var t *i18n.Translator
		if r.run != nil {
			t = r.run.s.T
		} else if r.d.Translator != nil {
			t = r.d.Translator(r.g.Lang)
		}
		r.p = newPrinter(r.d, &r.g, t)
	}
	return r.p
}

// open builds the service once, after the flags are parsed.
func (r *runtime) open() (*Session, error) {
	if r.sess != nil {
		return r.sess, nil
	}
	if r.run != nil { // the Shell opened the session once, and reported its warnings then
		r.sess = r.run.s
		p := r.printer()
		p.setColor(r.sess.Color)
		p.logPath = r.sess.LogPath
		return r.sess, nil
	}
	s, err := r.d.Open(r.ctx, r.g)
	if err != nil {
		return nil, err
	}
	r.sess = s
	p := r.printer()
	p.t = s.T
	p.setColor(s.Color)
	p.logPath = s.LogPath
	for _, w := range s.Warnings {
		p.warning(w)
	}
	return s, nil
}

func (r *runtime) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "mothdex",
		Short:         "Search and download from the Mothdex catalog",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if r.g.Version {
				return r.runVersion()
			}
			if r.shellOnBareRoot() {
				return r.runShell()
			}
			r.cmd = "help"
			return cmd.Help()
		},
	}
	f := root.PersistentFlags()
	f.BoolVar(&r.g.JSON, "json", false, "print one JSON document on stdout")
	f.BoolVarP(&r.g.Quiet, "quiet", "q", false, "print only the result and errors")
	f.BoolVarP(&r.g.Verbose, "verbose", "v", false, "show the cause of errors")
	f.BoolVar(&r.g.NoColor, "no-color", false, "turn colors off")
	f.StringVar(&r.g.Lang, "lang", "", "interface language, for example pt_br")
	f.StringVar(&r.g.Config, "config", "", "configuration file")
	f.StringVar(&r.g.Profile, "profile", "", "frontend profile id")
	f.BoolVarP(&r.g.Force, "force", "f", false, "skip prompts; allow overwriting files")
	f.BoolVar(&r.g.Refresh, "refresh", false, "skip cache reads, still write")
	f.BoolVar(&r.g.Version, "version", false, "print the version and exit")
	logValues(f, "json", "quiet", "verbose", "no-color", "lang", "profile", "force", "refresh", "version")
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		r.cmd = commandName(c)
		return usageErr(r.cmd, "%s", err.Error())
	})
	root.PersistentPreRunE = func(c *cobra.Command, _ []string) error {
		r.cmd = commandName(c)
		r.g.Command = r.cmd
		return r.rejectStartupFlags(c)
	}
	root.AddCommand(
		r.searchCmd(), r.infoCmd(), r.randomCmd(), r.downloadCmd(), r.boxartCmd(),
		r.platformsCmd(), r.regionsCmd(), r.dbInfoCmd(), r.historyCmd(), r.cacheCmd(),
		r.configCmd(), r.profileCmd(), r.versionCmd(),
	)
	if r.run == nil { // the Shell never runs shell, tui or gui
		root.AddCommand(r.shellCmd(), r.tuiCmd(), r.guiCmd())
	}
	r.configureHelp(root)
	return root
}

// commandName is the name used in the --json envelope: the top-level command, so subcommands of
// cache, config and profile all report that group.
func commandName(c *cobra.Command) string {
	if !c.HasParent() {
		return "" // the root command itself
	}
	for c.Parent().HasParent() {
		c = c.Parent()
	}
	return c.Name()
}
