package tui

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/consent"
)

const (
	dateWidth     = 16 // "2026-11-20 14:03"
	dateLayout    = "2006-01-02 15:04"
	exportStamp   = "20060102T150405"
	errCodeExists = "exists" // the code of an export that would overwrite a file
)

type (
	histMsg struct {
		tok  int64
		recs []app.HistoryRecord
		err  error
	}
	exportDoneMsg struct {
		path   string
		target string
		err    error
	}
	// jobEndedMsg tells the screens a job reached a final state, so History can reload.
	jobEndedMsg struct{}
)

// overwriteAsk is an export waiting for the "Overwrite? [y/N]" answer.
type overwriteAsk struct {
	path string
}

// historyScreen lists past downloads (tui.md section 6). The service returns every record once; the
// text filter, paging and re-queue work on that list (ui.md section 3).
type historyScreen struct {
	recs       []app.HistoryRecord
	st         loadState
	fail       failure
	tok        int64
	failedOnly bool
	filter     string
	typing     bool
	input      textinput.Model
	curID      string
	top        int
	detail     bool
	export     *textinput.Model
	over       *overwriteAsk
	ask        *app.HistoryRecord // a record that stopped on its real size, waiting for the "[y/N]" before it is queued again
	queued     map[string]bool    // records queued again this run: d twice would queue the file twice
}

func newHistory() historyScreen {
	in := textinput.New()
	in.Prompt = ""
	in.SetWidth(30)
	return historyScreen{st: loadLoading, input: in}
}

func (historyScreen) id() screenID    { return scHistory }
func (s historyScreen) loading() bool { return s.st == loadLoading }
func (s historyScreen) title(rt *shared) string {
	t := rt.T("ui.tui.screen.history", nil)
	if s.st == loadReady {
		t += " ─ " + s.summary(rt)
	}
	return t
}

func (s historyScreen) focus() focusKind {
	switch {
	case s.over != nil || s.ask != nil:
		return focusPrompt
	case s.typing || s.export != nil:
		return focusText
	}
	return focusNone
}

func (s historyScreen) init(rt *shared) (screen, tea.Cmd) { return s.load(rt) }

// load reads the records with the current failed-only setting.
func (s historyScreen) load(rt *shared) (historyScreen, tea.Cmd) {
	s.tok = rt.token()
	tok, e, failed := s.tok, rt.env, s.failedOnly
	return s, func() tea.Msg {
		recs, err := e.svc.History(e.ctx, app.HistoryFilter{Failed: failed})
		return histMsg{tok: tok, recs: recs, err: err}
	}
}

// visible is the loaded records that match the text filter, newest first.
func (s historyScreen) visible() []app.HistoryRecord {
	return filterRecords(s.recs, s.filter)
}

// filterRecords keeps the records whose title contains the text, ignoring case.
func filterRecords(recs []app.HistoryRecord, text string) []app.HistoryRecord {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return recs
	}
	var out []app.HistoryRecord
	for _, r := range recs {
		if strings.Contains(strings.ToLower(r.Title), text) {
			out = append(out, r)
		}
	}
	return out
}

// newestFirst orders records by start time, newest first, keeping the given order for equal times.
func newestFirst(recs []app.HistoryRecord) []app.HistoryRecord {
	out := slices.Clone(recs)
	slices.SortStableFunc(out, func(a, b app.HistoryRecord) int { return b.StartedAt.Compare(a.StartedAt) })
	return out
}

// at is the position of the cursor in the visible list.
func (s historyScreen) at(vis []app.HistoryRecord) int {
	for i, r := range vis {
		if r.ID == s.curID {
			return i
		}
	}
	return 0
}

func (s historyScreen) update(msg tea.Msg, rt *shared) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case histMsg:
		return s.onLoaded(msg, rt)
	case jobEndedMsg:
		if s.st == loadReady || s.st == loadEmpty {
			return s.load(rt)
		}
		return s, nil
	case exportDoneMsg:
		return s.onExported(msg, rt)
	case retriedMsg: // a failed Enqueue gives the records back (the record id is the job id)
		if msg.enq.err != nil {
			s.queued = without(s.queued, msg.ids)
		}
		return s, nil
	case keyEv:
		return s.onKey(msg, rt)
	}
	return s, nil
}

