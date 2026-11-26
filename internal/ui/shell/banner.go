package shell

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// banner prints the one-line welcome (shell.md section 2). Offline it says `offline` instead of the
// entry count and never shows an error.
func (s *Shell) banner(ctx context.Context) {
	bctx, cancel := context.WithTimeout(ctx, networkWait)
	defer cancel()
	entries := s.t("ui.shell.offline", nil)
	if res, err := s.o.Service.DBInfo(bctx); err == nil {
		entries = s.o.T.N("ui.shell.entries", int(res.Value.TotalEntries), i18n.Args{"count": s.numbers.Sprintf("%d", res.Value.TotalEntries)})
	}
	line := s.t("ui.shell.banner", i18n.Args{"version": s.o.Version, "entries": entries, "profile": s.o.Service.ActiveProfile().ID})
	_, _ = fmt.Fprintln(s.o.Out, app.SanitizeText(line))
	if s.o.UpdateNotice != "" {
		_, _ = fmt.Fprintln(s.o.Out, app.SanitizeText(s.o.UpdateNotice))
	}
}

// helpRow is one line of the command list.
type helpRow struct{ name, summary string }

// metaHelp are the Shell's own commands in the order the list shows them: the name as typed and the
// i18n key of its summary.
var metaHelp = []struct{ shown, key string }{
	{"help [command]", "help"}, {"clear", "clear"}, {"exit", "exit"}, {"quit", "quit"}, {"last", "last"},
	{"n", "n"}, {"p", "p"}, {"page <n>", "page"}, {"sort <key>", "sort"}, {"<#>", "number"}, {"d <#>", "d"},
}

// help lists every command with a one-line summary, or prints the help of one: the same text as
// `mothdex <command> --help`.
func (s *Shell) help(ctx context.Context, args []string) int {
	if len(args) == 0 {
		s.helpList()
		return 0
	}
	name := args[0]
	if source, ok := s.o.Runner.(interface{ HelpTopics() []string }); ok {
		for _, topic := range source.HelpTopics() {
			if topic == name {
				return s.exec(ctx, func(c context.Context) int { return s.o.Runner.Run(c, append([]string{"help"}, args...)) })
			}
		}
	}
	if _, ok := findCommand(s.o.Runner.Commands(), name); ok {
		return s.exec(ctx, func(c context.Context) int { return s.o.Runner.Run(c, []string{name, "--help"}) })
	}
	for _, m := range metaHelp {
		if m.key == name || strings.Fields(m.shown)[0] == name {
			_, _ = fmt.Fprintln(s.o.Out, s.t("ui.shell.help."+m.key, nil))
			return 0
		}
	}
	return s.unknown(name)
}

func (s *Shell) helpList() {
	var rows []helpRow
	cmds := s.o.Runner.Commands()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	for _, c := range cmds {
		if c.Name != "help" {
			rows = append(rows, helpRow{c.Name, c.Short})
		}
	}
	for _, m := range metaHelp {
		rows = append(rows, helpRow{m.shown, s.t("ui.shell.help."+m.key, nil)})
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r.name))
	}
	for _, r := range rows {
		_, _ = fmt.Fprintf(s.o.Out, "  %-*s  %s\n", width, r.name, r.summary)
	}
}
