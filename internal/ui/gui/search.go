package gui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// searchState is the Search screen: the text with its cursor, the keyboard layout, the filters, and where the
// focus is (a key or a chip).
type searchState struct {
	field     editField
	layout    int // index into layoutCycle
	platforms []string
	regions   []string
	focus     string
	msg       string // an inline message under the field
}

// Ids of the chips above the keyboard.
const (
	chipPlatform   = "chip:platform"
	chipRegion     = "chip:region"
	chipSearch     = "chip:search"
	regionChipsTop = 0 // the chips are region 0 of this screen, the keys region 2
)

// firstKey is the key that gets focus when the screen opens empty: q.
var firstKey = gridKeyID(1, 0)

// enterSearch opens the Search screen. Its text and filters are kept from the last visit; focus starts on q for
// an empty field and on the Search key when there is text (re-editing from Results).
func (m Model) enterSearch() Model {
	if m.screen != screenSearch {
		m = m.enter(screenSearch)
	}
	m.search.focus = firstKey
	if len(m.search.field.text) > 0 {
		m.search.focus = keySearch
	}
	m.search.msg = ""
	return m
}

func (m Model) cycle() []kbLayout { return layoutCycle(m.o.T.Locale(), m.search.field.text) }

func (m Model) currentLayout() kbLayout {
	c := m.cycle()
	return c[min(m.search.layout, len(c)-1)]
}

func (m Model) nextLayout() kbLayout {
	c := m.cycle()
	return c[(min(m.search.layout, len(c)-1)+1)%len(c)]
}

// searchGeom is where everything on the Search screen is.
type searchGeom struct {
	field    draw.Rect
	counter  string
	msg      draw.Rect
	chips    []chipBox
	kb       kbGeometry
	elements []Element
}

func (m Model) searchGeometry() searchGeom {
	met := m.metrics
	p := m.painter(nil)
	c := met.ContentRect()
	gap := met.Unit
	g := searchGeom{}

	y := c.Y
	g.field = draw.Rect{X: c.X, Y: y, W: c.W, H: met.Row1}
	y += met.Row1 + gap
	if m.search.msg != "" {
		g.msg = draw.Rect{X: c.X, Y: y, W: c.W, H: LineHeight(met.Secondary)}
		y += g.msg.H + gap
	}

	chipH := met.Row1
	x := c.X
	for _, ch := range []struct{ id, label string }{
		{chipPlatform, m.filterLabel("ui.gui.search.platform", m.search.platforms)},
		{chipRegion, m.filterLabel("ui.gui.search.region", m.search.regions)},
		{chipSearch, m.tr("ui.gui.kb.search", nil)},
	} {
		w := min(p.width(met.Body, ch.label)+4*gap, max(c.Right()-x, 4*gap))
		label := p.fit(met.Body, ch.label, w-4*gap)
		r := draw.Rect{X: x, Y: y, W: w, H: chipH}
		g.chips = append(g.chips, chipBox{id: ch.id, label: label, rect: r})
		g.elements = append(g.elements, Element{ID: ch.id, Rect: r, Region: regionChipsTop})
		x += w + gap
	}
	y += chipH + gap

	g.kb = newKBGeometry(draw.Rect{X: c.X, Y: y, W: c.W, H: c.Bottom() - y}, met.Key, gap)
	g.elements = append(g.elements, g.kb.keys...)
	g.counter = strconv.Itoa(len(m.search.field.text)) + "/" + strconv.Itoa(maxQueryRunes)
	return g
}

// filterLabel is "Platform: snes, gbc" or "Platform: any".
func (m Model) filterLabel(key string, ids []string) string {
	list := m.tr("ui.gui.search.any", nil)
	if len(ids) > 0 {
		list = strings.Join(ids, ", ")
	}
	return m.tr(key, map[string]any{"list": list})
}

// keyLabel is what a key shows.
func (m Model) keyLabel(id string) string {
	switch id {
	case keyLayout:
		return m.nextLayout().name
	case keySpace:
		return m.tr("ui.gui.kb.space", nil)
	case keyDelete:
		return m.tr("ui.gui.kb.delete", nil)
	case keySearch:
		return m.tr("ui.gui.kb.search", nil)
	}
	return keyText(m.currentLayout(), id)
}

func (m Model) onSearchAction(a input.Action, repeat bool, now time.Time) (Model, []Cmd) {
	s := m.search
	s.msg = ""
	if d, ok := dirOf(a); ok {
		s.focus = Navigate(m.searchGeometry().elements, s.focus, d)
		m.search = s
		return m, nil
	}
	switch a {
	case input.Confirm:
		return m.activateSearch(s.focus, now)
	case input.Back:
		if len(s.field.text) > 0 {
			s.field, _ = s.field.backspace()
			m.search = s
			return m, nil
		}
		if repeat { // a held B that deleted the last character must not also leave the screen
			return m, nil
		}
		return m.leave(), nil
	case input.Delete:
		s.field, _ = s.field.backspace()
	case input.Download:
		s.field, _ = s.field.insert(" ")
	case input.Info:
		s.layout = (min(s.layout, len(m.cycle())-1) + 1) % len(m.cycle())
	case input.PageUp:
		s.field = s.field.moved(-1)
	case input.PageDown:
		s.field = s.field.moved(1)
	case input.Menu:
		m.search = s
		return m.runSearch(now)
	}
	m.search = s
	return m, nil
}

