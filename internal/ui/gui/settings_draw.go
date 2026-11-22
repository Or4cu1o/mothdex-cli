package gui

import (
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// ---- layout ----

// itemHeight of the settings list: a header or the closing line is one small line; a row is a button-sized line, plus
// one small line under it for an error and one for "Applies on next start".
func (m Model) settingItemHeight(it settingItem) int {
	met := m.metrics
	switch {
	case it.header != "" || it.more:
		return LineHeight(met.Secondary) + met.Unit
	}
	h := met.Row1
	if m.settings.errs[it.row.id] != "" {
		h += LineHeight(met.Secondary)
	}
	if m.settings.changed[it.row.id] && it.row.nextRun {
		h += LineHeight(met.Secondary)
	}
	return h
}

// settingsVisibleRows is about how many rows a page holds, for L and R.
func (m Model) settingsVisibleRows() int { return max(m.metrics.ContentRect().H/m.metrics.Row1-1, 1) }

// settingsWindow is the range of items shown: the list scrolls so the focused row is whole, a header always travels with
// its first row, and the last item shown is never a header that would sit alone at the bottom.
func (m Model) settingsWindow(items []settingItem) (from, to int) {
	avail := m.metrics.ContentRect().H
	focus := 0
	for i, it := range items {
		if it.row != nil && it.row.id == m.settings.focus {
			focus = i
		}
	}
	top := min(max(m.settings.top, 0), len(items)-1)
	if focus < top {
		top = focus
	}
	if focus > 0 && items[focus-1].header != "" && top > focus-1 { // keep the header with its first row
		top = focus - 1
	}
	end := focus
	if focus == len(items)-2 { // the last row brings the closing line with it
		end = len(items) - 1
	}
	for top < focus {
		used := 0
		for i := top; i <= end; i++ {
			used += m.settingItemHeight(items[i])
		}
		if used <= avail {
			break
		}
		top++
	}
	used := 0
	to = top
	for to < len(items) && used+m.settingItemHeight(items[to]) <= avail {
		used += m.settingItemHeight(items[to])
		to++
	}
	for to-1 > top && items[to-1].header != "" { // a header never ends the page alone
		to--
	}
	return top, max(to, min(top+1, len(items)))
}

// ---- frame and hints ----

func (m Model) settingsFrame() frameSpec {
	spec := frameSpec{crumbs: []string{m.tr("ui.gui.screen.settings", nil)}}
	back := m.tr("ui.gui.hint.back", nil)
	if e := m.settings.ed; e != nil {
		switch e.kind {
		case edMargin:
			spec.hints = []hint{{role: input.Left, role2: input.Right, label: m.tr("ui.gui.hint.change", nil)}, {role: input.Confirm, label: m.tr("ui.gui.hint.save", nil)}, {role: input.Back, label: m.tr("ui.gui.hint.cancel", nil)}}
		case edInfo:
			spec.hints = []hint{{role: input.Back, label: m.tr("ui.gui.queue.close", nil)}}
		default:
			spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.select", nil)}, {role: input.Back, label: m.tr("ui.gui.queue.close", nil)}}
		}
		return spec
	}
	row := m.settingRowByID(m.settings.focus)
	if row == nil {
		spec.hints = []hint{{role: input.Back, label: back}}
		return spec
	}
	if note := m.readOnlyNote(row); note != "" {
		spec.hints = []hint{{label: note}, {role: input.Back, label: back}}
		return spec
	}
	var hs []hint
	change := hint{role: input.Left, role2: input.Right, label: m.tr("ui.gui.hint.change", nil)}
	switch row.kind {
	case kindStepper:
		hs = append(hs, change)
	case kindChoice:
		hs = append(hs, hint{role: input.Confirm, label: m.tr("ui.gui.hint.edit", nil)}, change)
	case kindToggle:
		hs = append(hs, hint{role: input.Confirm, label: m.tr("ui.gui.hint.toggle", nil)})
	case kindLanguages, kindMargin:
		hs = append(hs, hint{role: input.Confirm, label: m.tr("ui.gui.hint.edit", nil)})
	case kindCache:
		hs = append(hs, hint{role: input.Confirm, label: m.tr("ui.gui.hint.clear", nil)})
	case kindInfo:
		hs = append(hs, hint{role: input.Confirm, label: m.tr("ui.gui.queue.details", nil)})
	}
	if m.rowSource(row) == "file" && row.kind != kindCache && row.kind != kindInfo {
		hs = append(hs, hint{role: input.Download, label: m.tr("ui.gui.hint.reset", nil)})
	}
	spec.hints = append(hs, hint{role: input.Back, label: back})
	return spec
}

// ---- values ----

// settingValue is the text of the value column of a row.
func (m Model) settingValue(p painter, r *settingRow, width int) string {
	t := func(key string, args map[string]any) string { return m.tr("ui.gui.settings."+key, args) }
	switch r.kind {
	case kindStepper:
		return m.settingString(r.id)
	case kindToggle:
		if m.settingString(r.id) == "true" {
			return t("val.on", nil)
		}
		return t("val.off", nil)
	case kindChoice:
		cur := m.settingString(r.id)
		for _, c := range r.choices {
			if c.value == cur {
				return m.choiceLabel(c)
			}
		}
		if cur == "overwrite" {
			return t("val.overwrite", nil)
		}
		return app.SanitizeText(cur)
	case kindLanguages:
		langs := m.languageList()
		if len(langs) == 0 {
			return t("val.any_language", map[string]any{"count": 0})
		}
		parts := make([]string, len(langs))
		for i, l := range langs {
			parts[i] = titleToken(l)
		}
		return strings.Join(parts, ", ")
	case kindMargin:
		s := m.cfg.safe
		pct := strconv.Itoa(s.percentFor(m.metrics.DisplayW, m.metrics.DisplayH))
		if s.Auto {
			return t("val.auto_pct", map[string]any{"pct": pct})
		}
		return t("val.pct", map[string]any{"pct": pct})
	case kindCache:
		if m.cacheTotal < 0 {
			return ""
		}
		return humanSize(m.cacheTotal)
	case kindInfo:
		if r.id == "about" {
			return app.SanitizeText(m.o.Version)
		}
		root := shortenPath(p, m.metrics.Body, app.SanitizeText(m.profile.RomRoot), max(width/2, 1))
		return app.SanitizeText(m.profile.Name) + " · " + root
	}
	return ""
}

// ---- drawing ----

func (m Model) drawSettings(p painter) {
	met := m.metrics
	c := met.ContentRect()
	items := m.settingItems()
	from, to := m.settingsWindow(items)
	y := c.Y
	compact := met.Tier == Compact
	labelW := c.W * 44 / 100
	valueX := c.X + labelW + 2*met.Unit
	srcW := 0
	if !compact {
		srcW = p.width(met.Secondary, "mmmmmmmmmm")
	}
	for i := from; i < to; i++ {
		it := items[i]
		h := m.settingItemHeight(it)
		switch {
		case it.header != "":
			p.text(c.X+met.Unit, y+(h-LineHeight(met.Secondary))/2, met.Secondary, colDim, it.header)
		case it.more:
			p.text(c.X+met.Unit, y+(h-LineHeight(met.Secondary))/2, met.Secondary, colDim, p.fit(met.Secondary, m.tr("ui.gui.settings.more", nil), c.W-2*met.Unit))
		default:
			m.drawSettingRow(p, draw.Rect{X: c.X, Y: y, W: c.W, H: h}, it.row, labelW, valueX, srcW)
		}
		y += h
	}
	if e := m.settings.ed; e != nil {
		m.drawEditor(p, e)
	}
}

func (m Model) drawSettingRow(p painter, r draw.Rect, row *settingRow, labelW, valueX, srcW int) {
	met := m.metrics
	line := draw.Rect{X: r.X, Y: r.Y, W: r.W, H: met.Row1}.Inset(1)
	focused := m.settings.focus == row.id && m.settings.ed == nil
	if focused {
		p.box(line, true)
	}
	top := line.Y + (line.H-LineHeight(met.Body))/2
	label := m.tr("ui.gui.settings.row."+row.label, nil)
	p.text(r.X+met.Unit*2, top, met.Body, colText, p.fit(met.Body, label, labelW-met.Unit))
	right := r.Right() - met.Unit*2
	if srcW > 0 {
		if src := m.rowSource(row); src != "" {
			p.text(right-srcW, line.Y+(line.H-LineHeight(met.Secondary))/2, met.Secondary, colDim, m.tr("ui.gui.settings.src."+src, nil))
		}
		right -= srcW + met.Unit
	}
	valueW := max(right-valueX, 1)
	value := m.settingValue(p, row, valueW)
	color := colText
	if m.readOnlyNote(row) != "" {
		color = colDim
	}
	switch row.kind {
	case kindStepper:
		if focused {
			value = "‹ " + value + " ›"
		}
	case kindCache:
		btn := m.tr("ui.gui.settings.cache_clear", nil)
		bw := p.width(met.Secondary, btn) + 3*met.Unit
		br := draw.Rect{X: valueX + p.width(met.Body, value) + 2*met.Unit, Y: line.Y + met.Unit/2, W: bw, H: line.H - met.Unit}
		p.l.Fill(br, met.Radius, colBG)
		p.centered(br, met.Secondary, colText, btn)
	}
	p.text(valueX, top, met.Body, color, p.fit(met.Body, value, valueW))
	y := r.Y + met.Row1
	if msg := m.settings.errs[row.id]; msg != "" {
		p.text(r.X+met.Unit*2, y, met.Secondary, colError, p.fit(met.Secondary, msg, r.W-4*met.Unit))
		y += LineHeight(met.Secondary)
	}
	if m.settings.changed[row.id] && row.nextRun {
		p.text(r.X+met.Unit*2, y, met.Secondary, colDim, m.tr("ui.gui.settings.next_start", nil))
	}
}

// drawEditor draws the open editor over the list.
func (m Model) drawEditor(p painter, e *settingsEditor) {
	switch e.kind {
	case edMargin:
		m.drawMargin(p, e)
	case edInfo:
		m.drawInfoPanel(p, e)
	default:
		m.panelList(p, m.editorOptions(e), e.focus)
	}
}

// drawMargin is the calibration view of docs/ux/gui.md section 1.2: four corner brackets and a frame at the margin
// being tried, its percent in the largest size, a help line, and the hint bar inside the same margin, so the person
// sees what they will get. The real margin changes only when A saves.
func (m Model) drawMargin(p painter, e *settingsEditor) {
	met := m.metrics
	tried := NewMetrics(met.DisplayW, met.DisplayH, SafeArea{Percent: e.pct}, m.cfg.largeText)
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, 0, colBG)
	s := tried.Safe
	ring := max(met.Ring, 2)
	arm := 4 * met.Unit * 2
	// the frame, thin and dim
	for _, r := range []draw.Rect{
		{X: s.X, Y: s.Y, W: s.W, H: 1}, {X: s.X, Y: s.Bottom() - 1, W: s.W, H: 1},
		{X: s.X, Y: s.Y, W: 1, H: s.H}, {X: s.Right() - 1, Y: s.Y, W: 1, H: s.H},
	} {
		p.l.Fill(r, 0, colDim)
	}
	// the corner brackets, in the color and stroke of the focus ring
	for _, r := range []draw.Rect{
		{X: s.X, Y: s.Y, W: arm, H: ring}, {X: s.X, Y: s.Y, W: ring, H: arm},
		{X: s.Right() - arm, Y: s.Y, W: arm, H: ring}, {X: s.Right() - ring, Y: s.Y, W: ring, H: arm},
		{X: s.X, Y: s.Bottom() - ring, W: arm, H: ring}, {X: s.X, Y: s.Bottom() - arm, W: ring, H: arm},
		{X: s.Right() - arm, Y: s.Bottom() - ring, W: arm, H: ring}, {X: s.Right() - ring, Y: s.Bottom() - arm, W: ring, H: arm},
	} {
		p.l.Fill(r, 0, colAccent)
	}
	pct := m.tr("ui.gui.settings.val.pct", map[string]any{"pct": strconv.Itoa(e.pct)})
	cy := s.Y + s.H/2 - LineHeight(met.Title)
	p.text(s.X+(s.W-p.width(met.Title, pct))/2, cy, met.Title, colText, pct)
	for i, line := range p.wrap(met.Secondary, m.tr("ui.gui.settings.calib_help", nil), s.W-2*arm, 2) {
		p.text(s.X+(s.W-p.width(met.Secondary, line))/2, cy+LineHeight(met.Title)+met.Unit+i*LineHeight(met.Secondary), met.Secondary, colDim, line)
	}
	mm := m
	mm.metrics = tried
	mm.drawHintBar(mm.painter(p.l), m.settingsFrame().hints, "", false)
}