func (s historyScreen) onLoaded(msg histMsg, rt *shared) (screen, tea.Cmd) {
	if msg.tok != s.tok {
		return s, nil
	}
	if msg.err != nil {
		s.st, s.fail = loadError, rt.failureOf(msg.err)
		return s, nil
	}
	s.recs = newestFirst(msg.recs)
	s.st = loadReady
	if len(s.recs) == 0 {
		s.st = loadEmpty
	}
	return s, nil
}

func (s historyScreen) onExported(msg exportDoneMsg, rt *shared) (screen, tea.Cmd) {
	var ae *app.Error
	switch {
	case msg.err == nil:
		return s, toastCmd(rt.T("ui.cli.export.saved", i18n.Args{"path": clean(msg.target)}), toastInfo)
	case errors.As(msg.err, &ae) && ae.Code == errCodeExists:
		s.over = &overwriteAsk{path: msg.path}
		return s, nil
	}
	f := rt.failureOf(msg.err)
	return s, toastCmd(f.msg+" ["+f.code+"]", toastError)
}

func (s historyScreen) onKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	switch {
	case s.over != nil:
		return s.answerOverwrite(ev.a, rt)
	case s.ask != nil:
		return s.answerConsent(ev.a, rt)
	case s.export != nil:
		return s.onExportKey(ev, rt)
	case s.typing:
		return s.onFilterKey(ev), nil
	case s.st == loadError:
		switch ev.a {
		case actRetry:
			return s.load(rt)
		case actBack:
			return s, popCmd()
		}
		return s, nil
	}
	vis := s.visible()
	i := s.at(vis)
	switch ev.a {
	case actBack:
		if s.detail {
			s.detail = false
			return s, nil
		}
		return s, popCmd()
	case actUp, actDown, actFirst, actLast, actPageUp, actPageDown:
		s.detail = false
		if len(vis) > 0 {
			s.curID = vis[moveBy(ev.a, i, len(vis), rowsH(rt))].ID
			s.top = scrollTop(s.top, moveBy(ev.a, i, len(vis), rowsH(rt)), listRows(rt, s), len(vis))
		}
	case actFailedOnly:
		s.failedOnly = !s.failedOnly
		return s.load(rt)
	case actFilter:
		s.typing = true
		s.input.SetValue(s.filter)
		s.input.Focus()
	case actExport:
		in := textinput.New()
		in.Prompt = ""
		in.SetWidth(46)
		in.SetValue("./mothdex-history-" + rt.now().UTC().Format(exportStamp) + ".csv")
		in.Focus()
		s.export = &in
	case actEnter:
		if len(vis) > 0 {
			s.curID, s.detail = vis[i].ID, !s.detail
		}
	case actDownload:
		return s.requeue(vis, i, rt)
	}
	return s, nil
}

// moveBy is the cursor position after a movement key; a page is the number of rows on screen.
func moveBy(a action, i, n, page int) int {
	switch a {
	case actUp:
		i--
	case actDown:
		i++
	case actFirst:
		i = 0
	case actLast:
		i = n - 1
	case actPageUp:
		i -= max(page, 1)
	case actPageDown:
		i += max(page, 1)
	}
	return max(0, min(i, n-1))
}

// listRows is how many records fit under the optional filter line.
func listRows(rt *shared, s historyScreen) int {
	if s.typing || s.filter != "" {
		return max(rowsH(rt)-1, 1)
	}
	return rowsH(rt)
}

// onFilterKey edits the text filter: Enter keeps it, Esc clears it.
func (s historyScreen) onFilterKey(ev keyEv) historyScreen {
	switch ev.a {
	case actEnter:
		s.filter, s.typing = strings.TrimSpace(s.input.Value()), false
	case actBlur:
		s.filter, s.typing = "", false
	case actText:
		s.input, _ = s.input.Update(ev.k)
	}
	s.top, s.curID = 0, ""
	return s
}

// requeue queues a failed record again with the link it used.
func (s historyScreen) requeue(vis []app.HistoryRecord, i int, rt *shared) (screen, tea.Cmd) {
	if len(vis) == 0 {
		return s, nil
	}
	r := vis[i]
	if r.Error == nil {
		return s, toastCmd(rt.T("ui.tui.history.only_failed", nil), toastWarn)
	}
	if s.queued[r.ID] {
		return s, toastCmd(rt.T("ui.tui.history.already_queued", nil), toastWarn)
	}
	if consent.RecordNeeded(r) { // it stopped on its real size: asks again, with that size, instead of going out with no consent
		s.ask = &r
		return s, nil
	}
	return s.enqueueRecord(r, 0, rt)
}

