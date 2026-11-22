package tui

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// loadState is where a screen that fetches data stands.
type loadState int

const (
	loadLoading loadState = iota
	loadReady
	loadEmpty
	loadError
)

// row is one result: the number the CLI uses for it (0 for a random pick) and the entry.
type row struct {
	index int
	e     mothdex.Entry
}

// marks are the rows picked with Space, kept per page and dropped with the screen: a new search is a
// new screen (tui.md section 3).
type marks map[int]map[string]bool

func (m marks) has(page int, slug string) bool { return m[page][slug] }

// toggle returns the marks with one row flipped.
func (m marks) toggle(page int, slug string) marks {
	out := make(marks, len(m)+1)
	for p, set := range m {
		cp := make(map[string]bool, len(set)+1)
		for s := range set {
			cp[s] = true
		}
		out[p] = cp
	}
	if out[page] == nil {
		out[page] = map[string]bool{}
	}
	if out[page][slug] {
		delete(out[page], slug)
	} else {
		out[page][slug] = true
	}
	return out
}

func (m marks) count(page int) int { return len(m[page]) }

// pick returns the slugs to act on: the marked rows of the page in list order, or the one under the
// cursor when nothing is marked.
func pick(m marks, page int, rows []row, cur int) []string {
	var out []string
	for _, r := range rows {
		if m.has(page, r.e.Slug) {
			out = append(out, r.e.Slug)
		}
	}
	if len(out) == 0 && cur >= 0 && cur < len(rows) {
		out = append(out, rows[cur].e.Slug)
	}
	return out
}

type (
	searchDoneMsg struct {
		tok  int64
		page app.SearchPage
		err  error
	}
	randomDoneMsg struct {
		tok     int64
		entries []mothdex.Entry
		meta    app.ResultMeta
		err     error
	}
)

// resultsScreen lists a search page, or a few random picks (tui.md section 3).
type resultsScreen struct {
	q      app.SearchQuery
	random bool
	st     loadState
	rows   []row
	page   app.SearchPage
	fail   failure
	tok    int64
	cancel context.CancelFunc
	cur    int
	top    int
	marks  marks
	ask    *pendingBatch // a batch waiting for the "Download N files?" answer
}

// pendingBatch is a download request that interface.confirm_downloads holds back until the user says y.
type pendingBatch struct {
	reqs []app.DownloadRequest
	text string
}

// confirmBytes is the size above which one file also asks (cli-conventions section 6); the downloader's
// check at the real size uses the same constant.
const confirmBytes = app.ConfirmBytes

// needsConfirm is the rule of cli-conventions section 6: with interface.confirm_downloads on, ask when
// more than one file is queued or the total is above 500 MB.
func needsConfirm(confirm bool, files int, bytes int64) bool {
	return confirm && (files > 1 || bytes > confirmBytes)
}

func newResults(q app.SearchQuery, random bool) resultsScreen {
	return resultsScreen{q: q, random: random, st: loadLoading, marks: marks{}}
}

func (resultsScreen) id() screenID { return scResults }
func (s resultsScreen) focus() focusKind {
	if s.ask != nil {
		return focusPrompt
	}
	return focusNone
}
func (s resultsScreen) loading() bool          { return s.st == loadLoading }
func (s resultsScreen) query() app.SearchQuery { return s.q }

func (s resultsScreen) title(rt *shared) string {
	t := rt.T("ui.tui.screen.results", nil)
	switch {
	case s.random:
		t = rt.T("ui.tui.results.random_title", nil)
	case s.q.Text != "" || len(s.q.Platforms) > 0 || len(s.q.Regions) > 0:
		t += " ─ " + s.querySummary()
	}
	if rt.tier == tierShort && s.st == loadReady && !s.random { // few rows: the page footer moves up here
		t += " ─ " + s.footer(rt)
	}
	return t
}

func (s resultsScreen) querySummary() string {
	parts := []string{}
	if s.q.Text != "" {
		parts = append(parts, `"`+clean(s.q.Text)+`"`)
	}
	if len(s.q.Platforms) > 0 {
		parts = append(parts, clean(strings.Join(s.q.Platforms, ",")))
	}
	if len(s.q.Regions) > 0 {
		parts = append(parts, clean(strings.Join(s.q.Regions, ",")))
	}
	return strings.Join(parts, " · ")
}

// init starts the request.
func (s resultsScreen) init(rt *shared) (screen, tea.Cmd) { return s.load(rt) }

