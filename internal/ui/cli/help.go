package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SystemGroup is shared by system commands, including logs and upgrade.
const SystemGroup = "system"

// HelpTopic registers reference text with `mothdex help <name>`. A topic may share a
// command name (config); it takes precedence only under help.
type HelpTopic struct {
	Name, Short string
	Text        func() string
}

func helpID(c *cobra.Command) string {
	if !c.HasParent() {
		return "root"
	}
	path := strings.TrimPrefix(c.CommandPath(), c.Root().Name()+" ")
	return strings.NewReplacer(" ", "_", "-", "_").Replace(path)
}

func (r *runtime) configureHelp(root *cobra.Command) {
	root.Use = strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	root.AddGroup(&cobra.Group{ID: "catalog", Title: "Catalog"}, &cobra.Group{ID: "downloads", Title: "Downloads"}, &cobra.Group{ID: "interfaces", Title: "Interfaces"}, &cobra.Group{ID: SystemGroup, Title: "System"})
	root.SetHelpCommand(r.helpCmd(root))
	root.InitDefaultHelpCmd()
	root.CompletionOptions.DisableDefaultCmd = true
	if r.run == nil {
		root.AddCommand(r.completionCmd())
	}
	for _, c := range root.Commands() {
		switch c.Name() {
		case "search", "info", "random", "platforms", "regions", "db-info":
			c.GroupID = "catalog"
		case "download", "boxart", "history":
			c.GroupID = "downloads"
		case "shell", "tui", "gui":
			c.GroupID = "interfaces"
		default:
			c.GroupID = SystemGroup
		}
	}
	walkCommands(root, func(c *cobra.Command) {
		if h, ok := commandHelp[helpID(c)]; ok {
			c.Long, c.Example = h.long, h.example
		}
		c.InitDefaultHelpFlag()
	})
	root.SetHelpFunc(func(c *cobra.Command, _ []string) { r.printHelp(c) })
	root.SetUsageFunc(func(c *cobra.Command) error { r.printHelp(c); return nil })
}

func walkCommands(c *cobra.Command, f func(*cobra.Command)) {
	f(c)
	for _, sub := range c.Commands() {
		walkCommands(sub, f)
	}
}

func (r *runtime) helpCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{Use: "help [command or topic]", Short: "Help for commands and reference topics", RunE: func(c *cobra.Command, args []string) error {
		if len(args) == 1 {
			for _, topic := range append(referenceTopics(), r.d.HelpTopics...) {
				if topic.Name == args[0] {
					_, err := fmt.Fprintln(c.OutOrStdout(), topic.Text())
					return err
				}
			}
		}
		target, rest, err := root.Find(args)
		if err != nil || len(rest) > 0 {
			return usageErr("help", "unknown help topic: %s", strings.Join(args, " "))
		}
		return target.Help()
	}}
}

func (r *runtime) printHelp(c *cobra.Command) {
	out := c.OutOrStdout()
	_, _ = fmt.Fprintln(out, c.Long+"\n")
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  "+c.UseLine())
	if c == c.Root() {
		_, _ = fmt.Fprintln(out, "\nModes:\n"+modesHelp)
		if r.run != nil {
			_, _ = fmt.Fprintln(out, "\nShell commands: n, p, page <n>, sort <key>, last, clear, exit, quit")
		}
	}
	printCommands := func(title, group string) {
		shown := false
		for _, sub := range c.Commands() {
			if sub.Hidden || (group != "" && sub.GroupID != group) {
				continue
			}
			if !shown {
				_, _ = fmt.Fprintln(out, "\n"+title+":")
				shown = true
			}
			_, _ = fmt.Fprintf(out, "  %-18s %s\n", sub.Name(), sub.Short)
		}
	}
	if c == c.Root() {
		for _, g := range c.Groups() {
			printCommands(g.Title, g.ID)
		}
	} else {
		printCommands("Available Commands", "")
	}
	if c.Example != "" {
		_, _ = fmt.Fprintln(out, "\nExamples:\n"+c.Example)
	}
	local := pflag.NewFlagSet("local", pflag.ContinueOnError)
	c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) { local.AddFlag(f) })
	if local.HasAvailableFlags() {
		_, _ = fmt.Fprintln(out, "\nFlags:\n"+local.FlagUsages())
	}
	global := c.Root().PersistentFlags()
	if global.HasAvailableFlags() {
		_, _ = fmt.Fprintln(out, "Global Flags:\n"+global.FlagUsages())
	}
}