// recordDir is where the file would be saved: the profile's folder for the platform, else the ROM root.
func recordDir(rt *shared, r app.HistoryRecord) string {
	p := rt.svc.ActiveProfile()
	if dir, err := p.Profile.DestDir(r.Platform); err == nil && dir != "" {
		return clean(dir)
	}
	return clean(p.RomRoot)
}

// answerConsent is the answer of the question of a record that stopped on its real size: y queues it again with the
// consent for exactly that size, anything else leaves it as it is.
func (s historyScreen) answerConsent(a action, rt *shared) (screen, tea.Cmd) {
	r := *s.ask
	s.ask = nil
	if a != actYes || s.queued[r.ID] {
		return s, nil
	}
	return s.enqueueRecord(r, r.ContentLength, rt)
}

// enqueueRecord queues a record's file again with the link it used; consent is the size accepted for it, 0 for none.
func (s historyScreen) enqueueRecord(r app.HistoryRecord, consentSize int64, rt *shared) (screen, tea.Cmd) {
	s.queued = withID(s.queued, r.ID)
	req := app.DownloadRequest{Ref: app.Ref{Slug: r.Slug}, Link: app.LinkSelector{Index: r.LinkIndex, Prefer: rt.prefs}, SizeConsent: consentSize}
	enqueue := enqueueCmd(rt.env, []app.DownloadRequest{req})
	ids := []app.JobID{app.JobID(r.ID)}
	return s, func() tea.Msg {
		enq, _ := enqueue().(enqueuedMsg)
		return retriedMsg{enq: enq, ids: ids}
	}
}

func withID(m map[string]bool, id string) map[string]bool {
	out := make(map[string]bool, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[id] = true
	return out
}

func without(m map[string]bool, ids []app.JobID) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	for _, id := range ids {
		delete(out, string(id))
	}
	return out
}

func (s historyScreen) onExportKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	switch ev.a {
	case actBlur:
		s.export = nil
	case actText:
		in, _ := s.export.Update(ev.k)
		s.export = &in
	case actEnter:
		path := strings.TrimSpace(s.export.Value())
		s.export = nil
		if path != "" {
			return s, exportCmd(rt, s.visible(), path, false)
		}
	}
	return s, nil
}

func (s historyScreen) answerOverwrite(a action, rt *shared) (screen, tea.Cmd) {
	path := s.over.path
	s.over = nil
	if a == actYes {
		return s, exportCmd(rt, s.visible(), path, true)
	}
	return s, nil
}

// exportCmd writes the records to a file; the format comes from the extension (txt, json, csv). An
// existing file is refused unless force is set, and the screen asks first.
func exportCmd(rt *shared, recs []app.HistoryRecord, path string, force bool) tea.Cmd {
	now, version := rt.now(), buildinfo.Get().Version
	return func() tea.Msg {
		target, format, err := app.ExportTarget(path, "", "history", now)
		if err != nil {
			return exportDoneMsg{path: path, err: err}
		}
		err = app.WriteExport(target, force, func(w io.Writer) error {
			return app.ExportHistory(w, format, recs, app.HistoryExportMeta{CLIVersion: version, FetchedAt: now})
		})
		return exportDoneMsg{path: path, target: target, err: err}
	}
}

// stateCell is the State column: a symbol with its ASCII fallback, and the error code of a failure.
func stateCell(st styles, r app.HistoryRecord) string {
	switch {
	case r.Error != nil:
		return render(st.bad, st.g.fail+" "+clean(r.Error.Code))
	case r.State == string(app.StateDone):
		return render(st.ok, st.g.ok)
	}
	return render(st.dim, clean(r.State))
}

