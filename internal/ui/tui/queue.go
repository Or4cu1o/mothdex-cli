package tui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
)

const (
	barWidth      = 24               // cells of a progress bar (cli-conventions section 5)
	etaAfter      = 3 * time.Second  // the ETA appears after this long a transfer
	warmHintAfter = 30 * time.Second // the Warming hint appears after this long
)

// queueScreen is the download queue (tui.md section 5): the jobs of this run, live.
type queueScreen struct {
	cur    app.JobID
	hidden map[app.JobID]bool // finished jobs hidden from view with c; history keeps them
	detail bool
	askAll bool // the [y/N] question of X
	ask    *consentAsk
}

// consentAsk is the [y/N] question of "Download anyway": one failed job whose real size needs the
// person's OK, and the request that would go again with that consent (tui.md section 5).
type consentAsk struct {
	job app.JobSnapshot
	req app.DownloadRequest
}

func newQueue() queueScreen { return queueScreen{hidden: map[app.JobID]bool{}} }

func (queueScreen) id() screenID                     { return scQueue }
func (queueScreen) loading() bool                    { return false }
func (s queueScreen) init(*shared) (screen, tea.Cmd) { return s, nil }
func (queueScreen) title(rt *shared) string          { return rt.T("ui.tui.screen.queue", nil) }

func (s queueScreen) focus() focusKind {
	if s.askAll || s.ask != nil {
		return focusPrompt
	}
	return focusNone
}

// split returns the jobs that are running or waiting in the order they started, and the finished
// ones that are not hidden, newest first.
func (s queueScreen) split(book jobBook) (active, finished []app.JobSnapshot) {
	for _, j := range book.list() {
		switch {
		case !j.Terminal():
			active = append(active, j)
		case !s.hidden[j.ID]:
			finished = append(finished, j)
		}
	}
	slices.Reverse(finished)
	return active, finished
}

// nav is every job the cursor can stand on, in screen order.
func (s queueScreen) nav(book jobBook) []app.JobSnapshot {
	active, finished := s.split(book)
	return append(active, finished...)
}

// at is the position of the cursor in nav, falling back to the first job.
func (s queueScreen) at(nav []app.JobSnapshot) int {
	for i, j := range nav {
		if j.ID == s.cur {
			return i
		}
	}
	return 0
}

func (s queueScreen) update(msg tea.Msg, rt *shared) (screen, tea.Cmd) {
	if r, ok := msg.(retriedMsg); ok { // a failed retry puts the jobs back in the list
		if r.enq.err != nil {
			hidden := make(map[app.JobID]bool, len(s.hidden))
			for id := range s.hidden {
				hidden[id] = true
			}
			for _, id := range r.ids {
				delete(hidden, id)
			}
			s.hidden = hidden
		}
		return s, nil
	}
	ev, ok := msg.(keyEv)
	if !ok {
		return s, nil
	}
	if s.askAll {
		s.askAll = false
		if ev.a == actYes {
			return s, cancelCmd(rt.env, rt.book.list())
		}
		return s, nil
	}
	if ask := s.ask; ask != nil {
		s.ask = nil
		if ev.a == actYes {
			return s.requeue(rt, []app.DownloadRequest{consent.WithConsent(ask.req, ask.job)}, []app.JobID{ask.job.ID})
		}
		return s, nil
	}
	nav := s.nav(rt.book)
	i := s.at(nav)
	switch ev.a {
	case actBack:
		if s.detail {
			s.detail = false
			return s, nil
		}
		return s, popCmd()
	case actUp, actDown, actFirst, actLast:
		s.detail = false
		if len(nav) > 0 {
			s.cur = nav[moveIndex(ev.a, i, len(nav))].ID
		}
	case actCancel:
		if len(nav) > 0 && !nav[i].Terminal() {
			return s, cancelCmd(rt.env, nav[i:i+1])
		}
	case actCancelAll:
		if rt.book.busy() > 0 {
			s.askAll = true
		}
	case actRetry, actDownload: // d is the same as r on a job that waits for the person's OK
		if len(nav) > 0 && consent.Needed(nav[i]) {
			if req, ok := rt.book.reqs[nav[i].ID]; ok {
				s.ask = &consentAsk{job: nav[i], req: req}
			}
			return s, nil
		}
		if ev.a == actDownload {
			return s, nil
		}
		var cmd tea.Cmd
		s, cmd = s.retry(rt)
		return s, cmd
	case actClearFinished:
		s.hidden = hideFinished(s.hidden, rt.book)
	case actEnter:
		if len(nav) > 0 && nav[i].Terminal() {
			s.cur, s.detail = nav[i].ID, !s.detail
		}
	case actCopy:
		if s.detail && len(nav) > 0 && nav[i].Path != "" {
			return s, tea.Batch(tea.SetClipboard(nav[i].Path), toastCmd(rt.T("ui.tui.queue.copied", nil), toastInfo))
		}
	}
	return s, nil
}

