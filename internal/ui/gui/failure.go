package gui

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

var unfilled = regexp.MustCompile(`\{[a-z_]+\}`)

// failure is an error ready to draw in place (docs/ux/gui.md section 6): the localized message, the hint for a
// person with a pad, the stable code for support, and whether a retry can help.
type failure struct {
	msg, hint, code string
	ref, log        string // what finds this occurrence in the log, and that log: set by report
	retryable       bool
	retryAfter      string // for rate_limited: how long to wait, as text
	network         bool
}

// report is failureOf for an error that is about to be shown: the occurrence gets a ref, one line in the log
// that carries it, and the path of that log (docs/contracts/cli/logs.md section 5). Call it once per occurrence,
// in Update, never while drawing. cmd says what was being done.
func (m Model) report(err error, cmd string) failure {
	path := m.logPath()
	f := m.failureWith(err, path)
	f.ref, f.log = mlog.NewRef(), path
	var ae *app.Error
	message := "unexpected error"
	if errors.As(err, &ae) {
		message = ae.Message
	}
	mlog.RecordError(m.o.Log, mlog.ErrorRecord{Ref: f.ref, Code: f.code, Cmd: cmd, Message: message, Cause: err})
	return f
}

// reportOr is report for an error that may only be the person leaving the screen, which is not shown as one.
func (m Model) reportOr(err error, cmd string) failure {
	if canceled(err) {
		return m.failureOf(err)
	}
	return m.report(err, cmd)
}

func (m Model) logPath() string {
	if m.o.LogPath == nil {
		return ""
	}
	return app.SanitizeText(m.o.LogPath())
}

// short is the failure on one line, for a toast or a row: what finds it in the log comes first, so a cut never
// takes it.
func (f failure) short() string { return f.tagged(f.msg) }

// tagged puts the code and the ref in front of a text.
func (f failure) tagged(text string) string {
	if f.ref == "" {
		return text
	}
	return f.code + " · " + f.ref + " · " + text
}

// failureOf localizes an error without recording it: for a line that is drawn again and again from the same
// error, whose occurrence report already recorded.
func (m Model) failureOf(err error) failure { return m.failureWith(err, "") }

// failureWith localizes an error from the service the way the CLI does (Args message_key, or else
// errors.<code>.message), with the hint of ui.gui.hints.<code> where the CLI hint names a flag or command a person
// on a couch cannot type, and errors.<code>.hint otherwise. A message or hint with a placeholder that has no value
// falls back to the next one, the message last to the service text. Server text is cleaned.
func (m Model) failureWith(err error, logPath string) failure {
	var ae *app.Error
	if !errors.As(err, &ae) {
		ae = &app.Error{Code: app.CodeInternal, Message: "unexpected error", Cause: err}
	}
	args := i18n.Args{"detail": ae.Message}
	if logPath != "" {
		args["log_path"] = logPath
	}
	for k, v := range ae.Args {
		args[k] = v
	}
	named, _ := ae.Args["message_key"].(string) // a message more precise than the one of the code
	msg := cmp.Or(m.firstText(args, named, "errors."+ae.Code+".message"), ae.Message)
	hint := m.firstText(args, "ui.gui.hints."+ae.Code, "errors."+ae.Code+".hint")
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

// firstText is the text of the first key that exists and has every value it asks for, or "".
func (m Model) firstText(args i18n.Args, keys ...string) string {
	for _, key := range keys {
		if !m.o.T.Has(key) {
			continue
		}
		if text := m.o.T.T(key, args); !unfilled.MatchString(text) {
			return text
		}
	}
	return ""
}

func stringArg(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// canceled reports whether an error is only a canceled request (the person left the screen).
func canceled(err error) bool { return errors.Is(err, context.Canceled) }
