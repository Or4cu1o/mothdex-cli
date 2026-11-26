package cli

import (
	"fmt"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/helpreference"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func referenceTopics(t *i18n.Translator) []HelpTopic {
	if t == nil {
		b, _ := i18n.Load(locales.FS)
		t = b.New(i18n.Fallback)
	}
	return []HelpTopic{
		{Name: "exit-codes", Short: "Process exit codes", Text: func() string { return t.T("ui.cli.template.exit_codes", nil) }},
		{Name: "config", Short: "Settings, defaults and environment variables", Text: func() string { return configHelp(t) }},
		{Name: "errors", Short: "Error codes and recovery", Text: func() string {
			b, err := i18n.Load(locales.FS)
			if err != nil {
				return err.Error()
			}
			return helpreference.Errors(b, t.Locale())
		}},
		logsTopic(t),
	}
}

func configHelp(t *i18n.Translator) string {
	defaults, err := config.Load(config.Options{})
	if err != nil {
		return err.Error()
	}
	var out strings.Builder
	out.WriteString(t.T("ui.cli.template.config_intro", nil) + "\n\n" + t.T("ui.cli.template.config_columns", nil) + "\n")
	for _, spec := range config.Specs() {
		value, err := defaults.Get(spec.Key)
		if err != nil {
			continue
		}
		accepted := spec.Kind
		if len(spec.Enum) > 0 {
			accepted = strings.Join(spec.Enum, ", ")
		}
		if spec.Min != "" || spec.Max != "" {
			accepted += " (" + spec.Min + ".." + spec.Max + ")"
		}
		fmt.Fprintf(&out, "%s | %s | %v | %s\n", spec.Key, accepted, value.Value, spec.Env)
	}
	out.WriteString(t.T("ui.cli.template.config_footer", nil) + "\n")
	return out.String()
}
