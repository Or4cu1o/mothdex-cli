package gui

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

type pickerKind int

const (
	pickPlatform pickerKind = iota
	pickRegion
)

// pickerState is the Platform or Region picker (docs/ux/gui.md section 5.5). From Search it is a multi-select
// with checkboxes; from Home › Platforms it is a single select that opens Results.
type pickerState struct {
	kind     pickerKind
	single   bool
	selected []string
	list     listState
}

// pickRow is one row of the list: a brand header or a choice.
type pickRow struct {
	header bool
	id     string
	name   string
}

type (
	platformsMsg struct {
		items []mothdex.Platform
		err   error
	}
	regionsMsg struct {
		items []mothdex.Region
		err   error
	}
)

// openPicker shows a picker, starting the load of its list if it is not loaded yet.
func (m Model) openPicker(kind pickerKind, single bool, now time.Time) (Model, []Cmd) {
	var selected []string
	if !single {
		selected = slices.Clone(m.search.platforms)
		if kind == pickRegion {
			selected = slices.Clone(m.search.regions)
		}
	}
	m.picker = pickerState{kind: kind, single: single, selected: selected}
	m = m.enter(screenPicker)
	m = m.firstChoice()
	return m.loadCatalog(kind, now)
}

// loadCatalog starts the load of the picker's list unless it is loaded or loading.
func (m Model) loadCatalog(kind pickerKind, now time.Time) (Model, []Cmd) {
	svc := m.o.Service
	if kind == pickPlatform {
		if m.platforms.phase == loadBusy || m.platforms.phase == loadOK {
			return m, nil
		}
		m.platforms.phase, m.platforms.since = loadBusy, now
		return m, []Cmd{func() Msg {
			res, err := svc.Platforms(context.Background(), app.PlatformFilter{})
			return platformsMsg{items: res.Value, err: err}
		}}
	}
	if m.regions.phase == loadBusy || m.regions.phase == loadOK {
		return m, nil
	}
	m.regions.phase, m.regions.since = loadBusy, now
	return m, []Cmd{func() Msg {
		res, err := svc.Regions(context.Background())
		return regionsMsg{items: res.Value, err: err}
	}}
}

func (m Model) onPlatforms(msg platformsMsg) Model {
	if msg.err != nil {
		m.platforms.phase, m.platforms.fail = loadFailed, m.reportOr(msg.err, "platforms")
		return m
	}
	m.platforms = catalogLoad[mothdex.Platform]{phase: loadOK, items: msg.items}
	if m.screen == screenPicker && m.picker.kind == pickPlatform {
		m = m.firstChoice()
	}
	return m
}

func (m Model) onRegions(msg regionsMsg) Model {
	if msg.err != nil {
		m.regions.phase, m.regions.fail = loadFailed, m.reportOr(msg.err, "regions")
		return m
	}
	m.regions = catalogLoad[mothdex.Region]{phase: loadOK, items: msg.items}
	if m.screen == screenPicker && m.picker.kind == pickRegion {
		m = m.firstChoice()
	}
	return m
}

// firstChoice puts the focus on the first choice (not a header) of the picker.
func (m Model) firstChoice() Model {
	rows := m.pickerRows()
	for i, r := range rows {
		if !r.header {
			m.picker.list = listState{focus: i}
			return m
		}
	}
	m.picker.list = listState{}
	return m
}

func (m Model) catalogPhase() (loadPhase, failure) {
	if m.picker.kind == pickPlatform {
		return m.platforms.phase, m.platforms.fail
	}
	return m.regions.phase, m.regions.fail
}

func (m Model) catalogSince() time.Time {
	if m.picker.kind == pickPlatform {
		return m.platforms.since
	}
	return m.regions.since
}