// activateSearch is A on the focused key or chip.
func (m Model) activateSearch(id string, now time.Time) (Model, []Cmd) {
	s := m.search
	switch id {
	case chipPlatform:
		return m.openPicker(pickPlatform, false, now)
	case chipRegion:
		return m.openPicker(pickRegion, false, now)
	case chipSearch, keySearch:
		return m.runSearch(now)
	case keyLayout:
		s.layout = (min(s.layout, len(m.cycle())-1) + 1) % len(m.cycle())
	case keySpace:
		s.field, _ = s.field.insert(" ")
	case keyDelete:
		s.field, _ = s.field.backspace()
	default:
		if key := keyText(m.currentLayout(), id); key != "" {
			s.field, _ = s.field.insert(key)
		}
	}
	m.search = s
	return m, nil
}

// onText types what a physical keyboard sent into the field, lowercased (search is case-insensitive).
func (m Model) onText(txt string) Model {
	if !m.TextMode() {
		return m
	}
	m.search.field, _ = m.search.field.insert(strings.ToLower(txt))
	m.search.msg = ""
	return m
}

// searchQuery is the query of the screen as the service takes it.
func (m Model) searchQuery() (text string, platforms, regions []string) {
	return strings.TrimSpace(m.search.field.String()), slices.Clone(m.search.platforms), slices.Clone(m.search.regions)
}

// runSearch checks the query and opens Results. With no text and no filters it says so under the field and calls
// nothing (the same rule as the TUI).
func (m Model) runSearch(now time.Time) (Model, []Cmd) {
	text, platforms, regions := m.searchQuery()
	if text == "" && len(platforms) == 0 && len(regions) == 0 {
		m.search.msg = m.tr("ui.gui.search.empty", nil)
		return m, nil
	}
	m, cmds := m.startResults(app.SearchQuery{Text: text, Platforms: platforms, Regions: regions}, false, now)
	if text != "" {
		var more []Cmd
		m, more = m.rememberQuery(text)
		cmds = append(cmds, more...)
	}
	return m, cmds
}

func (m Model) searchHints() []hint {
	typeWord := m.tr("ui.gui.hint.type", nil)
	if m.search.field.full() && keyText(m.currentLayout(), m.search.focus) != "" {
		typeWord = m.tr("ui.gui.hint.unavailable", nil)
	}
	back := m.tr("ui.gui.hint.delete", nil)
	if len(m.search.field.text) == 0 {
		back = m.tr("ui.gui.hint.back", nil)
	}
	return []hint{
		{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.cursor", nil), optional: true},
		{role: input.Confirm, label: typeWord},
		{role: input.Download, label: m.tr("ui.gui.hint.space", nil)},
		{role: input.Info, label: m.tr("ui.gui.hint.layout", nil)},
		{role: input.Back, label: back},
	}
}

func (m Model) drawSearch(p painter) {
	met := m.metrics
	g := m.searchGeometry()
	u := met.Unit

	// The field: the text, a cursor, and the counter on the right.
	p.l.Fill(g.field, met.Radius, colSurface)
	counterW := p.width(met.Secondary, g.counter)
	counterColor := colDim
	if len(m.search.field.text) >= counterWarnAt {
		counterColor = colWarn
	}
	textW := g.field.W - 4*u - counterW - u
	shown, caret := fieldWindow(m.search.field.text, m.search.field.cursor, textW, func(s string) int { return p.width(met.Body, s) })
	top := g.field.Y + (g.field.H-LineHeight(met.Body))/2
	p.text(g.field.X+2*u, top, met.Body, colText, shown)
	p.l.Fill(draw.Rect{X: g.field.X + 2*u + caret, Y: top + 2, W: 2, H: LineHeight(met.Body) - 4}, 0, colAccent)
	p.rightAligned(g.field.Right()-2*u, g.field.Y, g.field.H, met.Secondary, counterColor, g.counter)
	if m.search.msg != "" {
		p.text(g.msg.X+u, g.msg.Y, met.Secondary, colWarn, p.fit(met.Secondary, m.search.msg, g.msg.W-2*u))
	}

	for _, ch := range g.chips {
		p.box(ch.rect, m.search.focus == ch.id)
		p.centered(ch.rect, met.Body, colText, ch.label)
	}
	for _, k := range g.kb.keys {
		if m.search.focus == k.ID {
			p.box(k.Rect, true)
		} else {
			p.l.Fill(k.Rect, met.Radius, colSurface)
		}
		label := m.keyLabel(k.ID)
		size := met.Body
		if p.width(size, label)+u > k.Rect.W {
			size = met.Secondary
		}
		p.centered(k.Rect, size, colText, p.fit(size, label, k.Rect.W))
	}
}