// moveIndex is the cursor position after a movement key.
func moveIndex(a action, i, n int) int {
	switch a {
	case actUp:
		i--
	case actDown:
		i++
	case actFirst:
		i = 0
	case actLast:
		i = n - 1
	}
	return max(0, min(i, n-1))
}

func hideFinished(hidden map[app.JobID]bool, book jobBook) map[app.JobID]bool {
	out := make(map[app.JobID]bool, len(hidden))
	for id := range hidden {
		out[id] = true
	}
	for _, j := range book.list() {
		if j.Terminal() {
			out[j.ID] = true
		}
	}
	return out
}

// cancelCmd cancels jobs that have not finished; the final state arrives as an event.
func cancelCmd(e *env, jobs []app.JobSnapshot) tea.Cmd {
	ids := make([]app.JobID, 0, len(jobs))
	for _, j := range jobs {
		if !j.Terminal() {
			ids = append(ids, j.ID)
		}
	}
	return func() tea.Msg {
		for _, id := range ids {
			_ = e.svc.Cancel(id)
		}
		return nil
	}
}

// retry queues the original request of every failed job again and hides those jobs at once, so a
// second r cannot queue the same files twice. A job that waits for the person's OK to its real size is
// left alone: it needs its own answer (r on that row). If Enqueue fails they come back (retriedMsg).
// There is no new service method: it is Enqueue once more (tui.md section 5).
func (s queueScreen) retry(rt *shared) (queueScreen, tea.Cmd) {
	var reqs []app.DownloadRequest
	var ids []app.JobID
	for _, j := range rt.book.list() {
		if j.State != app.StateFailed || s.hidden[j.ID] || consent.Needed(j) {
			continue
		}
		if r, ok := rt.book.reqs[j.ID]; ok {
			reqs, ids = append(reqs, r), append(ids, j.ID)
		}
	}
	return s.requeue(rt, reqs, ids)
}

// requeue queues the requests again and hides the jobs they came from; if Enqueue fails they come back.
func (s queueScreen) requeue(rt *shared, reqs []app.DownloadRequest, ids []app.JobID) (queueScreen, tea.Cmd) {
	if len(reqs) == 0 {
		return s, nil
	}
	hidden := make(map[app.JobID]bool, len(s.hidden)+len(ids))
	for id := range s.hidden {
		hidden[id] = true
	}
	for _, id := range ids {
		hidden[id] = true
	}
	s.hidden = hidden
	enqueue := enqueueCmd(rt.env, reqs)
	return s, func() tea.Msg {
		enq, _ := enqueue().(enqueuedMsg)
		return retriedMsg{enq: enq, ids: ids}
	}
}

// progressBar draws bytes of expected as a bar of width cells and the percentage.
func progressBar(width int, bytes, expected int64, g glyphs) (bar string, pct int) {
	if expected <= 0 {
		return strings.Repeat(g.barEmpty, width), 0
	}
	pct = int(min(max(bytes*100/expected, 0), 100))
	full := pct * width / 100
	return strings.Repeat(g.bar, full) + strings.Repeat(g.barEmpty, width-full), pct
}

// eta is the time left at the current rate, or --:-- before there is a rate worth trusting.
func eta(j app.JobSnapshot, since, now time.Time) string {
	if j.RateBps <= 0 || j.Expected <= 0 || since.IsZero() || now.Sub(since) < etaAfter {
		return "--:--"
	}
	left := max(j.Expected-j.Bytes, 0)
	return clock(time.Duration(left/j.RateBps) * time.Second)
}