func (s historyScreen) view(rt *shared, w, h int) screenView {
	switch s.st {
	case loadLoading:
		return screenView{body: []string{rt.st.spin(rt.spin) + " " + rt.T("ui.tui.loading", nil)}}
	case loadError:
		return screenView{body: errorBody(rt, s.fail), status: rt.T("ui.tui.results.retry_help", nil)}
	case loadEmpty:
		return screenView{body: []string{render(rt.st.dim, rt.T("ui.tui.history.empty", nil))}}
	}
	if s.over != nil {
		return screenView{body: s.rows(rt, w, h), status: render(rt.st.warn, rt.T("ui.tui.history.overwrite", nil)), prompt: true}
	}
	if s.ask != nil {
		return screenView{body: s.rows(rt, w, h), status: render(rt.st.warn, consentAskText(rt, consent.RecordJob(*s.ask, recordDir(rt, *s.ask)))), prompt: true}
	}
	if s.export != nil {
		return screenView{body: s.rows(rt, w, h), status: rt.T("ui.tui.history.export_to", nil) + " [ " + s.export.View() + " ]", prompt: true}
	}
	vis := s.visible()
	if s.detail && len(vis) > 0 {
		return s.detailView(rt, vis[s.at(vis)])
	}
	return screenView{body: s.rows(rt, w, h)}
}

// rows draws the filter line, when there is one, and the visible records.
func (s historyScreen) rows(rt *shared, w, h int) []string {
	var lines []string
	switch {
	case s.typing:
		lines = append(lines, rt.T("ui.tui.history.filter", nil)+": [ "+s.input.View()+" ]")
	case s.filter != "":
		lines = append(lines, render(rt.st.dim, rt.T("ui.tui.history.filter", nil)+": "+clean(s.filter)))
	}
	vis := s.visible()
	if len(vis) == 0 {
		return append(lines, render(rt.st.dim, rt.T("ui.tui.history.no_match", nil)))
	}
	room := max(h-len(lines), 1)
	i := s.at(vis)
	top := scrollTop(s.top, i, room, len(vis))
	wide := rt.w >= fullCols
	platW, sizeW := 0, 0
	for _, r := range vis {
		platW, sizeW = max(platW, cells(clean(r.Platform))), max(sizeW, cells(humanSize(r.Bytes)))
	}
	stateW := 0
	for _, r := range vis {
		stateW = max(stateW, cells(stateCell(rt.st, r)))
	}
	titleW := max(w-1-dateWidth-2-stateW-2, 8)
	if wide {
		titleW = max(titleW-2-platW-2-sizeW, 8)
	}
	for n := top; n < len(vis) && n < top+room; n++ {
		r := vis[n]
		parts := []string{fit(r.StartedAt.Local().Format(dateLayout), dateWidth), fit(stateCell(rt.st, r), stateW), fit(clean(r.Title), titleW)}
		if wide {
			parts = append(parts, fit(clean(r.Platform), platW), padLeft(orDash(strings.TrimSpace(humanSize(r.Bytes))), sizeW))
		}
		line := strings.Join(parts, "  ")
		if n == i {
			line = render(rt.st.on, ">"+fit(line, max(w-1, 1)))
		} else {
			line = " " + line
		}
		lines = append(lines, line)
	}
	return lines
}

// summary is "filter: all · 128 records", shown in the title bar.
func (s historyScreen) summary(rt *shared) string {
	f := rt.T("ui.tui.history.all", nil)
	if s.failedOnly {
		f = rt.T("ui.tui.history.failed", nil)
	}
	return rt.N("ui.tui.history.records", len(s.visible()), i18n.Args{"filter": f})
}

// detailView is the panel of one record (tui.md section 6).
func (s historyScreen) detailView(rt *shared, r app.HistoryRecord) screenView {
	row := func(key, val string) string {
		return fit(rt.T("ui.tui.history.detail."+key, nil), 11) + clean(val)
	}
	lines := []string{
		render(rt.st.heading, clean(r.Title)),
		row("file", orDash(r.Filename)),
		row("link", orDash(linkText(r))),
		row("path", orDash(r.Path)),
		row("sha256", orDash(r.SHA256)),
		row("crc", orDash(r.CRC)),
		row("attempts", strconv.Itoa(r.Attempts)),
		row("duration", clock(time.Duration(r.DurationMS)*time.Millisecond)),
	}
	if r.Error != nil {
		f := rt.failureOf(&app.Error{Code: r.Error.Code, Message: r.Error.Message})
		lines = append(lines, row("error", f.msg+" ["+f.code+"]"))
	}
	return screenView{body: lines, status: rt.T("ui.tui.history.detail_help", nil)}
}

func linkText(r app.HistoryRecord) string {
	if r.SourceName == "" {
		return "#" + strconv.Itoa(r.LinkIndex)
	}
	return "#" + strconv.Itoa(r.LinkIndex) + " " + r.SourceName
}
