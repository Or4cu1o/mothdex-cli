//go:build gui

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	mlog "github.com/Or4cu1o/mothdex-cli/internal/log"
)

// The session log of a run (docs/contracts/cli/logs.md). It exists for a run that opens the GUI, or tries to:
// --probe, --version, --spike and --help write no file.

// loggedFlags are the flags whose value the session header may show. Any other value, a path above all, is
// redacted (T-110 TM-17).
var loggedFlags = map[string]bool{"profile": true, "lang": true, "log-level": true, "window": true, "driver": true}

// startLog starts the log of this process. It touches no file until the configuration says where, or Close
// writes what it kept to the default folder.
func startLog(args []string) *mlog.Session {
	logs := mlog.StartSession(mlog.SessionOptions{Binary: "gui", Args: longForm(args), Flags: logFlagRules(), Build: buildinfo.Get()})
	logs.Update(mlog.Info{Mode: "gui"})
	return logs
}

func logFlagRules() map[string]mlog.FlagRule {
	var o options
	rules := map[string]mlog.FlagRule{}
	newFlags(&o).VisitAll(func(f *flag.Flag) {
		b, isBool := f.Value.(interface{ IsBoolFlag() bool })
		rules[f.Name] = mlog.FlagRule{TakesValue: !isBool || !b.IsBoolFlag(), Log: loggedFlags[f.Name]}
	})
	return rules
}

// longForm writes every flag with two dashes. The flag package takes one dash or two, but the header's mask
// reads "-config" as the letters c, o, n... and would then take the path after it for a plain word.
func longForm(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
			a = "-" + a
		}
		out[i] = a
	}
	return out
}

// recordError writes the line of an error that stops the program and returns its ref.
func recordError(l *slog.Logger, code, message string, cause error) string {
	ref := mlog.NewRef()
	mlog.RecordError(l, mlog.ErrorRecord{Ref: ref, Code: code, Cmd: "gui", Message: message, Cause: cause})
	return ref
}

// appError is the code and the English message of an error of the composition root.
func appError(err error) (code, message string) {
	var ae *app.Error
	if errors.As(err, &ae) {
		return ae.Code, ae.Message
	}
	return app.CodeInternal, "cannot start"
}

// support writes what a tester sends for an error: the code, the ref and the log that holds them.
func support(w io.Writer, tr *i18n.Translator, code, ref, logPath string) {
	_, _ = fmt.Fprintf(w, "  %s\n", tr.T("ui.gui.error.code_ref", i18n.Args{"code": code, "ref": ref}))
	if logPath != "" {
		_, _ = fmt.Fprintf(w, "  %s\n", tr.T("ui.gui.error.log", i18n.Args{"path": mlog.DisplayPath(logPath)}))
	}
}

// reportPanic records a recovered panic: one ERROR line with the ref and the stack, and the crash file. Both
// go through the session's redactor, which is why the value is never printed anywhere else (TM-27).
func reportPanic(logs *mlog.Session, recovered any, stack []byte) (ref string) {
	ref = mlog.NewRef()
	log := logs.Logger()
	log.Error("gui: internal error", "ref", ref, "code", app.CodeInternal, "cmd", "gui", "panic", fmt.Sprint(recovered), "stack", string(stack))
	if _, err := logs.WriteCrash(recovered, stack, ref); err != nil {
		log.Warn("gui: the crash file was not written", "err", err)
	}
	return ref
}

// exportLogs is the "Export logs" action: the bundle `mothdex logs bundle` writes, from local files only.
func exportLogs(logs *mlog.Session, options func() mlog.BundleOptions) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		res, err := mlog.Bundle(ctx, options())
		if err != nil {
			return "", fmt.Errorf("log bundle: %w", err)
		}
		logs.Logger().Info("gui: logs exported", "bytes", res.Bytes, "sessions", len(res.Sessions))
		return mlog.DisplayPath(res.Path), nil
	}
}
