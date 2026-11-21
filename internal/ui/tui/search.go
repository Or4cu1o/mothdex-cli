package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/uistate"
)

const (
	stateFile    = "tui_state"
	maxRecent    = 5
	randomCount  = 10 // entries a Random pick shows (tui.md section 2)
	queryWidth   = 40
	manualWidth  = 24
	labelWidth   = 9
	fieldGap     = "   "
	anyTextWidth = 10 // room for the platform or region text inside its brackets
)

// field is a stop of the Search screen's focus ring.
type field int

const (
	fQuery field = iota
	fPlatform
	fRegion
	fSort
	fRecent
	fRandom
)

var fieldRing = []field{fQuery, fPlatform, fRegion, fSort, fRandom}

// listState is a platform or region list: loaded on first use, kept for the run.
type listState struct {
	items   []pickItem
	loaded  bool
	failed  bool
	loading bool
}

// choice is a multi-select field: ids picked from a list, or typed by hand when the list cannot load.
type choice struct {
	ids    []string
	list   listState
	free   textinput.Model
	manual bool
}

func newChoice() choice {
	f := textinput.New()
	f.Prompt = ""
	f.SetWidth(manualWidth)
	return choice{free: f}
}

// values are the ids the field stands for: the picked ones, or the comma-separated ones typed.
func (c choice) values() []string {
	if c.manual {
		return splitIDs(c.free.Value())
	}
	return c.ids
}

// splitIDs reads a comma- or space-separated list of ids.
func splitIDs(s string) []string {
	out := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	if len(out) == 0 {
		return nil
	}
	return out
}

// searchScreen is the home screen (tui.md section 2).
type searchScreen struct {
	query    textinput.Model
	plat     choice
	reg      choice
	sort     app.SortKey
	at       field
	recent   []string
	recentAt int
	picker   *picker
	pickFor  field
	needText bool
}

func newSearch(recent []string) searchScreen {
	q := textinput.New()
	q.Prompt = ""
	q.SetWidth(queryWidth)
	q.Focus()
	return searchScreen{query: q, plat: newChoice(), reg: newChoice(), sort: app.SortRelevance, recent: recent}
}

func (searchScreen) id() screenID                     { return scSearch }
func (searchScreen) loading() bool                    { return false }
func (s searchScreen) init(*shared) (screen, tea.Cmd) { return s, nil }
func (searchScreen) title(rt *shared) string          { return rt.T("ui.tui.screen.search", nil) }
func (s *searchScreen) choiceAt(f field) *choice {
	if f == fRegion {
		return &s.reg
	}
	return &s.plat
}

// toQuery reads the fields as a search.
func (s searchScreen) toQuery() app.SearchQuery {
	return app.SearchQuery{Text: strings.TrimSpace(s.query.Value()), Platforms: s.plat.values(), Regions: s.reg.values(), Sort: s.sort}
}

func (s searchScreen) focus() focusKind {
	switch {
	case s.picker != nil || s.at == fQuery:
		return focusText
	case s.at == fPlatform && s.plat.manual, s.at == fRegion && s.reg.manual:
		return focusText
	}
	return focusNone
}

// listMsg carries a loaded platform or region list.
type listMsg struct {
	field field
	items []pickItem
	err   error
}

func (s searchScreen) update(msg tea.Msg, rt *shared) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case listMsg:
		return s.onList(msg)
	case fillMsg:
		return s.fill(msg.q), nil
	case keyEv:
		return s.onKey(msg, rt)
	}
	return s, nil
}

// fill puts a query back into the fields ("/" from Results).
func (s searchScreen) fill(q app.SearchQuery) searchScreen {
	s.query.SetValue(q.Text)
	s.plat.ids, s.reg.ids, s.sort = q.Platforms, q.Regions, q.Sort
	if s.sort == "" {
		s.sort = app.SortRelevance
	}
	if s.plat.manual {
		s.plat.free.SetValue(strings.Join(q.Platforms, ","))
	}
	if s.reg.manual {
		s.reg.free.SetValue(strings.Join(q.Regions, ","))
	}
	s.focusField(fQuery)
	return s
}

