package gui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/uistate"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const randomCount = 10 // entries a Random pick shows (docs/ux/gui.md section 7)

// resultRow is one row of Results: the entry with what its two lines show.
type resultRow struct {
	slug     string
	title    string
	platform string
	regions  string
	size     int64
	sizeStr  string
}

// resultsState is the Results screen (docs/ux/gui.md section 5.6): a continuous list over a window of service
// pages, and the state of the call that fills it.
type resultsState struct {
	query    app.SearchQuery
	random   bool
	win      window[resultRow]
	list     listState
	phase    loadPhase
	fail     failure
	since    time.Time // when the first call began, for the spinner that waits 200 ms
	nextBusy bool
	prevBusy bool
	seq      int                // a result with another number is from an earlier search and is dropped
	cancel   context.CancelFunc // cancels the first call of the search while it runs
	meta     app.ResultMeta
	total    int
	dropped  int
}

func (s resultsState) focusedSlug() string {
	if s.phase != loadOK || s.list.focus < 0 || s.list.focus >= len(s.win.rows) {
		return ""
	}
	return s.win.rows[s.list.focus].slug
}

func (s resultsState) focusedRow() (resultRow, bool) {
	if s.phase != loadOK || s.list.focus < 0 || s.list.focus >= len(s.win.rows) {
		return resultRow{}, false
	}
	return s.win.rows[s.list.focus], true
}

type (
	searchPageMsg struct {
		seq  int
		page int
		dir  int // 0 the first page of a search, 1 the page after the window, -1 the page before it
		res  app.SearchPage
		err  error
	}
	randomMsg struct {
		seq int
		res app.Result[[]mothdex.Entry]
		err error
	}
)

// bestLink is the link a row speaks for: the first one the API lists, which is what the service itself sorts by
// (app.topSize). The ranking by preference is not exported, so this is an approximation of the top-ranked link
// (docs/ux/gui.md section 5.6); the downloader checks the real size again (cli#75).
func bestLink(e mothdex.Entry) mothdex.Link {
	if len(e.Links) > 0 {
		return e.Links[0]
	}
	return mothdex.Link{}
}

func rowOf(e mothdex.Entry) resultRow {
	l := bestLink(e)
	return resultRow{
		slug: e.Slug, title: app.SanitizeText(e.Title), platform: app.SanitizeText(e.Platform),
		regions: app.SanitizeText(strings.Join(e.Regions, ",")), size: l.Size, sizeStr: app.SanitizeText(l.SizeStr),
	}
}

// startResults opens Results for a query (or Random picks) and starts the first call.
func (m Model) startResults(q app.SearchQuery, random bool, now time.Time) (Model, []Cmd) {
	seq := m.results.seq + 1
	m.results = resultsState{query: q, random: random, win: newWindow[resultRow](servicePageSize), phase: loadBusy, since: now, seq: seq}
	m = m.enter(screenResults)
	m.coverSlug, m.coverSince = "", now
	ctx, cancel := context.WithTimeout(context.Background(), dbInfoTimeout)
	m.results.cancel = cancel
	if random {
		return m, []Cmd{m.randomCmd(ctx, cancel, seq)}
	}
	return m, []Cmd{m.searchCmd(ctx, cancel, seq, q, 1, 0)}
}

// cancelResults stops the call that is running and makes any answer to it stale (B on a wait).
func (m Model) cancelResults() Model {
	if m.results.cancel != nil {
		m.results.cancel()
	}
	m.results.seq++
	return m
}

func (m Model) searchCmd(ctx context.Context, cancel context.CancelFunc, seq int, q app.SearchQuery, page, dir int) Cmd {
	svc := m.o.Service
	q.Page, q.PageSize = page, servicePageSize
	return func() Msg {
		defer cancel()
		res, err := svc.Search(ctx, q)
		return searchPageMsg{seq: seq, page: page, dir: dir, res: res, err: err}
	}
}

// moreCmd is the call for a page next to the window: it has its own timeout and nothing cancels it.
func (m Model) moreCmd(seq int, q app.SearchQuery, page, dir int) Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), dbInfoTimeout)
	return m.searchCmd(ctx, cancel, seq, q, page, dir)
}