// load (re)starts the request with a new context; the previous one is canceled.
func (s resultsScreen) load(rt *shared) (resultsScreen, tea.Cmd) {
	if s.cancel != nil {
		s.cancel()
	}
	ctx, cancel := context.WithCancel(rt.ctx)
	s.cancel, s.tok, s.st = cancel, rt.token(), loadLoading
	tok, q, svc, random := s.tok, s.q, rt.svc, s.random
	return s, func() tea.Msg {
		if random {
			res, err := svc.Random(ctx, app.RandomQuery{
				Filters: mothdex.Filters{SearchKey: q.Text, Platforms: q.Platforms, Regions: q.Regions}, Count: randomCount,
			})
			return randomDoneMsg{tok: tok, entries: res.Value, meta: res.Meta, err: err}
		}
		page, err := svc.Search(ctx, q)
		return searchDoneMsg{tok: tok, page: page, err: err}
	}
}

func (s resultsScreen) update(msg tea.Msg, rt *shared) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDoneMsg:
		if msg.tok != s.tok {
			return s, nil
		}
		return s.onSearch(msg, rt)
	case randomDoneMsg:
		if msg.tok != s.tok {
			return s, nil
		}
		return s.onRandom(msg, rt)
	case keyEv:
		return s.onKey(msg, rt)
	}
	return s, nil
}

func (s resultsScreen) onSearch(msg searchDoneMsg, rt *shared) (screen, tea.Cmd) {
	if msg.err != nil {
		return s.onError(msg.err, rt), nil
	}
	keep := s.slugAt()
	s.page = msg.page
	s.q.Page = msg.page.Page
	s.rows = make([]row, len(msg.page.Results))
	for i, r := range msg.page.Results {
		s.rows[i] = row{index: r.Index, e: r.Entry}
	}
	s.settle(keep, len(s.rows) == 0)
	if note := pageNotes(rt, s.q.Page, msg.page); note != "" {
		return s, toastCmd(note, toastWarn)
	}
	return s, nil
}

// pageNotes joins what the user must know about a page into one toast: a page past the last one, and
// results the slug rule hid (ui.md F2). A second toast would replace the first at once.
func pageNotes(rt *shared, asked int, page app.SearchPage) string {
	var notes []string
	if page.PastLastPage {
		notes = append(notes, rt.T("ui.cli.search.past_last", i18n.Args{"asked": asked, "shown": page.Page}))
	}
	if page.Dropped > 0 {
		notes = append(notes, rt.T("ui.cli.search.dropped", i18n.Args{"n": page.Dropped}))
	}
	return strings.Join(notes, " ")
}

func (s resultsScreen) onRandom(msg randomDoneMsg, rt *shared) (screen, tea.Cmd) {
	if msg.err != nil {
		return s.onError(msg.err, rt), nil
	}
	keep := s.slugAt()
	s.page = app.SearchPage{Meta: msg.meta}
	s.rows = make([]row, len(msg.entries))
	for i, e := range msg.entries {
		s.rows[i] = row{e: e}
	}
	s.settle(keep, len(s.rows) == 0)
	return s, nil
}

// settle finishes a load: the cursor goes back to the row it was on, if it is still there.
func (s *resultsScreen) settle(keep string, empty bool) {
	s.st = loadReady
	if empty {
		s.st = loadEmpty
	}
	s.cur, s.top = 0, 0
	for i, r := range s.rows {
		if r.e.Slug == keep && keep != "" {
			s.cur = i
		}
	}
}

func (s resultsScreen) onError(err error, rt *shared) resultsScreen {
	if errors.Is(err, context.Canceled) { // Esc: the screen is already gone or idle
		return s
	}
	s.st, s.fail = loadError, rt.failureOf(err)
	return s
}

func (s resultsScreen) slugAt() string {
	if s.cur >= 0 && s.cur < len(s.rows) {
		return s.rows[s.cur].e.Slug
	}
	return ""
}

// rowsH is how many result rows fit: the whole body.
func rowsH(rt *shared) int { return max(rt.h-4, 1) }