// focusField moves the focus to a stop and gives the text input its cursor.
func (s *searchScreen) focusField(f field) {
	s.query.Blur()
	s.plat.free.Blur()
	s.reg.free.Blur()
	s.at = f
	switch {
	case f == fQuery:
		s.query.Focus()
	case f == fPlatform && s.plat.manual:
		s.plat.free.Focus()
	case f == fRegion && s.reg.manual:
		s.reg.free.Focus()
	}
}

func (s searchScreen) onList(msg listMsg) (screen, tea.Cmd) {
	c := s.choiceAt(msg.field)
	list := &c.list
	list.loading = false
	if msg.err != nil {
		list.failed = true
		if s.picker != nil && s.pickFor == msg.field { // the list cannot load: type the ids instead
			s.picker = nil
			c.manual = true
			s.focusField(msg.field)
		}
		return s, nil
	}
	list.items, list.loaded = msg.items, true
	if s.picker != nil && s.pickFor == msg.field {
		p := s.picker.withItems(msg.items)
		s.picker = &p
	}
	return s, nil
}

func (s searchScreen) onKey(ev keyEv, rt *shared) (screen, tea.Cmd) {
	if s.picker != nil {
		return s.onPickerKey(ev.k)
	}
	switch ev.a {
	case actText:
		return s.typeInto(ev.k)
	case actNextField, actPrevField:
		return s.cycle(ev.a == actNextField), nil
	case actBlur:
		s.focusField(fRandom)
		return s, nil
	case actDown:
		return s.down(), nil
	case actUp:
		return s.up(), nil
	case actLeft, actRight, actToggle:
		if s.at == fSort {
			s.sort = nextSort(s.sort)
		}
		return s, nil
	case actRandom:
		return s.run(rt, true)
	case actEnter:
		return s.enter(rt)
	}
	return s, nil
}

// typeInto sends a typed key to the text input that has the focus.
func (s searchScreen) typeInto(k tea.KeyPressMsg) (screen, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case s.at == fQuery:
		s.query, _ = s.query.Update(k) // the cursor does not blink: its timer command is dropped
		s.needText = false
	case s.at == fPlatform && s.plat.manual:
		s.plat.free, _ = s.plat.free.Update(k)
	case s.at == fRegion && s.reg.manual:
		s.reg.free, _ = s.reg.free.Update(k)
	}
	return s, cmd
}

// cycle moves the focus around the ring with Tab and Shift+Tab.
func (s searchScreen) cycle(forward bool) searchScreen {
	i := 0
	for n, f := range fieldRing {
		if f == s.at || (s.at == fRecent && f == fQuery) {
			i = n
		}
	}
	step := len(fieldRing) - 1
	if forward {
		step = 1
	}
	s.focusField(fieldRing[(i+step)%len(fieldRing)])
	return s
}

// down: from the query into the recent list, down inside it, or to the next field.
func (s searchScreen) down() searchScreen {
	switch {
	case s.at == fQuery && len(s.recent) > 0:
		s.query.Blur()
		s.at, s.recentAt = fRecent, 0
	case s.at == fRecent:
		s.recentAt = min(s.recentAt+1, len(s.recent)-1)
	case s.at != fQuery:
		return s.cycle(true)
	}
	return s
}

func (s searchScreen) up() searchScreen {
	switch {
	case s.at == fRecent && s.recentAt > 0:
		s.recentAt--
	case s.at == fRecent:
		s.focusField(fQuery)
	case s.at != fQuery:
		return s.cycle(false)
	}
	return s
}

// enter: run the search from the query or the sort, open the picker on a list field, press Random,
// or run a recent query.
func (s searchScreen) enter(rt *shared) (screen, tea.Cmd) {
	switch s.at {
	case fPlatform, fRegion:
		return s.openPicker(rt)
	case fRandom:
		return s.run(rt, true)
	case fRecent:
		s.query.SetValue(s.recent[s.recentAt])
	}
	return s.run(rt, false)
}

func (s searchScreen) openPicker(rt *shared) (screen, tea.Cmd) {
	c := s.choiceAt(s.at)
	if c.manual { // already typing by hand: Enter runs the search
		return s.run(rt, false)
	}
	title := rt.T("ui.tui.search.platform", nil)
	if s.at == fRegion {
		title = rt.T("ui.tui.search.region", nil)
	}
	p := newPicker(title, c.list.items, c.ids, !c.list.loaded)
	s.picker, s.pickFor = &p, s.at
	if c.list.loaded || c.list.loading {
		return s, nil
	}
	c.list.loading = true
	return s, loadList(rt.env, s.at)
}

