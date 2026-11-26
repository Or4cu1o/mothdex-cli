package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/update"
)

func (r *runtime) startUpdateCheck(s *Session) {
	if s.Updater == nil {
		return
	}
	skip := ""
	switch {
	case r.g.JSON:
		skip = "json"
	case r.g.Quiet:
		skip = "quiet"
	case !r.isTTY(r.d.Stderr):
		skip = "non_tty"
	case r.g.Version || r.cmd == "upgrade" || r.cmd == "version" || r.cmd == "completion" || r.cmd == "logs" || strings.HasPrefix(r.cmd, "__complete"):
		skip = "command"
	}
	if skip != "" {
		s.Updater.LogSkipped(skip)
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, 3*time.Second)
	r.updateCancel = cancel
	results := make(chan update.CheckResult, 1)
	r.updateResult = results
	go func() {
		if reason := s.Updater.SkipReason(s.UpdateCheck, r.d.Getenv); reason != "" {
			s.Updater.LogSkipped(reason)
			return
		}
		result, err := s.Updater.Check(ctx, false)
		if err == nil {
			results <- result
		}
	}()
}
func (r *runtime) finishUpdateCheck() {
	if r.updateCancel != nil {
		defer r.updateCancel()
	}
	if r.g.JSON || r.g.Quiet || r.g.Shell || !r.isTTY(r.d.Stderr) || r.sess == nil || r.sess.Updater == nil {
		return
	}
	select {
	case result := <-r.updateResult:
		if result.Notify {
			r.printer().info("%s", r.printer().T("update.notice", i18n.Args(result.NoticeArgs(r.programName()))))
			r.sess.Updater.MarkNotified(result.Latest)
		}
	default:
	}
}
func (r *runtime) cachedUpdateNotice(s *Session) string {
	if s.Updater == nil || r.g.JSON || r.g.Quiet || !r.isTTY(r.d.Stderr) || s.Updater.SkipReason(s.UpdateCheck, r.d.Getenv) != "" {
		return ""
	}
	result := s.Updater.Cached()
	if !result.Notify {
		return ""
	}
	notice := s.T.T("update.notice", i18n.Args(result.NoticeArgs(r.programName())))
	s.Updater.MarkNotified(result.Latest)
	return notice
}
func (r *runtime) programName() string {
	if r.d.ProgramName != "" {
		return r.d.ProgramName
	}
	return os.Args[0]
}
