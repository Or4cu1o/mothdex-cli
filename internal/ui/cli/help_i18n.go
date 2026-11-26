package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

// Help is resolved without opening the app service: it works offline and with an
// invalid configuration, and does not create a log or a downloader.
func (r *runtime) helpTranslator() *i18n.Translator {
	if r.run != nil && r.run.s.T != nil {
		return r.run.s.T
	}
	lang := r.g.Lang
	if lang == "" {
		if platform, err := config.CurrentPlatform(); err == nil {
			if file, err := config.FilePath(platform, r.g.Config); err == nil {
				if result, err := config.Load(config.Options{File: file, Environ: os.Environ(), Force: true}); err == nil {
					lang = result.Config.Interface.Language
				}
			}
		}
	}
	if r.d.Translator != nil {
		if t := r.d.Translator(lang); t != nil {
			return t
		}
	}
	b, _ := i18n.Load(locales.FS) // Embedded en_us is required and checked in locale tests.
	return b.New(b.Resolve(lang, os.Getenv, i18n.SystemLanguages))
}

func localizeHelp(root *cobra.Command, t *i18n.Translator) {
	walkCommands(root, func(c *cobra.Command) {
		prefix := "ui.cli.help." + helpID(c) + "."
		if t.Has(prefix + "short") {
			c.Short = t.T(prefix+"short", nil)
		}
		if t.Has(prefix + "long") {
			c.Long = t.T(prefix+"long", nil)
		}
		if t.Has(prefix + "example") {
			c.Example = t.T(prefix+"example", nil)
		}
		translate := func(f *pflag.Flag) {
			name := strings.ReplaceAll(f.Name, "-", "_")
			key := prefix + "flags." + name
			if !t.Has(key) {
				key = "ui.cli.flags." + name
			}
			if t.Has(key) {
				f.Usage = t.T(key, nil)
			}
		}
		c.Flags().VisitAll(translate)
		c.PersistentFlags().VisitAll(translate)
	})
	for _, group := range root.Groups() {
		group.Title = t.T("ui.cli.template."+group.ID, nil)
	}
}

// Cobra delays flag parsing for __complete until its own Run. Read just the locale
// inputs here, excluding the unfinished last word, before descriptions are collected.
func (r *runtime) localizeCompletion(c *cobra.Command, args []string) {
	flags := pflag.NewFlagSet("completion-language", pflag.ContinueOnError)
	flags.ParseErrorsWhitelist.UnknownFlags = true
	flags.StringVar(&r.g.Lang, "lang", r.g.Lang, "")
	flags.StringVar(&r.g.Config, "config", r.g.Config, "")
	if len(args) > 0 {
		_ = flags.Parse(args[:len(args)-1])
	}
	localizeHelp(c.Root(), r.helpTranslator())
}