func (s resultsScreen) onKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	if s.ask != nil {
		batch := s.ask
		s.ask = nil
		if ev.a == actYes {
			return s, enqueueCmd(rt.env, batch.reqs)
		}
		return s, nil
	}
	if s.st == loadLoading {
		if ev.a == actBack { // Esc cancels the request and returns to Search
			if s.cancel != nil {
				s.cancel()
			}
			return s, popCmd()
		}
		return s, nil
	}
	switch ev.a {
	case actBack:
		return s, popCmd()
	case actRetry:
		if s.st == loadError {
			return s.load(rt)
		}
	case actUp, actDown, actFirst, actLast:
		s = s.moveCursor(ev.a, rt)
	case actToggle:
		if s.cur < len(s.rows) {
			s.marks = s.marks.toggle(s.page.Page, s.rows[s.cur].e.Slug)
		}
	case actEnter:
		if s.cur < len(s.rows) {
			slugs := make([]string, len(s.rows))
			for i, r := range s.rows {
				slugs[i] = r.e.Slug
			}
			return s, pushCmd(newEntry(slugs, s.cur))
		}
	case actDownload:
		return s.download(rt)
	case actNextPage, actPrevPage:
		return s.turn(ev.a == actNextPage, rt)
	case actSort:
		if !s.random && s.st == loadReady {
			s.q.Sort = nextSort(s.q.Sort)
			return s.load(rt)
		}
	}
	return s, nil
}

func (s resultsScreen) moveCursor(a action, rt *shared) resultsScreen {
	n := len(s.rows)
	if n == 0 {
		return s
	}
	switch a {
	case actUp:
		s.cur--
	case actDown:
		s.cur++
	case actFirst:
		s.cur = 0
	case actLast:
		s.cur = n - 1
	}
	s.cur = max(0, min(s.cur, n-1))
	s.top = scrollTop(s.top, s.cur, rowsH(rt), n)
	return s
}

// turn goes to the next or previous page of the search through Search with the page changed.
func (s resultsScreen) turn(forward bool, rt *shared) (screen, tea.Cmd) {
	if s.random || s.st != loadReady {
		return s, nil
	}
	switch {
	case forward && s.page.Page >= s.page.TotalPages:
		return s, toastCmd(rt.T("ui.tui.page_last", nil), toastInfo)
	case !forward && s.page.Page <= 1:
		return s, toastCmd(rt.T("ui.tui.page_first", nil), toastInfo)
	case forward:
		s.q.Page = s.page.Page + 1
	default:
		s.q.Page = s.page.Page - 1
	}
	return s.load(rt)
}

// download queues the marked rows, or the row under the cursor; each uses the best link by the
// active preference (tui.md section 3).
func (s resultsScreen) download(rt *shared) (screen, tea.Cmd) {
	slugs := pick(s.marks, s.page.Page, s.rows, s.cur)
	if len(slugs) == 0 {
		return s, nil
	}
	reqs := make([]app.DownloadRequest, len(slugs))
	size := map[string]int64{}
	for _, r := range s.rows {
		size[r.e.Slug] = bestLink(r.e).Size
	}
	var total int64
	for i, slug := range slugs {
		reqs[i] = app.DownloadRequest{Ref: app.Ref{Slug: slug}, Link: app.LinkSelector{Prefer: rt.prefs}}
		total += size[slug]
	}
	if needsConfirm(rt.confirm, len(reqs), total) {
		s.ask = &pendingBatch{reqs: reqs, text: rt.N("ui.tui.confirm_download", len(reqs), i18n.Args{"size": humanSize(total)})}
		return s, nil
	}
	return s, enqueueCmd(rt.env, reqs)
}

func popCmd() tea.Cmd { return func() tea.Msg { return popMsg{} } }

func toastCmd(text string, kind toastKind) tea.Cmd {
	return func() tea.Msg { return toastMsg{text: text, kind: kind} }
}

// enqueueCmd queues download requests. It runs on the root context: leaving a screen never cancels a
// download (app-service.md, concurrency).
func enqueueCmd(e *env, reqs []app.DownloadRequest) tea.Cmd {
	return func() tea.Msg {
		ids, err := e.svc.Enqueue(e.ctx, reqs)
		if err != nil {
			return enqueuedMsg{err: err}
		}
		want := make(map[app.JobID]bool, len(ids))
		for _, id := range ids {
			want[id] = true
		}
		var jobs []app.JobSnapshot
		for _, j := range e.svc.Jobs() {
			if want[j.ID] {
				jobs = append(jobs, j)
			}
		}
		return enqueuedMsg{reqs: reqs, ids: ids, jobs: jobs}
	}
}

// footer is "Page 1/4 · 37 results · 10 per page · cached 3h ago", with the marks and the sort.
func (s resultsScreen) footer(rt *shared) string {
	if s.random || s.st != loadReady {
		return ""
	}
	pageSize := s.q.PageSize
	if pageSize < 1 {
		pageSize = rt.pageSize
	}
	f := rt.T("ui.cli.search.footer", i18n.Args{"page": s.page.Page, "pages": s.page.TotalPages, "total": s.page.TotalResults, "size": pageSize})
	if s.page.Meta.Cached {
		f += " · " + rt.T("ui.cli.search.cached", i18n.Args{"age": age(rt.now().Sub(s.page.Meta.FetchedAt))})
	}
	if s.page.Meta.Stale {
		f += " · " + rt.T("ui.cli.search.stale", nil)
	}
	if n := s.marks.count(s.page.Page); n > 0 {
		f += " · " + rt.T("ui.tui.results.marked", i18n.Args{"count": n})
	}
	if s.q.Sort != "" && s.q.Sort != app.SortRelevance {
		f += " · " + rt.T("ui.tui.results.sorted", i18n.Args{"sort": string(s.q.Sort)})
	}
	return f
}

