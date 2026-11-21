package gui

import (
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

const (
	etaAfter       = 3 * time.Second  // an estimate is shown only after this long of transfer
	warmHintAfter  = 30 * time.Second // the Warming explanation appears after this long
	sheetRowsMax   = 4
	detailHashLen  = 16
	queueUITickDur = time.Second // the Warming counter has its own 1 s timer
)

// jobBook is the GUI's picture of the downloads of this run for the Queue screen: the latest snapshot of every
// job in the order they were first seen, and when each began to warm up or to transfer. It is a value.
type jobBook struct {
	order     []app.JobID
	snaps     map[app.JobID]app.JobSnapshot
	warmSince map[app.JobID]time.Time
	xferSince map[app.JobID]time.Time
}

func (b jobBook) apply(j app.JobSnapshot, now time.Time) jobBook {
	next := jobBook{order: b.order, snaps: maps.Clone(b.snaps), warmSince: maps.Clone(b.warmSince), xferSince: maps.Clone(b.xferSince)}
	if next.snaps == nil {
		next.snaps, next.warmSince, next.xferSince = map[app.JobID]app.JobSnapshot{}, map[app.JobID]time.Time{}, map[app.JobID]time.Time{}
	}
	if _, known := next.snaps[j.ID]; !known {
		next.order = append(slices.Clone(b.order), j.ID)
	}
	next.snaps[j.ID] = j
	if j.State == app.StateWarming {
		if _, ok := next.warmSince[j.ID]; !ok {
			next.warmSince[j.ID] = now
		}
	} else {
		delete(next.warmSince, j.ID)
	}
	if j.State == app.StateTransferring {
		if _, ok := next.xferSince[j.ID]; !ok {
			next.xferSince[j.ID] = now
		}
	} else {
		delete(next.xferSince, j.ID)
	}
	return next
}

// queueItem is a row of the Queue list: a job, or the header of the Finished group.
type queueItem struct {
	job    app.JobSnapshot
	header string
}

// sheetItem is an entry of the action sheet.
type sheetItem struct {
	id    string
	label string
}

// queueState is the Queue screen (docs/ux/gui.md section 5.8).
type queueState struct {
	list     listState
	hidden   map[app.JobID]bool
	focusJob app.JobID // the job that has focus, so focus follows it when the list changes

	sheet      []sheetItem // the open action sheet, empty when closed
	sheetFocus string
	sheetJob   app.JobID
	details    app.JobID // the job whose details are shown, "" when closed
}

// Ids of the action sheet items.
const (
	sheetCancel   = "cancel"
	sheetRetry    = "retry"
	sheetDetails  = "details"
	sheetHide     = "hide"
	sheetClose    = "close"
	sheetAnyway   = "anyway"
	itemCancelAll = "menu:cancel_all"
	itemHideDone  = "menu:hide_finished"
)

func isActive(j app.JobSnapshot) bool { return !j.Terminal() && j.State != app.StateQueued }

// queueItems orders the jobs: active, then queued, then the Finished group, newest first.
func (m Model) queueItems() []queueItem {
	var active, queued, finished []app.JobSnapshot
	for _, id := range m.book.order {
		j := m.book.snaps[id]
		switch {
		case j.Terminal():
			if !m.queue.hidden[id] {
				finished = append(finished, j)
			}
		case j.State == app.StateQueued:
			queued = append(queued, j)
		default:
			active = append(active, j)
		}
	}
	slices.Reverse(finished)
	var items []queueItem
	for _, j := range append(active, queued...) {
		items = append(items, queueItem{job: j})
	}
	if len(finished) > 0 {
		items = append(items, queueItem{header: m.o.T.N("ui.gui.queue.finished", len(finished), i18n.Args{"count": len(finished)})})
		for _, j := range finished {
			items = append(items, queueItem{job: j})
		}
	}
	return items
}

func (m Model) openQueue() Model {
	if m.screen == screenQueue {
		return m
	}
	m = m.enter(screenQueue)
	m.queue.list, m.queue.sheet, m.queue.details = listState{}, nil, ""
	items := m.queueItems()
	for i, it := range items {
		if it.header == "" {
			m.queue.list.focus = i
			m.queue.focusJob = it.job.ID
			break
		}
	}
	return m
}

// followFocus keeps the focus on the same job when the list changed under it.
func (m Model) followFocus() Model {
	items := m.queueItems()
	for i, it := range items {
		if it.header == "" && it.job.ID == m.queue.focusJob {
			m.queue.list.focus = i
			return m
		}
	}
	// The job is gone: the nearest row that is a job.
	f := clampFocus(m.queue.list.focus, len(items))
	for d := 0; d < len(items); d++ {
		for _, i := range []int{f - d, f + d} {
			if i >= 0 && i < len(items) && items[i].header == "" {
				m.queue.list.focus, m.queue.focusJob = i, items[i].job.ID
				return m
			}
		}
	}
	m.queue.list = listState{}
	return m
}

// itemHeight is how tall a Queue row is: a header is one line, a queued or finished job two lines (name and
// state), and an active job three, since the Warming notice adds a line after 30 s.
func (m Model) itemHeight(it queueItem) int {
	met := m.metrics
	switch {
	case it.header != "":
		return LineHeight(met.Secondary) + met.Unit
	case isActive(it.job):
		return LineHeight(met.Body) + 2*LineHeight(met.Secondary) + met.Unit
	}
	return LineHeight(met.Body) + LineHeight(met.Secondary) + met.Unit
}

// queueWindow scrolls the list by item heights so that the focused row is fully visible, and returns the first
// row shown and the one after the last.
func (m Model) queueWindow(items []queueItem) (top, to int) {
	avail := m.metrics.ContentRect().H
	top = min(max(m.queue.list.top, 0), max(len(items)-1, 0))
	focus := clampFocus(m.queue.list.focus, len(items))
	if focus < top {
		top = focus
	}
	for top < focus {
		used := 0
		for i := top; i <= focus; i++ {
			used += m.itemHeight(items[i])
		}
		if used <= avail {
			break
		}
		top++
	}
	used := 0
	to = top
	for to < len(items) && used+m.itemHeight(items[to]) <= avail {
		used += m.itemHeight(items[to])
		to++
	}
	return top, max(to, min(top+1, len(items)))
}

func (m Model) focusedJob() (app.JobSnapshot, bool) {
	items := m.queueItems()
	f := m.queue.list.focus
	if f >= 0 && f < len(items) && items[f].header == "" {
		return items[f].job, true
	}
	return app.JobSnapshot{}, false
}

func (m Model) onQueueAction(a input.Action) (Model, []Cmd) {
	q := m.queue
	if q.details != "" {
		if a == input.Back || a == input.Confirm {
			m.queue.details = ""
		}
		return m, nil
	}
	if len(q.sheet) > 0 {
		return m.onSheetAction(a)
	}
	items := m.queueItems()
	switch a {
	case input.Up, input.Down:
		dir := 1
		if a == input.Up {
			dir = -1
		}
		rows := make([]pickRow, len(items))
		for i, it := range items {
			rows[i] = pickRow{header: it.header != ""}
		}
		to := moveChoice(rows, q.list.focus, dir)
		q.list.focus = to
		m.queue = q
		m.queue.list.top, _ = m.queueWindow(items)
		if to < len(items) && items[to].header == "" {
			m.queue.focusJob = items[to].job.ID
		}
	case input.Confirm:
		if j, ok := m.focusedJob(); ok {
			m.queue.sheet = m.sheetFor(j)
			m.queue.sheetFocus, m.queue.sheetJob = m.queue.sheet[0].id, j.ID
		}
	case input.Info:
		if j, ok := m.focusedJob(); ok {
			m.queue.details = j.ID
		}
	case input.Back:
		return m.leave(), nil
	}
	return m, nil
}

// sheetFor is the action sheet of a job by its state: never a direct cancel (section 5.8).
func (m Model) sheetFor(j app.JobSnapshot) []sheetItem {
	closeItem := sheetItem{sheetClose, m.tr("ui.gui.queue.close", nil)}
	details := sheetItem{sheetDetails, m.tr("ui.gui.queue.details", nil)}
	switch {
	case !j.Terminal():
		return []sheetItem{{sheetCancel, m.tr("ui.gui.queue.cancel", nil)}, details, closeItem}
	case j.State == app.StateFailed || j.State == app.StateCanceled:
		return []sheetItem{{sheetRetry, m.tr("ui.gui.queue.retry", nil)}, details, closeItem}
	}
	return []sheetItem{details, {sheetHide, m.tr("ui.gui.queue.hide", nil)}, closeItem}
}

func (m Model) onSheetAction(a input.Action) (Model, []Cmd) {
	q := m.queue
	idx := slices.IndexFunc(q.sheet, func(s sheetItem) bool { return s.id == q.sheetFocus })
	switch a {
	case input.Up:
		idx = max(idx-1, 0)
	case input.Down:
		idx = min(idx+1, len(q.sheet)-1)
	case input.Back:
		m.queue.sheet = nil
		return m, nil
	case input.Confirm:
		return m.runSheet(q.sheetFocus, q.sheetJob)
	}
	m.queue.sheetFocus = q.sheet[max(idx, 0)].id
	return m, nil
}

func (m Model) runSheet(id string, job app.JobID) (Model, []Cmd) {
	m.queue.sheet = nil
	j := m.book.snaps[job]
	switch id {
	case sheetCancel:
		return m, []Cmd{cancelCmd(m.o.Service, []app.JobSnapshot{j})}
	case sheetRetry:
		return m.enqueue(download{slug: j.Slug, title: j.Title, link: max(j.LinkIndex, 1)})
	case sheetDetails:
		m.queue.details = job
	case sheetHide:
		hidden := maps.Clone(m.queue.hidden)
		if hidden == nil {
			hidden = map[app.JobID]bool{}
		}
		hidden[job] = true
		m.queue.hidden = hidden
		return m.followFocus(), nil
	}
	return m, nil
}

func cancelCmd(svc Service, jobs []app.JobSnapshot) Cmd {
	return func() Msg {
		for _, j := range jobs {
			_ = svc.Cancel(j.ID) // a job that just finished refuses: nothing to do
		}
		return nil
	}
}

// cancelAllCmd cancels every job that has not finished (the confirmed Cancel all downloads).
func (m Model) cancelAllCmd() Cmd {
	var jobs []app.JobSnapshot
	for _, id := range m.book.order {
		if j := m.book.snaps[id]; !j.Terminal() {
			jobs = append(jobs, j)
		}
	}
	return cancelCmd(m.o.Service, jobs)
}

// hideFinished drops every finished job from the list (the History keeps them).
func (m Model) hideFinished() Model {
	hidden := maps.Clone(m.queue.hidden)
	if hidden == nil {
		hidden = map[app.JobID]bool{}
	}
	for _, id := range m.book.order {
		if m.book.snaps[id].Terminal() {
			hidden[id] = true
		}
	}
	m.queue.hidden = hidden
	return m.followFocus()
}

// noteJob feeds a job snapshot into the Queue's book and keeps marks and focus up to date.
func (m Model) noteJob(j app.JobSnapshot, now time.Time) Model {
	m.book = m.book.apply(j, now)
	if j.State == app.StateDone {
		m = m.markDone(j.Slug, max(j.LinkIndex, 1))
	}
	if m.screen == screenQueue {
		m = m.followFocus()
	}
	return m
}

func (m Model) queueFrame() frameSpec {
	active, queued := m.jobs.counts()
	extra := ""
	switch {
	case active+queued == 0:
	case queued > 0:
		extra = m.tr("ui.gui.queue.counts", map[string]any{"active": active, "queued": queued})
	default:
		extra = m.tr("ui.gui.queue.counts_active", map[string]any{"active": active})
	}
	spec := frameSpec{crumbs: []string{m.tr("ui.gui.screen.queue", nil)}, extra: extra}
	back := m.tr("ui.gui.hint.back", nil)
	switch {
	case m.queue.details != "":
		spec.hints = []hint{{role: input.Back, label: m.tr("ui.gui.queue.close", nil)}}
	case len(m.queue.sheet) > 0:
		spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.select", nil)}, {role: input.Back, label: m.tr("ui.gui.queue.close", nil)}}
	default:
		spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.actions", nil)}, {role: input.Info, label: m.tr("ui.gui.queue.details", nil)}, {role: input.Back, label: back}}
	}
	return spec
}

// ---- drawing ----

func (m Model) jobLabel(j app.JobSnapshot) string {
	for _, s := range []string{j.Name, j.Title, j.Slug} {
		if s != "" {
			return app.SanitizeText(s)
		}
	}
	return string(j.ID)
}

func eta(j app.JobSnapshot, since, now time.Time) string {
	if j.RateBps <= 0 || j.Expected <= 0 || since.IsZero() || now.Sub(since) < etaAfter {
		return "--:--"
	}
	return clock(time.Duration(max(j.Expected-j.Bytes, 0)/j.RateBps) * time.Second)
}

func (m Model) drawQueue(p painter, now time.Time) {
	met := m.metrics
	c := met.ContentRect()
	items := m.queueItems()
	if len(items) == 0 {
		p.text(c.X+met.Unit, c.Y+met.Unit, met.Body, colDim, m.tr("ui.gui.queue.empty", nil))
		return
	}
	from, to := m.queueWindow(items)
	y := c.Y
	for i := from; i < to; i++ {
		h := m.itemHeight(items[i])
		r := draw.Rect{X: c.X, Y: y, W: c.W, H: h}
		y += h
		it := items[i]
		if it.header != "" {
			p.text(r.X+met.Unit, r.Y+(r.H-LineHeight(met.Secondary))/2, met.Secondary, colDim, it.header)
			continue
		}
		box := r.Inset(1)
		if i == clampFocus(m.queue.list.focus, len(items)) {
			p.box(box, true)
		}
		m.drawJobRow(p, box, it.job, now)
	}
	switch {
	case len(m.queue.sheet) > 0:
		m.drawSheet(p)
	case m.queue.details != "":
		m.drawDetails(p)
	}
}

func (m Model) drawJobRow(p painter, r draw.Rect, j app.JobSnapshot, now time.Time) {
	met := m.metrics
	x := r.X + met.Unit
	w := r.W - 2*met.Unit
	y := r.Y + met.Unit/2
	label := m.jobLabel(j)
	if j.Terminal() {
		m.drawFinishedRow(p, r, j)
		return
	}
	p.text(x, y, met.Body, colText, p.fit(met.Body, label, w))
	y += LineHeight(met.Body)
	switch j.State {
	case app.StateQueued:
		p.text(x, y, met.Secondary, colDim, m.tr("ui.status.queued", nil))
	case app.StatePreflight, app.StateConnecting:
		p.text(x, y, met.Secondary, colText, m.tr("ui.cli.download.connecting", nil))
	case app.StateWarming:
		since := m.book.warmSince[j.ID]
		p.text(x, y, met.Secondary, colWarn, m.tr("ui.cli.download.warming", nil)+" "+clock(now.Sub(since)))
		if now.Sub(since) >= warmHintAfter {
			p.text(x, y+LineHeight(met.Secondary), met.Secondary, colDim, p.fit(met.Secondary, m.tr("ui.tui.queue.warming_hint", nil), w))
		}
	case app.StateRetryWait:
		p.text(x, y, met.Secondary, colWarn, m.tr("ui.cli.download.retry", i18n.Args{"attempt": j.Attempt}))
	case app.StateVerifying:
		p.text(x, y, met.Secondary, colText, m.tr("ui.cli.download.verifying", nil))
	case app.StateFinalizing:
		p.text(x, y, met.Secondary, colText, m.tr("ui.cli.download.saving", nil))
	default:
		m.drawProgress(p, draw.Rect{X: x, Y: y, W: w, H: LineHeight(met.Secondary)}, j, now)
	}
}

// drawProgress draws the bar and the numbers of a transferring job.
func (m Model) drawProgress(p painter, r draw.Rect, j app.JobSnapshot, now time.Time) {
	met := m.metrics
	pct := 0
	if j.Expected > 0 {
		pct = int(min(max(j.Bytes*100/j.Expected, 0), 100))
	}
	barW := r.W * 35 / 100
	by := r.Y + (r.H-met.Progress)/2
	p.l.Fill(draw.Rect{X: r.X, Y: by, W: barW, H: met.Progress}, met.Progress/2, colBG)
	if pct > 0 {
		p.l.Fill(draw.Rect{X: r.X, Y: by, W: max(barW*pct/100, met.Progress), H: met.Progress}, met.Progress/2, colAccent)
	}
	stats := strconv.Itoa(pct) + "%  " + humanSize(j.Bytes) + " / " + humanSize(j.Expected)
	if j.RateBps > 0 {
		stats += "  " + humanSize(j.RateBps) + "/s"
	}
	stats += "  " + eta(j, m.book.xferSince[j.ID], now)
	p.text(r.X+barW+met.Unit, r.Y, met.Secondary, colText, p.fit(met.Secondary, stats, r.W-barW-met.Unit))
}

func (m Model) drawFinishedRow(p painter, r draw.Rect, j app.JobSnapshot) {
	met := m.metrics
	x := r.X + met.Unit
	w := r.W - 2*met.Unit
	y := r.Y + met.Unit/2
	label := m.jobLabel(j)
	mark := LineHeight(met.Body) - 6
	switch j.State {
	case app.StateDone:
		drawCheck(p.l, x, y+2, mark, colOK)
		p.text(x+mark+met.Unit, y, met.Body, colText, p.fit(met.Body, label, w-mark-met.Unit))
		saved := m.tr("ui.cli.download.saved", i18n.Args{"path": app.SanitizeText(j.Path), "size": humanSize(j.Bytes), "time": clock(j.Finished.Sub(j.Started))})
		p.text(x, y+LineHeight(met.Body), met.Secondary, colDim, p.fit(met.Secondary, saved, w))
	case app.StateSkipped:
		p.text(x, y, met.Body, colText, p.fit(met.Body, label, w))
		p.text(x, y+LineHeight(met.Body), met.Secondary, colDim, p.fit(met.Secondary, m.tr("ui.cli.download.skipped", i18n.Args{"path": app.SanitizeText(j.Path)}), w))
	default:
		drawCross(p.l, x, y+2, mark, colError)
		p.text(x+mark+met.Unit, y, met.Body, colText, p.fit(met.Body, label, w-mark-met.Unit))
		msg := m.tr("errors.canceled.message", nil)
		if j.Err != nil {
			f := m.failureOf(j.Err)
			msg = f.msg + " [" + f.code + "]"
		}
		p.text(x, y+LineHeight(met.Body), met.Secondary, colError, p.fit(met.Secondary, msg, w))
	}
}

// panelList draws a centered panel of one-line rows with the focused one boxed, for the action sheet.
func (m Model) panelList(p painter, labels []string, focus int) {
	met := m.metrics
	u := met.Unit
	widest := 0
	for _, l := range labels {
		widest = max(widest, p.width(met.Body, l))
	}
	panelW := min(max(widest+6*u, 12*met.Body), met.Safe.W-4*u)
	rowH := min(met.Row1, (met.Safe.H-4*u)/max(len(labels), 1))
	panel := draw.Rect{W: panelW, H: len(labels)*rowH + 2*u}
	panel.X, panel.Y = met.Safe.X+(met.Safe.W-panelW)/2, met.Safe.Y+(met.Safe.H-panel.H)/2
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, 0, colScrim)
	p.l.Fill(panel, met.Radius, colSurface)
	for i, l := range labels {
		r := draw.Rect{X: panel.X + u, Y: panel.Y + u + i*rowH, W: panelW - 2*u, H: rowH}
		if i == focus {
			p.box(r, true)
		}
		p.text(r.X+2*u, r.Y+(r.H-LineHeight(met.Body))/2, met.Body, colText, l)
	}
}