func (m Model) randomCmd(ctx context.Context, cancel context.CancelFunc, seq int) Cmd {
	svc, q := m.o.Service, m.results.query
	return func() Msg {
		defer cancel()
		res, err := svc.Random(ctx, app.RandomQuery{Filters: mothdex.Filters{Platforms: q.Platforms, Regions: q.Regions}, Count: randomCount})
		return randomMsg{seq: seq, res: res, err: err}
	}
}

func (m Model) onSearchPage(msg searchPageMsg, now time.Time) (Model, []Cmd) {
	s := m.results
	if msg.seq != s.seq || !m.onStack(screenResults) {
		return m, nil
	}
	switch msg.dir {
	case 1:
		s.nextBusy = false
	case -1:
		s.prevBusy = false
	}
	if msg.err != nil {
		if msg.dir == 0 {
			s.phase, s.fail = loadFailed, m.failureOf(msg.err)
		} else if !canceled(msg.err) {
			m.results = s
			return m.withToast(m.failureOf(msg.err).msg, toastError, now), nil
		}
		m.results = s
		return m, nil
	}
	rows := make([]resultRow, 0, len(msg.res.Results))
	for _, r := range msg.res.Results {
		rows = append(rows, rowOf(r.Entry))
	}
	switch msg.dir {
	case 0:
		s.win = window[resultRow]{rows: rows, first: msg.page, pageSize: servicePageSize, totalPages: msg.res.TotalPages}
		s.phase, s.list = loadOK, listState{}
	case 1:
		// An answer is taken only if its page is still the one after the window: the call ran off the render
		// thread, so the window may have moved since it was asked (PR-76 review, Major 1).
		if msg.page != s.win.lastPage()+1 {
			m.results = s
			return m, nil
		}
		next, dropped := s.win.withNext(rows, msg.res.TotalPages)
		if s.list.focus < dropped { // the rows that would drop off the front hold the focus: ask again on demand
			m.results = s
			return m, nil
		}
		s.win = next
		s.list.focus -= dropped
		s.list.top = max(s.list.top-dropped, 0)
	case -1:
		if msg.page != s.win.first-1 {
			m.results = s
			return m, nil
		}
		next, added := s.win.withPrev(rows)
		if s.list.focus+added >= len(next.rows) { // the rows that would drop off the end hold the focus
			m.results = s
			return m, nil
		}
		s.win = next
		s.list.focus += added
		s.list.top += added
	}
	s.list.focus = clampFocus(s.list.focus, len(s.win.rows))
	s.meta, s.total, s.dropped = msg.res.Meta, msg.res.TotalResults, msg.res.Dropped
	m.results = s
	m = m.noteCoverFocus(now)
	return m.fetchMore()
}

func (m Model) onRandom(msg randomMsg, now time.Time) Model {
	s := m.results
	if msg.seq != s.seq || !m.onStack(screenResults) {
		return m
	}
	if msg.err != nil {
		s.phase, s.fail = loadFailed, m.failureOf(msg.err)
		m.results = s
		return m
	}
	rows := make([]resultRow, 0, len(msg.res.Value))
	for _, e := range msg.res.Value {
		rows = append(rows, rowOf(e))
	}
	s.win = window[resultRow]{rows: rows, first: 1, pageSize: servicePageSize, totalPages: 1}
	s.phase, s.list, s.meta, s.total = loadOK, listState{}, msg.res.Meta, len(rows)
	m.results = s
	return m.noteCoverFocus(now)
}

// fetchMore asks for the page after the window (or before it) when the focus is near its end, once at a time.
func (m Model) fetchMore() (Model, []Cmd) {
	s := m.results
	if s.random || s.phase != loadOK {
		return m, nil
	}
	var cmds []Cmd
	if !s.nextBusy && s.win.wantNext(s.list.focus) {
		s.nextBusy = true
		cmds = append(cmds, m.moreCmd(s.seq, s.query, s.win.lastPage()+1, 1))
	}
	if !s.prevBusy && s.win.wantPrev(s.list.focus) {
		s.prevBusy = true
		cmds = append(cmds, m.moreCmd(s.seq, s.query, s.win.first-1, -1))
	}
	m.results = s
	return m, cmds
}

