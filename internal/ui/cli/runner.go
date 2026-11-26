package cli

import (
	"context"
	"errors"
	"slices"
	"sort"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// ErrPromptAborted is what a prompt hook returns when the user gave up (Ctrl+C or end of input).
var ErrPromptAborted = errors.New("prompt aborted")

// CommandInfo describes a command for the Shell's help and completion.
type CommandInfo struct {
	Name  string
	Short string
	Flags []FlagInfo
	Subs  []CommandInfo
}

// FlagInfo describes a flag of a command, global flags included.
type FlagInfo struct {
	Name       string // long name, without the dashes
	Short      string // one letter, or ""
	TakesValue bool
}

// Runner runs command lines against one open Session for the Shell: the same command tree and
// the same output as the CLI, and the session survives from line to line (ui.md section 2.1).
type Runner struct {
	d           Deps
	s           *Session
	interactive bool

	mu     sync.Mutex
	page   *app.SearchPage
	entry  *mothdex.Entry
	prompt func(label string) (string, error)

	cmdsOnce sync.Once
	cmds     []CommandInfo
}

// NewRunner returns a Runner over an open session. With interactive set (stdin is a terminal) the
// download command may prompt: the link chooser and the confirmation.
func NewRunner(d Deps, s *Session, interactive bool) *Runner {
	return &Runner{d: d, s: s, interactive: interactive}
}

// SetPrompt routes the prompts of the commands (chooser, confirmation) through the Shell's line
// editor. The hook prints the label itself and returns the answer; ErrPromptAborted cancels.
func (r *Runner) SetPrompt(fn func(label string) (string, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompt = fn
}

func (r *Runner) promptHook() func(string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.prompt
}

// Run executes one command line, without the program name. It prints every error and returns the
// exit code. A fresh command tree is built per line, so flag values never leak between lines.
func (r *Runner) Run(ctx context.Context, args []string) int {
	rt := r.newRuntime(ctx)
	root := rt.rootCommand()
	root.SetArgs(append([]string{}, args...)) // a nil slice would make cobra read os.Args
	root.SetOut(r.d.Stdout)
	root.SetErr(r.d.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		rt.noteCommand(root, args)
		return rt.fail(err)
	}
	return 0
}

// RunSearch runs a search and renders it like the search command. It serves n, p, page, sort and
// last of the Shell.
func (r *Runner) RunSearch(ctx context.Context, q app.SearchQuery) int {
	rt := r.newRuntime(ctx)
	rt.cmd = "search"
	sortKey := string(q.Sort)
	if sortKey == "" {
		sortKey = string(app.SortRelevance)
	}
	f := searchFlags{platforms: q.Platforms, regions: q.Regions, serial: q.Serial, sort: sortKey, page: max(q.Page, 1), pageSize: q.PageSize}
	err := rt.exec(func(ctx context.Context, s *Session) (outcome, error) { return rt.runSearch(ctx, s, q.Text, f) })
	if err != nil {
		return rt.fail(err)
	}
	return 0
}

// LastPage is the last search page shown in this process.
func (r *Runner) LastPage() (app.SearchPage, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.page == nil {
		return app.SearchPage{}, false
	}
	return *r.page, true
}

// LastEntry is the last entry shown by info in this process, for completion.
func (r *Runner) LastEntry() (mothdex.Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entry == nil {
		return mothdex.Entry{}, false
	}
	return *r.entry, true
}

func (r *Runner) setPage(p app.SearchPage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.page = &p
}

func (r *Runner) setEntry(e mothdex.Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entry = &e
}

func (r *Runner) newRuntime(ctx context.Context) *runtime { return &runtime{d: r.d, ctx: ctx, run: r} }

// Commands lists the commands the Shell can run, sorted by name, with their flags.
func (r *Runner) Commands() []CommandInfo {
	r.cmdsOnce.Do(func() {
		rt := r.newRuntime(context.Background())
		root := rt.rootCommand()
		localizeHelp(root, rt.helpTranslator())
		for _, c := range root.Commands() {
			if !c.Hidden {
				r.cmds = append(r.cmds, describe(c))
			}
		}
		sort.Slice(r.cmds, func(i, j int) bool { return r.cmds[i].Name < r.cmds[j].Name })
	})
	return slices.Clone(r.cmds)
}

func describe(c *cobra.Command) CommandInfo {
	info := CommandInfo{Name: c.Name(), Short: c.Short}
	seen := map[string]bool{}
	add := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			if f.Hidden || seen[f.Name] || slices.Contains(startupOnly, f.Name) {
				return
			}
			seen[f.Name] = true
			info.Flags = append(info.Flags, FlagInfo{Name: f.Name, Short: f.Shorthand, TakesValue: f.NoOptDefVal == ""})
		})
	}
	add(c.NonInheritedFlags())
	add(c.InheritedFlags())
	for _, sub := range c.Commands() {
		if !sub.Hidden {
			info.Subs = append(info.Subs, describe(sub))
		}
	}
	return info
}

// HelpTopics lists registered reference topics so the Shell can route help consistently.
func (r *Runner) HelpTopics() []string {
	topics := append(referenceTopics(r.s.T), r.d.HelpTopics...)
	names := make([]string, 0, len(topics))
	for _, topic := range topics {
		names = append(names, topic.Name)
	}
	return names
}