func (m Model) drawSheet(p painter) {
	labels := make([]string, len(m.queue.sheet))
	focus := 0
	for i, s := range m.queue.sheet {
		labels[i] = s.label
		if s.id == m.queue.sheetFocus {
			focus = i
		}
	}
	m.panelList(p, labels, focus)
}

// drawDetails shows what the service knows of a job: file, path, size, SHA-256, attempts, duration and error.
func (m Model) drawDetails(p painter) {
	met := m.metrics
	j := m.book.snaps[m.queue.details]
	u := met.Unit
	var lines []string
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, m.tr("ui.gui.queue.d_"+key, nil)+": "+value)
		}
	}
	add("file", app.SanitizeText(j.Name))
	add("path", app.SanitizeText(j.Path))
	add("size", humanSize(j.Bytes)+" / "+humanSize(j.Expected))
	if j.SHA256 != "" {
		add("sha", app.SanitizeText(j.SHA256[:min(detailHashLen, len(j.SHA256))])+"…")
	}
	add("attempts", strconv.Itoa(max(j.Attempt, 1)))
	if !j.Started.IsZero() && !j.Finished.IsZero() {
		add("duration", clock(j.Finished.Sub(j.Started)))
	}
	if j.Err != nil {
		f := m.failureOf(j.Err)
		add("error", f.msg+" ["+f.code+"]")
	}
	maxW := min(met.Safe.W-4*u, 28*met.Body)
	var wrapped []string
	for _, l := range lines {
		wrapped = append(wrapped, p.wrap(met.Secondary, l, maxW-4*u, 3)...)
	}
	h := len(wrapped)*LineHeight(met.Secondary) + LineHeight(met.Body) + 5*u
	panel := draw.Rect{X: met.Safe.X + (met.Safe.W-maxW)/2, Y: met.Safe.Y + max((met.Safe.H-h)/2, 0), W: maxW, H: min(h, met.Safe.H)}
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, 0, colScrim)
	p.l.Fill(panel, met.Radius, colSurface)
	y := panel.Y + 2*u
	p.text(panel.X+2*u, y, met.Body, colText, p.fit(met.Body, m.jobLabel(j), maxW-4*u))
	y += LineHeight(met.Body) + u
	for _, l := range wrapped {
		if y+LineHeight(met.Secondary) > panel.Bottom() {
			break
		}
		p.text(panel.X+2*u, y, met.Secondary, colText, l)
		y += LineHeight(met.Secondary)
	}
}

// batch summary toast ---------------------------------------------------------------------------------------

// summaryToast is shown when the queue goes idle after downloads (docs/ux/gui.md section 5.8): "3 downloaded,
// 1 skipped, 0 failed" for the batch that just ended.
func (m Model) summaryToast(now time.Time) Model {
	done, skipped, failed, _ := m.jobs.summary()
	b := m.batchBase
	text := m.tr("ui.tui.summary", i18n.Args{"done": done - b[0], "skipped": skipped - b[1], "failed": failed - b[2]})
	kind := toastOK
	if failed-b[2] > 0 {
		kind = toastWarn
	}
	return m.withToast(text, kind, now)
}