func (m Model) resultsVisible() int {
	return max(m.resultsLayout().list.H/m.metrics.Row2, 1)
}

// resultsGeom is where things are on Results: the list, the cover panel when the tier and width have room, and
// the footer line.
type resultsGeom struct {
	list   draw.Rect
	panel  draw.Rect
	hasPan bool
	footer draw.Rect
}

func (m Model) resultsLayout() resultsGeom {
	met := m.metrics
	c := met.ContentRect()
	g := resultsGeom{}
	foot := LineHeight(met.Secondary)
	g.footer = draw.Rect{X: c.X, Y: c.Bottom() - foot, W: c.W, H: foot}
	body := draw.Rect{X: c.X, Y: c.Y, W: c.W, H: c.H - foot - met.Unit}
	if met.Tier != Compact && !met.Narrow {
		w := c.W * 35 / 100
		g.hasPan = true
		g.panel = draw.Rect{X: body.Right() - w, Y: body.Y, W: w, H: body.H}
		body.W -= w + 2*met.Unit
	}
	g.list = body
	return g
}

func (m Model) onResultsAction(a input.Action, now time.Time) (Model, []Cmd) {
	s := m.results
	if s.phase == loadFailed {
		switch a {
		case input.Confirm: // a retry is a new search on a fresh screen, not one more screen on the stack
			m = m.leave()
			return m.startResults(s.query, s.random, now)
		case input.Back:
			return m.leave(), nil
		}
		return m, nil
	}
	if s.phase != loadOK {
		if a == input.Back { // B cancels the wait and goes back
			return m.cancelResults().leave(), nil
		}
		return m, nil
	}
	n := len(s.win.rows)
	vis := m.resultsVisible()
	switch a {
	case input.Up, input.Down:
		delta := 1
		if a == input.Up {
			delta = -1
		}
		s.list = s.list.moved(delta, vis, n)
	case input.PageUp:
		s.list = s.list.paged(-1, vis, n)
	case input.PageDown:
		s.list = s.list.paged(1, vis, n)
	case input.Confirm:
		if row, ok := s.focusedRow(); ok {
			return m.openEntry(row, now)
		}
	case input.Download:
		if row, ok := s.focusedRow(); ok {
			return m.requestDownload(download{slug: row.slug, title: row.title, platform: row.platform, size: row.size}, now)
		}
	case input.Info:
		if s.random {
			return m.startResultsFresh(now)
		}
		return m.editSearch(), nil
	case input.Back:
		return m.cancelResults().leave(), nil
	}
	m.results = s
	m = m.noteCoverFocus(now)
	return m.fetchMore()
}

// startResultsFresh asks for another set of random picks without piling screens up.
func (m Model) startResultsFresh(now time.Time) (Model, []Cmd) {
	m = m.leave()
	return m.startResults(m.results.query, true, now)
}

// editSearch is Y on Results: back to Search with the query and filters filled.
func (m Model) editSearch() Model {
	q := m.results.query
	m.search.field = editField{text: []rune(q.Text), cursor: len([]rune(q.Text))}
	m.search.platforms, m.search.regions = slices.Clone(q.Platforms), slices.Clone(q.Regions)
	if len(m.back) > 0 && m.back[len(m.back)-1] == screenSearch {
		m = m.leave()
	} else {
		m = m.replaceTop(screenSearch)
	}
	m.search.focus = keySearch
	m.search.msg = ""
	return m
}

// rememberQuery moves the query to the front of the recent searches and saves them where the TUI keeps theirs.
func (m Model) rememberQuery(text string) (Model, []Cmd) {
	recent := []string{text}
	for _, r := range m.recent {
		if r != text && len(recent) < maxRecent {
			recent = append(recent, r)
		}
	}
	m.recent = recent
	dir := m.o.DataDir
	if dir == "" {
		return m, nil
	}
	return m, []Cmd{func() Msg {
		body, err := json.Marshal(struct {
			Recent []string `json:"recent"`
		}{recent})
		if err == nil {
			err = uistate.WriteAtomic(filepath.Join(dir, stateFile), body)
		}
		if err != nil {
			return nil
		}
		return nil
	}}
}