// pickerRows are the rows of the loaded list: platforms grouped by brand (groups by name, platforms by name
// inside them), or regions by name.
func (m Model) pickerRows() []pickRow {
	if m.picker.kind == pickRegion {
		items := slices.Clone(m.regions.items)
		slices.SortFunc(items, func(a, b mothdex.Region) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		rows := make([]pickRow, 0, len(items))
		for _, r := range items {
			rows = append(rows, pickRow{id: r.ID, name: app.SanitizeText(r.Name)})
		}
		return rows
	}
	other := m.tr("ui.gui.picker.other_brand", nil)
	byBrand := map[string][]mothdex.Platform{}
	for _, pl := range m.platforms.items {
		brand := strings.TrimSpace(app.SanitizeText(pl.Brand))
		if brand == "" {
			brand = other
		}
		byBrand[brand] = append(byBrand[brand], pl)
	}
	brands := make([]string, 0, len(byBrand))
	for b := range byBrand {
		brands = append(brands, b)
	}
	slices.SortFunc(brands, func(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) })
	var rows []pickRow
	for _, b := range brands {
		rows = append(rows, pickRow{header: true, name: b})
		items := byBrand[b]
		slices.SortFunc(items, func(a, c mothdex.Platform) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(c.Name))
		})
		for _, pl := range items {
			rows = append(rows, pickRow{id: pl.ID, name: app.SanitizeText(pl.Name)})
		}
	}
	return rows
}

// moveChoice moves focus by one choice up or down, skipping headers and stopping at the ends.
func moveChoice(rows []pickRow, from, dir int) int {
	for i := from + dir; i >= 0 && i < len(rows); i += dir {
		if !rows[i].header {
			return i
		}
	}
	return from
}

// jumpGroup moves to the first choice of the previous or next brand group (L and R).
func jumpGroup(rows []pickRow, from, dir int) int {
	group := from
	for group > 0 && !rows[group].header {
		group--
	}
	if dir > 0 {
		for i := from + 1; i < len(rows); i++ {
			if rows[i].header && i+1 < len(rows) {
				return i + 1
			}
		}
		return from
	}
	if rows[from].header {
		group = from
	}
	for i := group - 1; i >= 0; i-- {
		if rows[i].header && i+1 < len(rows) {
			if i+1 < group || i+1 < from {
				return i + 1
			}
		}
	}
	return from
}

func (m Model) visibleRows() int {
	return max(m.metrics.ContentRect().H/m.metrics.Row1, 1)
}

func (m Model) onPickerAction(a input.Action, now time.Time) (Model, []Cmd) {
	phase, _ := m.catalogPhase()
	if phase != loadOK {
		switch a {
		case input.Back:
			return m.leave(), nil
		case input.Confirm:
			if phase == loadFailed {
				return m.loadCatalog(m.picker.kind, now)
			}
		}
		return m, nil
	}
	rows := m.pickerRows()
	l := m.picker.list
	n := len(rows)
	switch a {
	case input.Up, input.Down:
		dir := 1
		if a == input.Up {
			dir = -1
		}
		to := moveChoice(rows, l.focus, dir)
		m.picker.list = l.moved(to-l.focus, m.visibleRows(), n)
	case input.PageUp, input.PageDown:
		if m.picker.kind == pickPlatform {
			dir := 1
			if a == input.PageUp {
				dir = -1
			}
			to := jumpGroup(rows, l.focus, dir)
			m.picker.list = l.moved(to-l.focus, m.visibleRows(), n)
		}
	case input.Confirm:
		return m.choose(rows, now)
	case input.Info: // clear all
		if !m.picker.single {
			m.picker.selected = nil
		}
	case input.Menu: // Done
		if !m.picker.single {
			return m.applyPicker(), nil
		}
	case input.Back: // cancel: the changes are dropped
		return m.leave(), nil
	}
	return m, nil
}

// choose is A: toggle the focused choice, or in single mode open Results for it.
func (m Model) choose(rows []pickRow, now time.Time) (Model, []Cmd) {
	if m.picker.list.focus >= len(rows) || rows[m.picker.list.focus].header {
		return m, nil
	}
	id := rows[m.picker.list.focus].id
	if m.picker.single {
		m = m.leave() // B on Results goes back to Home
		return m.startResults(app.SearchQuery{Platforms: []string{id}}, false, now)
	}
	if i := slices.Index(m.picker.selected, id); i >= 0 {
		m.picker.selected = slices.Delete(slices.Clone(m.picker.selected), i, i+1)
	} else {
		m.picker.selected = append(slices.Clone(m.picker.selected), id)
	}
	return m, nil
}