func jobLabel(j app.JobSnapshot) string {
	for _, s := range []string{j.Name, j.Title, j.Slug} {
		if s != "" {
			return clean(s)
		}
	}
	return string(j.ID)
}

// stateLines are the lines under a running or waiting job, by state (cli-conventions section 5).
func (s queueScreen) stateLines(rt *shared, j app.JobSnapshot) []string {
	st := rt.st
	switch j.State {
	case app.StateQueued:
		return []string{"  " + render(st.dim, rt.T("ui.status.queued", nil))}
	case app.StatePreflight, app.StateConnecting:
		return []string{"  " + rt.T("ui.cli.download.connecting", nil)}
	case app.StateWarming:
		since := rt.book.warmSince[j.ID]
		line := "  " + render(st.warn, rt.T("ui.cli.download.warming", nil)+" "+clock(rt.now().Sub(since)))
		if rt.now().Sub(since) >= warmHintAfter {
			return []string{line, "  " + render(st.dim, rt.T("ui.tui.queue.warming_hint", nil))}
		}
		return []string{line}
	case app.StateRetryWait:
		return []string{"  " + render(st.warn, rt.T("ui.cli.download.retry", i18n.Args{"attempt": j.Attempt}))}
	case app.StateVerifying:
		return []string{"  " + rt.T("ui.cli.download.verifying", nil)}
	case app.StateFinalizing:
		return []string{"  " + rt.T("ui.cli.download.saving", nil)}
	}
	bar, pct := progressBar(barWidth, j.Bytes, j.Expected, st.g)
	line := "  " + render(st.accent, bar) + "  " + padLeft(strconv.Itoa(pct)+"%", 4) + "  " + humanSize(j.Bytes) + " / " + humanSize(j.Expected)
	if j.RateBps > 0 {
		line += "  " + humanSize(j.RateBps) + "/s"
	}
	return []string{line + "  eta " + eta(j, rt.book.xferSince[j.ID], rt.now())}
}

// finishedLine is the one line of a finished job.
func (s queueScreen) finishedLine(rt *shared, j app.JobSnapshot) string {
	st := rt.st
	label := jobLabel(j)
	switch j.State {
	case app.StateDone:
		saved := rt.T("ui.cli.download.saved", i18n.Args{"path": clean(j.Path), "size": humanSize(j.Bytes), "time": clock(j.Finished.Sub(j.Started))})
		return render(st.ok, st.g.ok+" "+label) + "  " + render(st.dim, saved)
	case app.StateSkipped:
		return render(st.dim, rt.T("ui.cli.download.skipped", i18n.Args{"path": clean(j.Path)}))
	}
	if j.Err == nil {
		return render(st.bad, st.g.fail+" "+label+": "+rt.T("errors.canceled.message", nil))
	}
	if consent.Needed(j) { // nothing failed: the job waits for a person (warn and !, not error and the cross)
		return render(st.warn, st.g.warn+" "+label+": "+consentRow(rt, j))
	}
	f := rt.failureOf(j.Err)
	return render(st.bad, st.g.fail+" "+label+": "+f.msg+" ["+f.code+"]")
}

func (s queueScreen) view(rt *shared, w, h int) screenView {
	nav := s.nav(rt.book)
	if len(nav) == 0 {
		return screenView{body: []string{render(rt.st.dim, rt.T("ui.tui.queue.empty", nil))}}
	}
	i := s.at(nav)
	if s.detail && nav[i].Terminal() {
		return s.detailView(rt, nav[i])
	}
	active, finished := s.split(rt.book)
	var lines []string
	curLine := 0
	mark := func(j app.JobSnapshot, text string) string {
		if j.ID == nav[i].ID {
			curLine = len(lines)
			return render(rt.st.on, ">"+fit(text, max(w-1, 1)))
		}
		return " " + text
	}
	for _, j := range active {
		lines = append(lines, mark(j, jobLabel(j)))
		lines = append(lines, s.stateLines(rt, j)...)
	}
	if len(finished) > 0 {
		lines = append(lines, render(rt.st.dim, rt.st.g.h+rt.st.g.h+" "+rt.T("ui.tui.queue.finished", i18n.Args{"count": len(finished)})+" "+strings.Repeat(rt.st.g.h, max(w-24, 2))))
		for _, j := range finished {
			lines = append(lines, mark(j, s.finishedLine(rt, j)))
		}
	}
	top := 0
	if curLine >= h {
		top = curLine - h + 1
	}
	if top > len(lines) {
		top = 0
	}
	sv := screenView{body: lines[top:]}
	switch {
	case s.askAll:
		sv.status, sv.prompt = render(rt.st.warn, rt.T("ui.tui.queue.cancel_all_ask", nil)), true
	case s.ask != nil:
		sv.status, sv.prompt = render(rt.st.warn, consentAskText(rt, s.ask.job)), true
	}
	return sv
}