// loadList fetches the platform or region ids and names, once.
func loadList(e *env, f field) tea.Cmd {
	return func() tea.Msg {
		var items []pickItem
		if f == fRegion {
			res, err := e.svc.Regions(e.ctx)
			if err != nil {
				return listMsg{field: f, err: err}
			}
			for _, r := range res.Value {
				items = append(items, pickItem{id: clean(r.ID), label: clean(r.Name)})
			}
		} else {
			res, err := e.svc.Platforms(e.ctx, app.PlatformFilter{})
			if err != nil {
				return listMsg{field: f, err: err}
			}
			for _, p := range res.Value {
				items = append(items, pickItem{id: clean(p.ID), label: clean(p.Name)})
			}
		}
		return listMsg{field: f, items: items}
	}
}

func (s searchScreen) onPickerKey(k tea.KeyPressMsg) (screen, tea.Cmd) {
	p, res := s.picker.handle(k)
	s.picker = &p
	switch res {
	case pickCancel:
		s.picker = nil
	case pickConfirm:
		s.choiceAt(s.pickFor).ids = p.selected()
		s.picker = nil
	}
	return s, nil
}

// run validates the fields and opens Results, or Random picks. A search needs text or a filter.
func (s searchScreen) run(rt *shared, random bool) (screen, tea.Cmd) {
	q := s.toQuery()
	q.Page, q.PageSize = 1, rt.pageSize
	if !random && q.Text == "" && len(q.Platforms) == 0 && len(q.Regions) == 0 {
		s.needText = true
		return s, nil
	}
	s.needText = false
	cmds := []tea.Cmd{pushCmd(newResults(q, random))}
	if q.Text != "" && !random {
		s.recent = addRecent(s.recent, q.Text)
		cmds = append(cmds, saveState(rt.env, s.recent))
	}
	return s, tea.Batch(cmds...)
}

func pushCmd(s screen) tea.Cmd { return func() tea.Msg { return pushMsg{s: s} } }

// nextSort cycles relevance, title, size (tui.md section 3).
func nextSort(k app.SortKey) app.SortKey {
	switch k {
	case app.SortRelevance:
		return app.SortTitle
	case app.SortTitle:
		return app.SortSize
	}
	return app.SortRelevance
}

// addRecent puts a query first, drops an earlier copy, and keeps the last maxRecent.
func addRecent(recent []string, q string) []string {
	out := []string{q}
	for _, r := range recent {
		if r != q && len(out) < maxRecent {
			out = append(out, r)
		}
	}
	return out
}

// uiState is the file `<paths.data>/tui_state` (ui.md section 1).
type uiState struct {
	Recent []string `json:"recent"`
}

// readState reads the recent queries; a missing or unreadable file is an empty list.
func readState(dir string) []string {
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, stateFile)) //nolint:gosec // the UI's own file under paths.data
	if err != nil {
		return nil
	}
	var st uiState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	out := make([]string, 0, maxRecent)
	for _, r := range st.Recent {
		if r = clean(r); r != "" && len(out) < maxRecent {
			out = append(out, r)
		}
	}
	return out
}

// saveState writes the recent queries. A failure is logged and never shown (ui.md section 1).
func saveState(e *env, recent []string) tea.Cmd {
	return func() tea.Msg {
		if e.dataDir == "" {
			return nil
		}
		data, err := json.Marshal(uiState{Recent: recent})
		if err == nil {
			err = uistate.WriteAtomic(filepath.Join(e.dataDir, stateFile), data)
		}
		if err != nil && e.log != nil {
			e.log.Warn("cannot write the TUI state", "err", err)
		}
		return nil
	}
}

