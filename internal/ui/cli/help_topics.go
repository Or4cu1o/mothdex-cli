package cli

import (
	"fmt"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/helpreference"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func referenceTopics() []HelpTopic {
	return []HelpTopic{
		{Name: "exit-codes", Short: "Process exit codes", Text: func() string { return exitCodesHelp }},
		{Name: "config", Short: "Settings, defaults and environment variables", Text: configHelp},
		{Name: "errors", Short: "Error codes and recovery", Text: func() string {
			b, err := i18n.Load(locales.FS)
			if err != nil {
				return err.Error()
			}
			return helpreference.Errors(b, i18n.Fallback)
		}},
	}
}

// Mirrors docs/contracts/cli/cli-output.md section 4. Keep the reference in sync when
// adding error families; the coverage test checks every documented code.
const exitCodesHelp = `Exit  Meaning                         error.code
0     Success (including skipped)     —
1     Unexpected internal error       internal
2     Usage or configuration          usage, invalid_config, invalid_request, confirmation_required, unmapped_platform
3     Not found                       not_found, no_session, index_out_of_range
4     Network/service unavailable     network, rate_limited, upstream, api_incompatible, untrusted_url, untrusted_redirect
5     Integrity                       size_mismatch, integrity
6     Local filesystem                permission_denied, disk_full, exists, path_rejected, unsafe_name
7     Partial batch                   First failure; all failures in data.jobs[].error
130   Interrupted (wins over 7)        canceled`

func configHelp() string {
	defaults, err := config.Load(config.Options{})
	if err != nil {
		return err.Error()
	}
	var out strings.Builder
	out.WriteString("Configuration: defaults < YAML file < MOTHDEX_* environment < command flags.\nUse mothdex config set <key> <value> to save an override.\n\nKey | Accepted values | Default | Environment\n")
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
	out.WriteString("profile.folders.<platform> | relative folder name | profile default | —\nEmpty paths use platform-specific directories. interface.language accepts auto or a locale such as en_us/pt_br.\n")
	return out.String()
}
