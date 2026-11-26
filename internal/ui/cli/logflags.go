package cli

import (
	"context"
	"slices"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

// logValueAnnotation marks a flag whose value the session log header may show (logs.md section 2,
// TM-17). Every other value is redacted: paths, addresses and anything added later.
const logValueAnnotation = "mothdex.log_value"

// logValues annotates the named flags of fs as safe to log. A wrong name is a programming error
// that the tests catch.
func logValues(fs *pflag.FlagSet, names ...string) {
	for _, name := range names {
		if err := fs.SetAnnotation(name, logValueAnnotation, []string{"true"}); err != nil {
			panic(err)
		}
	}
}

// LogFlagRules are the flag rules of the whole command tree, for the session header. A name or
// letter that two commands share is loggable only when it is in both.
func LogFlagRules() map[string]mlog.FlagRule {
	root := (&runtime{ctx: context.Background()}).rootCommand()
	rules := map[string]mlog.FlagRule{}
	add := func(f *pflag.Flag) {
		rule := mlog.FlagRule{TakesValue: f.NoOptDefVal == "", Log: f.Annotations[logValueAnnotation] != nil}
		for _, key := range []string{f.Name, f.Shorthand} {
			if key == "" {
				continue
			}
			merged := rule
			if old, ok := rules[key]; ok {
				merged = mlog.FlagRule{TakesValue: old.TakesValue || rule.TakesValue, Log: old.Log && rule.Log}
			}
			rules[key] = merged
		}
	}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(add)
		c.PersistentFlags().VisitAll(add)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return rules
}

// unlogged are the commands that keep no session: they run on every Tab, as the check of an
// upgrade or to read the logs, and would push real sessions out of the retention (logs.md
// section 1). Their errors still reach mothdex.log.
var unlogged = []string{cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd, "completion", "version", "logs"}

// Logged says whether a command line gets a session log.
func Logged(args []string, rules map[string]mlog.FlagRule) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" || a[0] != '-' {
			// upgrade shadows the global boolean flag with its target version.
			return (a == "upgrade" || a == "update") || (!slices.Contains(unlogged, a) && !slices.Contains(args, "--version"))
		}
		name := a[1:]
		if len(name) > 0 && name[0] == '-' {
			name = name[1:]
		}
		if rules[name].TakesValue {
			i++ // the value of "--lang pt_br" is not the command
		}
	}
	return !slices.Contains(args, "--version")
}