func (s searchScreen) view(rt *shared, w, h int) screenView {
	if s.picker != nil {
		return s.pickerView(rt, w, h)
	}
	st := rt.st
	platform := s.label(rt, "ui.tui.search.platform") + s.choiceView(rt, fPlatform)
	region := rt.T("ui.tui.search.region", nil) + " " + s.choiceView(rt, fRegion)
	sort := rt.T("ui.tui.search.sort", nil) + " " + s.bracket(rt, s.at == fSort, string(s.sort)+" "+st.g.down)
	lines := []string{s.label(rt, "ui.tui.search.search") + s.bracket(rt, s.at == fQuery, s.query.View())}
	if joined := platform + fieldGap + region + fieldGap + sort; cells(joined) <= w {
		lines = append(lines, joined)
	} else { // a narrow terminal: one field to a line
		lines = append(lines, platform, s.label(rt, "ui.tui.search.region")+s.choiceView(rt, fRegion), s.label(rt, "ui.tui.search.sort")+s.bracket(rt, s.at == fSort, string(s.sort)+" "+st.g.down))
	}
	lines = append(lines, "")
	if s.needText {
		lines[len(lines)-1] = render(st.warn, rt.T("ui.tui.search.need_text", nil))
	}
	if rt.tier != tierShort && len(s.recent) > 0 {
		lines = append(lines, s.label(rt, "ui.tui.search.recent")+s.recentView(rt))
	}
	lines = append(lines, "", s.infoLine(rt, w))
	return screenView{body: lines}
}

func (s searchScreen) label(rt *shared, key string) string {
	return fit(rt.T(key, nil), labelWidth)
}

// bracket draws a field as [ text ], in reverse video when it has the focus.
func (s searchScreen) bracket(rt *shared, focused bool, text string) string {
	b := "[ " + text + " ]"
	if focused {
		return render(rt.st.on, b)
	}
	return b
}

// choiceView shows the ids picked for a list field, or the text being typed.
func (s searchScreen) choiceView(rt *shared, f field) string {
	c := s.plat
	if f == fRegion {
		c = s.reg
	}
	focused := s.at == f
	if c.manual {
		return s.bracket(rt, focused, c.free.View())
	}
	text := rt.T("ui.tui.search.any", nil)
	if len(c.ids) > 0 {
		text = cutTo(strings.Join(c.ids, ", "), anyTextWidth)
	}
	return s.bracket(rt, focused, fit(text, anyTextWidth)+" "+rt.st.g.down)
}

func (s searchScreen) recentView(rt *shared) string {
	parts := make([]string, len(s.recent))
	for i, r := range s.recent {
		parts[i] = r
		if s.at == fRecent && i == s.recentAt {
			parts[i] = render(rt.st.on, "> "+r)
		}
	}
	return strings.Join(parts, " · ")
}

// infoLine is the entry count and profile on the left and the Random button on the right.
func (s searchScreen) infoLine(rt *shared, w int) string {
	profile := clean(rt.svc.ActiveProfile().ID)
	left := rt.T("ui.tui.search.profile", i18n.Args{"profile": profile})
	if rt.dbKnown {
		left = rt.N("ui.tui.search.entries", int(rt.dbTotal), i18n.Args{"count": rt.num.Sprintf("%d", rt.dbTotal)}) + " · " + left
	}
	button := s.bracket(rt, s.at == fRandom, rt.T("ui.tui.search.random", nil))
	gap := w - cells(left) - cells(button)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + button
}

func (s searchScreen) pickerView(rt *shared, w, h int) screenView {
	p := *s.picker
	lines := []string{p.title + "  " + rt.T("ui.tui.picker.filter", nil) + " [ " + p.filter.View() + " ]"}
	switch vis := p.visible(); {
	case p.loading:
		lines = append(lines, "", rt.T("ui.tui.loading", nil))
	case len(vis) == 0:
		lines = append(lines, "", render(rt.st.dim, rt.T("ui.tui.picker.none", nil)))
	default:
		room := max(h-2, 1)
		top := scrollTop(0, p.cur, room, len(vis))
		for i := top; i < len(vis) && i < top+room; i++ {
			mark := "[ ]"
			if p.marked[vis[i].id] {
				mark = "[x]"
			}
			row := mark + " " + fit(vis[i].id, 8) + " " + vis[i].label
			if i == p.cur {
				row = render(rt.st.on, "> "+fit(row, max(w-2, 1)))
			} else {
				row = "  " + row
			}
			lines = append(lines, row)
		}
	}
	return screenView{body: lines, status: rt.T("ui.tui.picker.help", nil)}
}