// detailView is the panel of a finished job: file, path, SHA-256, attempts, duration (tui.md section 5).
func (s queueScreen) detailView(rt *shared, j app.JobSnapshot) screenView {
	row := func(key, val string) string {
		return fit(rt.T("ui.tui.queue.detail."+key, nil), 11) + clean(val)
	}
	lines := []string{
		render(rt.st.heading, jobLabel(j)),
		row("path", orDash(j.Path)),
		row("sha256", orDash(j.SHA256)),
		row("attempts", strconv.Itoa(j.Attempt)),
		row("duration", clock(j.Finished.Sub(j.Started))),
	}
	switch {
	case consent.Needed(j):
		lines = append(lines, row("status", consentRow(rt, j))) // the code stays out: the line is a sentence, and the frame would cut its end
	case j.Err != nil:
		f := rt.failureOf(j.Err)
		lines = append(lines, row("error", f.msg+" ["+f.code+"]"))
	}
	return screenView{body: lines, status: rt.T("ui.tui.queue.detail_help", nil)}
}

// consentArgs are the values of the "Download anyway" texts: the real size, the catalog's (when it had
// one) and the folder, all cleaned. The key takes the _unknown variant when the catalog had no size.
func consentArgs(j app.JobSnapshot) (suffix string, args i18n.Args) {
	args = i18n.Args{"name": jobLabel(j), "size": humanSize(consent.Size(j)), "dir": clean(consent.Dir(j))}
	if j.CatalogSize > 0 {
		args["estimate"] = humanSize(j.CatalogSize)
		return "", args
	}
	return "_unknown", args
}

func consentRow(rt *shared, j app.JobSnapshot) string {
	suffix, args := consentArgs(j)
	return rt.T("ui.consent.row"+suffix, args)
}

// consentAskText is the question of r on such a row. It must end in [y/N] on the status line, so what
// does not fit is shortened: the longer of the folder and the name first, in the middle (the frame
// would cut the tail, and with it the [y/N]).
func consentAskText(rt *shared, j app.JobSnapshot) string {
	suffix, args := consentArgs(j)
	room := rt.w - 4
	text := rt.T("ui.consent.tui_ask"+suffix, args)
	for cells(text) > room {
		name, _ := args["name"].(string)
		dir, _ := args["dir"].(string)
		key, v := "name", name
		if cells(dir) > cells(name) {
			key, v = "dir", dir
		}
		if cells(v) <= minConsentPart {
			break
		}
		args[key] = midCut(v, cells(v)-1) // one cell at a time keeps the two parts balanced
		text = rt.T("ui.consent.tui_ask"+suffix, args)
	}
	return text
}

// minConsentPart is the least of a name or folder the question keeps when it has to shorten them.
const minConsentPart = 5

// midCut shortens s to w cells keeping its start and its end: "/home/me/…/roms/gbc".
func midCut(s string, w int) string {
	if cells(s) <= w {
		return s
	}
	if w <= 3 {
		return cutTo(s, w)
	}
	rs := []rune(s)
	left, right := 0, len(rs)
	lw, rw := 0, 0
	for left < right && lw+cells(string(rs[left])) <= (w-3+1)/2 {
		lw += cells(string(rs[left]))
		left++
	}
	for right > left && rw+cells(string(rs[right-1])) <= (w-3)/2 {
		rw += cells(string(rs[right-1]))
		right--
	}
	return string(rs[:left]) + "..." + string(rs[right:])
}

// consentFocused tells the status bar that r means "download anyway" on the row under the cursor.
func (s queueScreen) consentFocused(rt *shared) bool {
	nav := s.nav(rt.book)
	return len(nav) > 0 && consent.Needed(nav[s.at(nav)])
}
