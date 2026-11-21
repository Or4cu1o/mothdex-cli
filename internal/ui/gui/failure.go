package gui

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

var unfilled = regexp.MustCompile(`\{[a-z_]+\}`)

// failure is an error ready to draw in place (docs/ux/gui.md section 6): the localized message, the hint for a
// person with a pad, the stable code for support, and whether a retry can help.
type failure struct {
	msg, hint, code string
	retryable       bool
	retryAfter      string // for rate_limited: how long to wait, as text
	network         bool
}

// failureOf localizes an error from the service the way the CLI does (errors.<code>.message), with the hint
// of ui.gui.hints.<code> where the CLI hint names a flag or command a person on a couch cannot type, and
// errors.<code>.hint otherwise. A message or hint with a placeholder that has no value falls back to the
// service text. Server text is cleaned.
func (m Model) failureOf(err error) failure {
	var ae *app.Error
	if !errors.As(err, &ae) {
		ae = &app.Error{Code: app.CodeInternal, Message: "unexpected error", Cause: err}
	}
	args := i18n.Args{"detail": ae.Message, "log_path": m.o.LogPath}
	for k, v := range ae.Args {
		args[k] = v
	}
	msg := ae.Message
	if key := "errors." + ae.Code + ".message"; m.o.T.Has(key) {
		if text := m.o.T.T(key, args); !unfilled.MatchString(text) {
			msg = text
		}
	}
	hint := ""
	for _, key := range []string{"ui.gui.hints." + ae.Code, "errors." + ae.Code + ".hint"} {
		if !m.o.T.Has(key) {
			continue
		}
		if text := m.o.T.T(key, args); !unfilled.MatchString(text) {
			hint = text
			break
		}
	}
	f := failure{
		msg: app.SanitizeText(msg), hint: app.SanitizeText(hint), code: ae.Code,
		retryable: ae.Retryable || ae.Code == app.CodeNetwork || ae.Code == app.CodeUpstream,
		network:   ae.Code == app.CodeNetwork,
	}
	if ae.Code == app.CodeRateLimited {
		f.retryAfter = strings.TrimSpace(app.SanitizeText(stringArg(ae.Args["retry_after"])))
	}
	return f
}

func stringArg(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// canceled reports whether an error is only a canceled request (the person left the screen).
func canceled(err error) bool { return errors.Is(err, context.Canceled) }
