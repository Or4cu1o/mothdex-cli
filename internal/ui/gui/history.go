package gui

import (
	"strconv"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// History is docs/ux/gui.md section 5.9: every download this machine has made, newest first, from the service's
// history, with Details and "Download again" on A. It sits on top of whatever screen opened it, like the Queue.

// Ids of the buttons of the Details panel.
const (
	histAgain  = "again"
	histAnyway = "anyway"
	histClose  = "close"
)

// historyState is the History screen's own state; the records themselves are in Model.records.
type historyState struct {
	list        listState
	failedOnly  bool   // Y toggles All and Failed only, client side, over the loaded list
	focusID     string // the record that has focus, so focus follows it when the list is reloaded
	details     string // the record whose details are open, "" when closed
	detailFocus string // the button that has focus in the details
}

// historyRows are the records the screen lists: all of them, or only the failed ones.
func (m Model) historyRows() []app.HistoryRecord {
	if !m.history.failedOnly {
		return m.records
	}
	var out []app.HistoryRecord
	for _, r := range m.records {
		if r.State == string(app.StateFailed) {
			out = append(out, r)
		}
	}
	return out
}

// historyRowHeight is one row: the title and two dim lines under it, for the date and a reason that wraps (nothing is
// cut but titles, docs/ux/gui.md section 1.3; past two lines the text ends in an ellipsis, and Details has it all).
func (m Model) historyRowHeight() int {
	return LineHeight(m.metrics.Body) + 2*LineHeight(m.metrics.Secondary) + m.metrics.Unit
}

func (m Model) historyVisible() int {
	return max(m.metrics.ContentRect().H/m.historyRowHeight(), 1)
}

// openHistory shows the History on top of the current screen and loads the records again (they are local data,
// so there is no spinner).
func (m Model) openHistory() (Model, []Cmd) {
	if m.screen == screenHistory {
		return m, nil
	}
	m = m.enter(screenHistory)
	m.history = historyState{}
	m = m.syncHistoryFocus()
	return m, []Cmd{historyCmd(m.o.Service)}
}

// syncHistoryFocus keeps focus on the same record when the list changed under it (a reload, the filter), and on
// the nearest row when that record is gone.
func (m Model) syncHistoryFocus() Model {
	rows := m.historyRows()
	h := m.history
	for i, r := range rows {
		if r.ID != "" && r.ID == h.focusID {
			h.list.focus = i
			h.list.top = scrollTop(h.list.top, i, m.historyVisible(), len(rows))
			m.history = h
			return m
		}
	}
	h.list.focus = clampFocus(h.list.focus, len(rows))
	h.list.top = scrollTop(h.list.top, h.list.focus, m.historyVisible(), len(rows))
	h.focusID = ""
	if len(rows) > 0 {
		h.focusID = rows[h.list.focus].ID
	}
	m.history = h
	return m
}

// record finds a record by its id.
func (m Model) record(id string) (app.HistoryRecord, bool) {
	for _, r := range m.records {
		if r.ID == id {
			return r, true
		}
	}
	return app.HistoryRecord{}, false
}

func (m Model) onHistoryAction(a input.Action, now time.Time) (Model, []Cmd) {
	if m.history.details != "" {
		return m.onHistoryDetailsAction(a, now)
	}
	h := m.history
	rows := m.historyRows()
	n := len(rows)
	vis := m.historyVisible()
	switch a {
	case input.Up:
		h.list = h.list.moved(-1, vis, n)
	case input.Down:
		h.list = h.list.moved(1, vis, n)
	case input.PageUp:
		h.list = h.list.paged(-1, vis, n)
	case input.PageDown:
		h.list = h.list.paged(1, vis, n)
	case input.Confirm:
		if n > 0 {
			r := rows[clampFocus(h.list.focus, n)]
			h.details = r.ID
			h.detailFocus = m.historyButtons(r)[0].id
		}
	case input.Info:
		h.failedOnly = !h.failedOnly
		m.history = h
		return m.syncHistoryFocus(), nil
	case input.Back:
		return m.leave(), nil
	}
	if n > 0 && h.details == "" {
		h.focusID = rows[clampFocus(h.list.focus, n)].ID
	}
	m.history = h
	return m, nil
}

// historyButtons are the buttons under the details of a record: Download again (or Download anyway for a record
// that stopped on its real size and still waits for the OK) and Close.
func (m Model) historyButtons(r app.HistoryRecord) []dialogButton {
	first := dialogButton{histAgain, m.tr("ui.gui.history.again", nil)}
	if consent.RecordNeeded(r) {
		first = dialogButton{histAnyway, m.tr("ui.consent.action", nil)}
	}
	return []dialogButton{first, {histClose, m.tr("ui.gui.queue.close", nil)}}
}

func (m Model) onHistoryDetailsAction(a input.Action, now time.Time) (Model, []Cmd) {
	r, ok := m.record(m.history.details)
	if !ok {
		m.history.details = ""
		return m, nil
	}
	buttons := m.historyButtons(r)
	idx := 0
	for i, b := range buttons {
		if b.id == m.history.detailFocus {
			idx = i
		}
	}
	switch a {
	case input.Left:
		idx = max(idx-1, 0)
	case input.Right:
		idx = min(idx+1, len(buttons)-1)
	case input.Back:
		m.history.details = ""
		return m, nil
	case input.Confirm:
		return m.runHistoryButton(buttons[idx].id, r, now)
	}
	m.history.detailFocus = buttons[idx].id
	return m, nil
}

func (m Model) runHistoryButton(id string, r app.HistoryRecord, now time.Time) (Model, []Cmd) {
	m.history.details = ""
	switch id {
	case histAgain:
		link := max(r.LinkIndex, 1)
		if m.inQueue(r.Slug, link) { // the same file is already on its way: queue nothing
			return m.withToast(m.tr("ui.tui.history.already_queued", nil), toastWarn, now), nil
		}
		return m.enqueue(download{slug: r.Slug, title: r.Title, link: link})
	case histAnyway: // the stopped download asks again, with the size it stopped on, instead of going out with no consent
		return m.askDownloadAnyway(consent.RecordJob(r, m.destination(r.Platform)), now)
	}
	return m, nil
}

// inQueue reports whether a download of this entry and link is queued or running now.
func (m Model) inQueue(slug string, link int) bool {
	for _, j := range m.book.snaps {
		if j.Slug == slug && max(j.LinkIndex, 1) == link && !j.Terminal() {
			return true
		}
	}
	return false
}

func (m Model) historyFrame() frameSpec {
	filter := m.tr("ui.gui.history.all", nil)
	if m.history.failedOnly {
		filter = m.tr("ui.gui.history.failed", nil)
	}
	n := len(m.historyRows())
	spec := frameSpec{
		crumbs: []string{m.tr("ui.gui.screen.history", nil)},
		extra:  m.o.T.N("ui.gui.history.records", n, map[string]any{"filter": filter, "count": n}),
	}
	back := m.tr("ui.gui.hint.back", nil)
	switch {
	case m.history.details != "":
		spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.select", nil)}, {role: input.Back, label: m.tr("ui.gui.queue.close", nil)}}
	default:
		toggle := m.tr("ui.gui.hint.failed_only", nil)
		if m.history.failedOnly {
			toggle = m.tr("ui.gui.hint.show_all", nil)
		}
		spec.hints = []hint{
			{role: input.Confirm, label: m.tr("ui.gui.queue.details", nil)},
			{role: input.Info, label: toggle},
			{role: input.Back, label: back},
			{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.page", nil), optional: true},
		}
	}
	return spec
}

// ---- drawing ----

func (m Model) drawHistory(p painter) {
	met := m.metrics
	c := met.ContentRect()
	rows := m.historyRows()
	if len(rows) == 0 {
		key := "ui.gui.history.empty"
		if m.history.failedOnly {
			key = "ui.gui.history.empty_failed"
		}
		for i, line := range p.wrap(met.Body, m.tr(key, nil), c.W-2*met.Unit, 3) {
			p.text(c.X+met.Unit, c.Y+met.Unit+i*LineHeight(met.Body), met.Body, colDim, line)
		}
		return
	}
	list, from, to := m.history.list.shown(m.historyVisible(), len(rows))
	h := m.historyRowHeight()
	y := c.Y
	for i := from; i < to; i++ {
		box := draw.Rect{X: c.X, Y: y, W: c.W, H: h}.Inset(1)
		y += h
		if i == list.focus {
			p.box(box, true)
		}
		m.drawHistoryRow(p, box, rows[i])
	}
	if m.history.details != "" {
		m.drawHistoryDetails(p)
	}
}

// recordMark draws the state of a record at x, y: a check, a cross, the ! of a download that waits for the OK, a
// dash for a skipped one.
func (m Model) recordMark(p painter, x, y, size int, r app.HistoryRecord) {
	switch {
	case consent.RecordNeeded(r):
		drawBang(p.l, x, y, size, colWarn)
	case r.State == string(app.StateDone):
		drawCheck(p.l, x, y, size, colOK)
	case r.State == string(app.StateSkipped):
		p.l.Fill(draw.Rect{X: x + size/6, Y: y + size/2 - 1, W: size * 2 / 3, H: 2}, 0, colDim)
	case r.State == string(app.StateCanceled):
		drawCross(p.l, x, y, size, colDim)
	default:
		drawCross(p.l, x, y, size, colError)
	}
}

// recordDate is when the record ended, in the machine's time zone.
func recordDate(r app.HistoryRecord) string {
	t := r.FinishedAt
	if t.IsZero() {
		t = r.StartedAt
	}
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// recordReason is the short reason under a record that did not finish normally, in the interface language.
func (m Model) recordReason(r app.HistoryRecord) string {
	switch {
	case consent.RecordNeeded(r):
		return m.consentRow(consent.RecordJob(r, ""))
	case r.State == string(app.StateFailed) && r.Error != nil:
		return m.failureOf(&app.Error{Code: r.Error.Code, Message: r.Error.Message}).msg
	case r.State == string(app.StateSkipped):
		return m.tr("ui.status.skipped", nil)
	case r.State == string(app.StateCanceled):
		return m.tr("ui.status.canceled", nil)
	}
	return ""
}

func (m Model) drawHistoryRow(p painter, r draw.Rect, rec app.HistoryRecord) {
	met := m.metrics
	x := r.X + met.Unit
	w := r.W - 2*met.Unit
	y := r.Y + met.Unit/2
	mark := LineHeight(met.Body) - 6
	m.recordMark(p, x, y+2, mark, rec)
	size := "-"
	if rec.State == string(app.StateDone) {
		size = humanSize(rec.Bytes)
	}
	right := app.SanitizeText(rec.Platform) + "   " + size
	rightW := p.width(met.Secondary, right)
	p.text(r.Right()-met.Unit-rightW, y+(LineHeight(met.Body)-LineHeight(met.Secondary))/2, met.Secondary, colDim, right)
	titleW := w - mark - met.Unit - rightW - 2*met.Unit
	p.text(x+mark+met.Unit, y, met.Body, colText, p.fit(met.Body, app.SanitizeText(rec.Title), max(titleW, met.Body)))
	line := recordDate(rec)
	if reason := m.recordReason(rec); reason != "" {
		line = m.tr("ui.gui.history.reason", map[string]any{"date": line, "reason": reason})
	}
	color := colDim
	if consent.RecordNeeded(rec) {
		color = colWarn
	}
	for i, l := range p.wrap(met.Secondary, line, w, 2) {
		p.text(x, y+LineHeight(met.Body)+i*LineHeight(met.Secondary), met.Secondary, color, l)
	}
}

// drawHistoryDetails is the panel of the Queue's details, with Download again and Close under it.
func (m Model) drawHistoryDetails(p painter) {
	rec, ok := m.record(m.history.details)
	if !ok {
		return
	}
	met := m.metrics
	u := met.Unit
	var lines []string
	add := func(key, value string) {
		if value != "" {
			lines = append(lines, m.tr("ui.gui.queue.d_"+key, nil)+": "+value)
		}
	}
	add("file", app.SanitizeText(rec.Filename))
	add("path", app.SanitizeText(rec.Path))
	size := humanSize(rec.Bytes)
	if consent.RecordNeeded(rec) {
		size = humanSize(rec.ContentLength) // nothing was transferred: the size is the real one the person is asked about
	}
	add("size", size)
	if rec.SHA256 != "" {
		add("sha", app.SanitizeText(rec.SHA256[:min(detailHashLen, len(rec.SHA256))])+"…")
	}
	add("attempts", strconv.Itoa(max(rec.Attempts, 1)))
	add("duration", clock(time.Duration(rec.DurationMS)*time.Millisecond))
	switch {
	case consent.RecordNeeded(rec):
		add("status", m.consentRow(consent.RecordJob(rec, ""))+" ["+app.SanitizeText(rec.Error.Code)+"]")
	case rec.Error != nil:
		add("error", m.recordReason(rec)+" ["+app.SanitizeText(rec.Error.Code)+"]")
	}
	maxW := min(met.Safe.W-4*u, 28*met.Body)
	var wrapped []string
	for _, l := range lines {
		wrapped = append(wrapped, p.wrap(met.Secondary, l, maxW-4*u, 3)...)
	}
	buttons := m.historyButtons(rec)
	btnH := met.Row1
	h := len(wrapped)*LineHeight(met.Secondary) + LineHeight(met.Body) + 5*u + btnH + u
	panel := draw.Rect{X: met.Safe.X + (met.Safe.W-maxW)/2, Y: met.Safe.Y + max((met.Safe.H-h)/2, 0), W: maxW, H: min(h, met.Safe.H)}
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, 0, colScrim)
	p.l.Fill(panel, met.Radius, colSurface)
	y := panel.Y + 2*u
	p.text(panel.X+2*u, y, met.Body, colText, p.fit(met.Body, app.SanitizeText(rec.Title), maxW-4*u))
	y += LineHeight(met.Body) + u
	for _, l := range wrapped {
		p.text(panel.X+2*u, y, met.Secondary, colText, l)
		y += LineHeight(met.Secondary)
	}
	x := panel.X + 2*u
	by := panel.Bottom() - btnH - u
	for _, b := range buttons {
		bw := p.width(met.Body, b.label) + 4*u
		r := draw.Rect{X: x, Y: by, W: bw, H: btnH}
		if m.history.detailFocus == b.id {
			p.box(r, true)
		} else {
			p.l.Fill(r, met.Radius, colBG)
		}
		p.centered(r, met.Body, colText, b.label)
		x += bw + u
	}
}
