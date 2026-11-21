package gui

import (
	"context"
	"strconv"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
)

// confirmBytes is the size above which one file also asks when interface.confirm_downloads is on
// (cli-conventions section 6, docs/ux/gui.md section 5.7).
const confirmBytes = 500 << 20

// doneKey identifies a downloaded file: the entry and the link number.
func doneKey(slug string, link int) string { return slug + "|" + strconv.Itoa(link) }

// isDownloaded reports whether the history has a finished download of this entry and link.
func (m Model) isDownloaded(slug string, link int) bool { return m.downloaded[doneKey(slug, link)] }

// slugDownloaded reports whether any link of the entry was downloaded (the mark of a Results row).
func (m Model) slugDownloaded(slug string) bool {
	for k := range m.downloaded {
		if len(k) > len(slug) && k[:len(slug)] == slug && k[len(slug)] == '|' {
			return true
		}
	}
	return false
}

type (
	historyMsg struct {
		records []app.HistoryRecord
		err     error
	}
	// enqueuedMsg is the answer of Enqueue for one request.
	enqueuedMsg struct {
		err   error
		title string
		jobs  []app.JobSnapshot
	}
)

func historyCmd(svc Service) Cmd {
	return func() Msg {
		ctx, cancel := context.WithTimeout(context.Background(), dbInfoTimeout)
		defer cancel()
		recs, err := svc.History(ctx, app.HistoryFilter{})
		return historyMsg{records: recs, err: err}
	}
}

func (m Model) onHistory(msg historyMsg) Model {
	if msg.err != nil {
		m.o.Log.Warn("gui: History", "err", msg.err)
		return m
	}
	marks := map[string]bool{}
	for _, r := range msg.records {
		if r.State == string(app.StateDone) {
			marks[doneKey(r.Slug, r.LinkIndex)] = true
		}
	}
	m.downloaded = marks
	return m
}

// markDone records a download that finished in this run.
func (m Model) markDone(slug string, link int) Model {
	marks := make(map[string]bool, len(m.downloaded)+1)
	for k, v := range m.downloaded {
		marks[k] = v
	}
	marks[doneKey(slug, link)] = true
	m.downloaded = marks
	return m
}

// download is what a person asks for: an entry, a link (0 for the default ranked one) and the size the
// confirmation rule looks at.
type download struct {
	slug, title, platform string
	link                  int // 0: the default link, chosen by the preference
	size                  int64
}

func (d download) request(prefer app.Preference) app.DownloadRequest {
	sel := app.LinkSelector{Prefer: prefer}
	if d.link > 0 {
		sel = app.LinkSelector{Index: d.link}
	}
	return app.DownloadRequest{Ref: app.Ref{Slug: d.slug}, Link: sel}
}

// requestDownload queues a download, asking first when interface.confirm_downloads is on and the file is above
// 500 MB (Download is the default there, since the person just pressed download).
func (m Model) requestDownload(d download, now time.Time) (Model, []Cmd) {
	if m.o.Confirm && d.size > confirmBytes {
		dir := m.destination(d.platform)
		return m.openDialog(dialogState{
			kind: dialogConfirmDownload, pending: d, initial: 1,
			title: m.tr("ui.gui.confirm.download", map[string]any{"title": app.SanitizeText(d.title), "size": humanSize(d.size), "dir": dir}),
			buttons: []dialogButton{
				{btnCancel, m.tr("ui.gui.dialog.cancel", nil)},
				{btnDownload, m.tr("ui.gui.dialog.download", nil)},
			},
		}, now), nil
	}
	return m.enqueue(d, now)
}

// destination is where a platform's downloads go, for the confirmation text.
func (m Model) destination(platform string) string {
	if dir, err := m.profile.Profile.DestDir(platform); err == nil && dir != "" {
		return app.SanitizeText(dir)
	}
	return app.SanitizeText(m.profile.RomRoot)
}

// enqueue sends the request to the service off the render thread. It runs on its own context: leaving a screen
// never cancels a download.
func (m Model) enqueue(d download, now time.Time) (Model, []Cmd) {
	svc, req := m.o.Service, d.request(m.o.Prefer)
	return m, []Cmd{func() Msg {
		ids, err := svc.Enqueue(context.Background(), []app.DownloadRequest{req})
		if err != nil {
			return enqueuedMsg{err: err, title: d.title}
		}
		want := map[app.JobID]bool{}
		for _, id := range ids {
			want[id] = true
		}
		var jobs []app.JobSnapshot
		for _, j := range svc.Jobs() {
			if want[j.ID] {
				jobs = append(jobs, j)
			}
		}
		return enqueuedMsg{title: d.title, jobs: jobs}
	}}
}

func (m Model) onEnqueued(msg enqueuedMsg, now time.Time) Model {
	if msg.err != nil {
		f := m.failureOf(msg.err)
		return m.withToast(f.msg, toastError, now)
	}
	for _, j := range msg.jobs {
		m.jobs, _, _ = m.jobs.apply(j)
		m = m.noteJob(j, now)
	}
	link := 1
	if len(msg.jobs) > 0 {
		link = max(msg.jobs[0].LinkIndex, 1)
	}
	return m.withToast(m.tr("ui.tui.queued_one", i18n.Args{"title": app.SanitizeText(msg.title), "link": link}), toastOK, now)
}