func (s resultsScreen) view(rt *shared, w, h int) screenView {
	switch s.st {
	case loadLoading:
		return screenView{body: []string{rt.st.spin(rt.spin) + " " + rt.T("ui.tui.results.searching", nil)}}
	case loadError:
		return screenView{body: errorBody(rt, s.fail), status: rt.T("ui.tui.results.retry_help", nil)}
	case loadEmpty:
		return screenView{body: []string{clean(rt.T("ui.tui.results.none", i18n.Args{"text": s.q.Text}))}, status: rt.T("ui.tui.results.edit_help", nil)}
	}
	foot := s.footer(rt)
	if rt.tier == tierShort {
		foot = ""
	}
	sv := screenView{body: s.rowLines(rt, w, h), footer: foot}
	if s.ask != nil {
		sv.status, sv.prompt = render(rt.st.warn, s.ask.text), true
	}
	return sv
}

// errorBody is the in-place error state: the message, the hint and the code.
func errorBody(rt *shared, f failure) []string {
	lines := []string{render(rt.st.bad, rt.T("ui.messages.error", nil)+": "+f.msg)}
	if f.hint != "" {
		lines = append(lines, "  "+f.hint)
	}
	return append(lines, "  "+render(rt.st.dim, "code: "+f.code))
}

// bestLink is the link a row's Links and Size columns describe: the first Game link, else the first.
func bestLink(e mothdex.Entry) mothdex.Link {
	for _, l := range e.Links {
		if l.Type == "Game" {
			return l
		}
	}
	if len(e.Links) > 0 {
		return e.Links[0]
	}
	return mothdex.Link{}
}

// rowLines draws the visible rows with the columns the width allows.
func (s resultsScreen) rowLines(rt *shared, w, h int) []string {
	cols := columns(rt.tier, rt.w)
	has := func(c col) bool {
		for _, x := range cols {
			if x == c {
				return true
			}
		}
		return false
	}
	idxW, platW, regW, linkW, sizeW := 1, 0, 0, 0, 0
	for _, r := range s.rows {
		idxW = max(idxW, len(strconv.Itoa(r.index)))
		platW = max(platW, cells(clean(r.e.Platform)))
		regW = max(regW, cells(clean(strings.Join(r.e.Regions, ","))))
		linkW = max(linkW, len(strconv.Itoa(len(r.e.Links))))
		sizeW = max(sizeW, cells(clean(bestLink(r.e).SizeStr)))
	}
	fixed := 1 + 3 + 2 + idxW + 2 + 2 + platW // cursor, mark, gaps, index, title gap, platform
	if has(colRegions) {
		fixed += 2 + regW
	}
	if has(colLinks) {
		fixed += 2 + linkW
	}
	if has(colSize) {
		fixed += 2 + sizeW
	}
	titleW := max(w-fixed, 8)
	top := scrollTop(s.top, s.cur, h, len(s.rows))
	var out []string
	for i := top; i < len(s.rows) && i < top+h; i++ {
		r := s.rows[i]
		mark := "[ ]"
		if s.marks.has(s.page.Page, r.e.Slug) {
			mark = "[x]"
		}
		idx := "-"
		if r.index > 0 {
			idx = strconv.Itoa(r.index)
		}
		parts := []string{mark, padLeft(idx, idxW), fit(clean(r.e.Title), titleW), fit(clean(r.e.Platform), platW)}
		if has(colRegions) {
			parts = append(parts, fit(clean(strings.Join(r.e.Regions, ",")), regW))
		}
		if has(colLinks) {
			parts = append(parts, padLeft(strconv.Itoa(len(r.e.Links)), linkW))
		}
		if has(colSize) {
			parts = append(parts, padLeft(clean(bestLink(r.e).SizeStr), sizeW))
		}
		marker := " "
		line := strings.Join(parts, "  ")
		if i == s.cur {
			marker = ">"
			line = render(rt.st.on, marker+line)
		} else {
			line = marker + line
		}
		out = append(out, line)
	}
	return out
}