func (m Model) resultsFrame() frameSpec {
	s := m.results
	spec := frameSpec{}
	if s.random {
		spec.crumbs = []string{m.tr("ui.gui.results.random_title", nil)}
	} else {
		spec.crumbs = []string{m.tr("ui.gui.screen.results", nil), m.querySummary()}
	}
	back := m.tr("ui.gui.hint.back", nil)
	switch {
	case s.phase == loadFailed:
		spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.retry", nil)}, {role: input.Back, label: back}}
	case s.phase != loadOK:
		spec.hints = []hint{{role: input.Back, label: back}}
	default:
		edit := m.tr("ui.gui.hint.edit_search", nil)
		if s.random {
			edit = m.tr("ui.gui.hint.new_picks", nil)
		}
		spec.hints = []hint{
			{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.page", nil), optional: true},
			{role: input.Confirm, label: m.tr("ui.gui.hint.open", nil)},
			{role: input.Download, label: m.tr("ui.gui.hint.download", nil)},
			{role: input.Info, label: edit},
			{role: input.Back, label: back},
		}
		if len(s.win.rows) == 0 && !s.random {
			spec.hints = []hint{{role: input.Info, label: edit}, {role: input.Back, label: back}}
		}
	}
	return spec
}

// querySummary is `"zelda" · snes` for the top bar.
func (m Model) querySummary() string {
	q := m.results.query
	var parts []string
	if q.Text != "" {
		parts = append(parts, `"`+app.SanitizeText(q.Text)+`"`)
	}
	if len(q.Platforms) > 0 {
		parts = append(parts, app.SanitizeText(strings.Join(q.Platforms, ", ")))
	}
	if len(q.Regions) > 0 {
		parts = append(parts, app.SanitizeText(strings.Join(q.Regions, ", ")))
	}
	return strings.Join(parts, " · ")
}

// footerLine is "37 results · cached 3h ago", or the stale notice in warn, and what was hidden.
func (m Model) footerLine() (string, draw.Color) {
	s := m.results
	text := m.o.T.N("ui.gui.results.count", s.total, i18n.Args{"count": s.total})
	color := colDim
	switch {
	case s.meta.Stale:
		text += " · " + m.tr("ui.gui.results.stale", nil)
		color = colWarn
	case s.meta.Cached && !s.meta.FetchedAt.IsZero():
		text += " · " + m.tr("ui.gui.results.cached", map[string]any{"age": age(m.o.Clock.Now().Sub(s.meta.FetchedAt))})
	}
	if s.dropped > 0 {
		text += " · " + m.o.T.N("ui.gui.results.dropped", s.dropped, i18n.Args{"count": s.dropped})
	}
	return text, color
}

func (m Model) drawResults(p painter, now time.Time) {
	met := m.metrics
	s := m.results
	g := m.resultsLayout()
	switch s.phase {
	case loadBusy:
		key := "ui.gui.searching"
		if s.random {
			key = "ui.gui.loading"
		}
		m.drawLoading(p, g.list, m.tr(key, nil), s.since, now)
		return
	case loadFailed:
		m.drawFailure(p, g.list, s.fail, true)
		return
	}
	if len(s.win.rows) == 0 {
		msg := m.tr("ui.gui.results.empty", map[string]any{"text": app.SanitizeText(s.query.Text)})
		for i, line := range p.wrap(met.Body, msg, g.list.W, 3) {
			p.text(g.list.X, g.list.Y+i*LineHeight(met.Body), met.Body, colText, line)
		}
		return
	}
	vis := m.resultsVisible()
	l, from, to := s.list.shown(vis, len(s.win.rows))
	for i := from; i < to; i++ {
		r := draw.Rect{X: g.list.X, Y: g.list.Y + (i-from)*met.Row2, W: g.list.W, H: met.Row2}
		m.drawResultRow(p, r, s.win.rows[i], i == l.focus)
	}
	if s.nextBusy && to == len(s.win.rows) { // a "Loading more…" row at the end while the next page comes
		p.text(g.list.X+2*met.Unit, g.list.Y+min(to-from, vis-1)*met.Row2, met.Secondary, colDim, m.tr("ui.gui.results.more", nil))
	}
	text, color := m.footerLine()
	p.text(g.footer.X, g.footer.Y, met.Secondary, color, p.fit(met.Secondary, text, g.footer.W))
	if g.hasPan {
		if row, ok := s.focusedRow(); ok {
			cover := draw.Rect{X: g.panel.X, Y: g.panel.Y, W: g.panel.W, H: g.panel.H - 2*LineHeight(met.Body) - met.Unit}
			m.drawCover(p, cover, row.slug, row.platform)
			for i, line := range p.wrap(met.Body, row.title, g.panel.W, 2) {
				p.text(g.panel.X, cover.Bottom()+met.Unit+i*LineHeight(met.Body), met.Body, colText, line)
			}
		}
	}
}

// drawResultRow draws one two-line row: the title, then the platform, regions and size, and the downloaded mark.
func (m Model) drawResultRow(p painter, r draw.Rect, row resultRow, focused bool) {
	met := m.metrics
	box := r.Inset(1) // the two lines fill the row: the ring is drawn just outside this box
	if focused {
		p.box(box, true)
	}
	x := box.X + met.Unit
	inner := box.W - 2*met.Unit
	p.text(x, box.Y+1, met.Body, colText, p.fit(met.Body, row.title, inner))
	meta := strings.Join(nonEmpty(row.platform, row.regions, row.sizeStr), " · ")
	top := box.Y + 1 + LineHeight(met.Body)
	markW := 0
	if m.slugDownloaded(row.slug) {
		word := m.tr("ui.gui.results.downloaded", nil)
		cs := LineHeight(met.Secondary) - 6
		markW = p.width(met.Secondary, word) + cs + met.Unit/2 + met.Unit
		end := box.Right() - met.Unit
		wordX := end - p.width(met.Secondary, word)
		p.text(wordX, top, met.Secondary, colOK, word)
		drawCheck(p.l, p.m.Pixel, wordX-cs-met.Unit/2, top+3, cs, colOK) // one space between the mark and its word
	}
	p.text(x, top, met.Secondary, colDim, p.fit(met.Secondary, meta, inner-markW))
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// drawLoading draws the spinner and the text of a wait. The spinner appears only after 200 ms, so cached
// answers never flash it.
func (m Model) drawLoading(p painter, r draw.Rect, text string, since, now time.Time) {
	met := m.metrics
	top := r.Y + met.Unit
	x := r.X + met.Unit
	p.text(x, top, met.Body, colDim, text)
	if spinnerShown(since, now) {
		lh := LineHeight(met.Body)
		p.spinner(x+p.width(met.Body, text)+met.Unit+p.spinnerSize(lh)/2, top+lh/2, lh, now)
	}
}

// drawFailure draws an error in place (docs/ux/gui.md section 6): the icon, the message, the hint, the code
// for support, and the Retry button when the call can be retried.
func (m Model) drawFailure(p painter, r draw.Rect, f failure, retry bool) {
	met := m.metrics
	y := r.Y + met.Unit
	bang := draw.Rect{X: r.X + met.Unit, Y: y, W: LineHeight(met.Body), H: LineHeight(met.Body)}
	p.centered(bang, met.Body, colError, "!") // the cross marks failed downloads; an error in place is a !
	x := r.X + met.Unit + LineHeight(met.Body) + met.Unit
	w := r.Right() - x
	for _, line := range p.wrap(met.Body, f.msg, w, 3) {
		p.text(x, y, met.Body, colError, line)
		y += LineHeight(met.Body)
	}
	if f.hint != "" {
		for _, line := range p.wrap(met.Secondary, f.hint, w, 4) {
			p.text(x, y, met.Secondary, colText, line)
			y += LineHeight(met.Secondary)
		}
	}
	p.text(x, y, met.Secondary, colDim, "code: "+f.code)
	y += LineHeight(met.Secondary) + met.Unit
	if retry && f.retryable {
		label := m.tr("ui.gui.hint.retry", nil)
		btn := draw.Rect{X: x, Y: y, W: p.width(met.Body, label) + 4*met.Unit, H: met.Row1}
		p.box(btn, true)
		p.centered(btn, met.Body, colText, label)
	}
}