// applyPicker writes the chosen ids into the search filters and returns to Search.
func (m Model) applyPicker() Model {
	if m.picker.kind == pickPlatform {
		m.search.platforms = slices.Clone(m.picker.selected)
	} else {
		m.search.regions = slices.Clone(m.picker.selected)
	}
	return m.leave()
}

func (m Model) pickerFrame() frameSpec {
	title := m.tr("ui.gui.search.platform_title", nil)
	if m.picker.kind == pickRegion {
		title = m.tr("ui.gui.search.region_title", nil)
	}
	spec := frameSpec{crumbs: []string{title}}
	phase, _ := m.catalogPhase()
	switch {
	case phase == loadFailed:
		spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.retry", nil)}, {role: input.Back, label: m.tr("ui.gui.hint.back", nil)}}
		spec.start = m.tr("ui.gui.hint.menu", nil)
	case m.picker.single:
		spec.hints = []hint{{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.group", nil), optional: true}, {role: input.Confirm, label: m.tr("ui.gui.hint.open", nil)}, {role: input.Back, label: m.tr("ui.gui.hint.back", nil)}}
		spec.start = ""
	default:
		spec.extra = m.tr("ui.gui.picker.selected", map[string]any{"count": len(m.picker.selected)})
		spec.start = m.tr("ui.gui.hint.done", nil)
		spec.hints = []hint{
			{role: input.PageUp, role2: input.PageDown, label: m.tr("ui.gui.hint.group", nil), optional: true},
			{role: input.Confirm, label: m.tr("ui.gui.hint.select", nil)},
			{role: input.Info, label: m.tr("ui.gui.hint.clear_all", nil)},
			{role: input.Back, label: m.tr("ui.gui.hint.cancel", nil)},
		}
	}
	return spec
}

func (m Model) searchFrame() frameSpec {
	return frameSpec{crumbs: []string{m.tr("ui.gui.screen.search", nil)}, hints: m.searchHints(), start: m.tr("ui.gui.hint.search", nil)}
}

func (m Model) drawPicker(p painter, now time.Time) {
	met := m.metrics
	c := met.ContentRect()
	phase, fail := m.catalogPhase()
	switch phase {
	case loadBusy, loadIdle:
		m.drawLoading(p, c, m.tr("ui.gui.loading", nil), m.catalogSince(), now)
		return
	case loadFailed:
		m.drawFailure(p, c, fail, true)
		return
	}
	rows := m.pickerRows()
	l, from, to := m.picker.list.shown(m.visibleRows(), len(rows))
	for i := from; i < to; i++ {
		r := draw.Rect{X: c.X, Y: c.Y + (i-from)*met.Row1, W: c.W, H: met.Row1}
		row := rows[i]
		top := r.Y + (r.H-LineHeight(met.Body))/2
		if row.header {
			p.text(r.X+met.Unit, r.Y+(r.H-LineHeight(met.Secondary))/2, met.Secondary, colDim, row.name)
			continue
		}
		if i == l.focus {
			p.box(r.Inset(met.Ring), true)
		}
		x := r.X + 2*met.Unit
		if !m.picker.single {
			mark := "[ ]"
			if slices.Contains(m.picker.selected, row.id) {
				mark = "[x]"
			}
			p.text(x, top, met.Body, colText, mark)
			x += p.width(met.Body, "[x]") + met.Unit
		}
		idW := p.width(met.Secondary, row.id)
		p.text(x, top, met.Body, colText, p.fit(met.Body, row.name, r.Right()-2*met.Unit-idW-2*met.Unit-x))
		p.rightAligned(r.Right()-2*met.Unit, r.Y, r.H, met.Secondary, colDim, row.id)
	}
}