// drawInfoPanel is the read-only panel of System and About.
func (m Model) drawInfoPanel(p painter, e *settingsEditor) {
	met := m.metrics
	u := met.Unit
	title := m.tr("ui.gui.settings.row."+e.row, nil)
	var lines []string
	switch e.row {
	case "system":
		root := shortenPath(p, met.Secondary, app.SanitizeText(m.profile.RomRoot), min(met.Safe.W-8*u, 24*met.Body))
		lines = append(lines, app.SanitizeText(m.profile.Name)+" · "+root)
		if m.profile.Verified && len(m.profile.Warnings) == 0 {
			lines = append(lines, m.tr("ui.gui.settings.sys_verified", nil))
		}
		for _, w := range append(append([]string{}, m.profile.Unverified...), m.profile.Warnings...) {
			lines = append(lines, m.tr("ui.gui.settings.sys_unverified", map[string]any{"warning": app.SanitizeText(w)}))
		}
	default:
		lines = append(lines, m.tr("ui.gui.settings.about_version", nil)+": "+app.SanitizeText(m.o.Version))
		if m.entriesText != "" {
			lines = append(lines, m.tr("ui.gui.settings.about_entries", nil)+": "+m.entriesText)
		}
		if m.o.LogPath != "" {
			lines = append(lines, m.tr("ui.gui.settings.about_log", nil)+": "+app.SanitizeText(m.o.LogPath))
		}
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
	p.text(panel.X+2*u, y, met.Body, colText, p.fit(met.Body, title, maxW-4*u))
	y += LineHeight(met.Body) + u
	for _, l := range wrapped {
		p.text(panel.X+2*u, y, met.Secondary, colText, l)
		y += LineHeight(met.Secondary)
	}
}
