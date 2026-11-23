package gui

import (
	"strconv"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// ---- layout ----

// itemHeight of the settings list: a header or the closing line is one small line; a row is a button-sized line, plus
// one small line under it for an error and one for "Applies on next start".
func (m Model) settingItemHeight(it settingItem) int {
	met := m.metrics
	switch {
	case it.header != "":
		h, _, _, _ := m.headerLayout(m.painter(nil), it.header)
		return h
	case it.more:
		return LineHeight(met.Secondary) + met.Unit
	}
	h := m.settingRowBase(it.row)
	if m.settings.errs[it.row.id] != "" {
		h += LineHeight(met.Secondary)
	}
	if m.settings.changed[it.row.id] && it.row.nextRun {
		h += LineHeight(met.Secondary)
	}
	return h
}

// settingsLabelWidth is the width of the label column: the widest label of the language, between 44 and 55 percent of
// the width, so a long label (es_es, ru_ru) neither is cut nor touches its value.
func (m Model) settingsLabelWidth(p painter, contentW int) int {
	met := m.metrics
	widest := 0
	for _, r := range settingRows {
		widest = max(widest, p.width(met.Body, m.tr("ui.gui.settings.row."+r.label, nil)))
	}
	return min(max(widest+4*met.Unit, contentW*44/100), contentW*55/100)
}

// settingLabelLines is the label of a row, wrapped to two lines when even the widest column is too narrow for it.
func (m Model) settingLabelLines(p painter, r *settingRow, labelW int) []string {
	return p.wrap(m.metrics.Body, m.tr("ui.gui.settings.row."+r.label, nil), labelW-3*m.metrics.Unit, 2)
}

// headerLayout lays out a section header: its band has 1u above the text and half a unit below it, so a header never
// touches the row above or the focus frame of the row under it (Bússola, T-83). It returns the band height, the painter and
// size to draw the text with (the display face at body x 1.35, or the body face at the secondary size when the face lacks a
// glyph of the label) and the top of the text box from the top of the band.
func (m Model) headerLayout(p painter, label string) (h int, hp painter, size, textTop int) {
	met := m.metrics
	hp = p.display(label)
	size = met.Secondary
	if hp.disp {
		size = met.Header
	}
	ascent, descent := size*4/5, size/5
	if f := hp.face(size); f != nil {
		ascent, descent = f.Ascent(), f.Descent()
	}
	inkAscent, inkDescent := ascent, descent
	if hp.disp { // a header has no accented capital, so its ink is the ascenders (0.71 em) and descenders (0.24 em) of the face
		inkAscent, inkDescent = size*71/100, size*24/100+1
	}
	baseline := met.Unit + inkAscent // from the top of the band: the ink starts 1u below it
	textTop = baseline - ascent - (LineHeight(size)-(ascent+descent))/2
	return met.Unit + inkAscent + inkDescent + met.Unit/2, hp, size, textTop
}

// settingRowBase is the height of a row without the lines under it: one button-sized line, or two lines of text when
// the label wraps (docs/ux/gui.md section 1.3: nothing is cut but titles).
func (m Model) settingRowBase(r *settingRow) int {
	met := m.metrics
	p := m.painter(nil)
	lines := m.settingLabelLines(p, r, m.settingsLabelWidth(p, met.ContentRect().W))
	return max(met.Row1, len(lines)*LineHeight(met.Body)+met.Unit)
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
			if m.infoScrolls(e.row) {
				spec.hints = append([]hint{{role: input.Up, role2: input.Down, label: m.tr("ui.gui.buttons.move", nil)}}, spec.hints...)
			}
		default:
			spec.hints = []hint{{role: input.Confirm, label: m.tr("ui.gui.hint.choose", nil)}, {role: input.Back, label: m.tr("ui.gui.queue.close", nil)}}
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
	labelW := m.settingsLabelWidth(p, c.W)
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
			_, hp, size, top := m.headerLayout(p, it.header)
			hp.text(c.X+met.Unit, y+top, size, colDim, it.header) // a short label: the display face, in the dim text color
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
	base := m.settingRowBase(row)
	line := draw.Rect{X: r.X, Y: r.Y, W: r.W, H: base}.Inset(1)
	focused := m.settings.focus == row.id && m.settings.ed == nil
	if focused {
		p.box(line, true)
	}
	top := line.Y + (line.H-LineHeight(met.Body))/2
	labelLines := m.settingLabelLines(p, row, labelW)
	labelTop := line.Y + (line.H-len(labelLines)*LineHeight(met.Body))/2
	for i, l := range labelLines {
		p.text(r.X+met.Unit*2, labelTop+i*LineHeight(met.Body), met.Body, colText, l)
	}
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
		if focused && m.readOnlyNote(row) == "" { // a row that cannot be changed does not offer the arrows
			value = "‹ " + value + " ›"
		}
	case kindCache:
		pillH := line.H - met.Unit
		p.pillOn(valueX+p.width(met.Body, value)+2*met.Unit, line.Y+met.Unit/2, pillH, m.tr("ui.gui.settings.cache_clear", nil), colBG)
	}
	p.text(valueX, top, met.Body, color, p.fit(met.Body, value, valueW))
	y := r.Y + base
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
		m.panelListTitled(p, m.editorTitle(e), m.editorOptions(e), e.focus)
	}
}

// editorTitle names what a list editor is about: the setting for its lists, the language for its item sheet.
func (m Model) editorTitle(e *settingsEditor) string {
	if e.kind == edLangItem && e.idx < len(e.langs) {
		return titleToken(e.langs[e.idx])
	}
	if r := m.settingRowByID(e.row); r != nil {
		return m.tr("ui.gui.settings.row."+r.label, nil)
	}
	return ""
}

// drawMargin is the calibration view of docs/ux/gui.md section 1.2: four corner brackets and a frame at the margin
// being tried, its percent in the largest size, a help line, and the hint bar inside the same margin, so the person
// sees what they will get. The real margin changes only when A saves.
func (m Model) drawMargin(p painter, e *settingsEditor) {
	met := m.metrics
	tried := NewMetrics(met.DisplayW, met.DisplayH, SafeArea{Percent: e.pct}, m.cfg.largeText)
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, colBG)
	mm := m
	mm.metrics = tried
	mm.drawHintBar(mm.painter(p.l), m.settingsFrame().hints, "", false) // first, so the brackets stay on top of it
	s := tried.Safe
	ring := max(met.Ring, 2)
	arm := snapDown(4*met.Unit*2, met.Pixel)
	// the frame, thin and dim
	for _, r := range []draw.Rect{
		{X: s.X, Y: s.Y, W: s.W, H: 1}, {X: s.X, Y: s.Bottom() - 1, W: s.W, H: 1},
		{X: s.X, Y: s.Y, W: 1, H: s.H}, {X: s.Right() - 1, Y: s.Y, W: 1, H: s.H},
	} {
		p.l.Fill(r, colDim)
	}
	// the corner brackets, in the color and stroke of the focus ring
	for _, r := range []draw.Rect{
		{X: s.X, Y: s.Y, W: arm, H: ring}, {X: s.X, Y: s.Y, W: ring, H: arm},
		{X: s.Right() - arm, Y: s.Y, W: arm, H: ring}, {X: s.Right() - ring, Y: s.Y, W: ring, H: arm},
		{X: s.X, Y: s.Bottom() - ring, W: arm, H: ring}, {X: s.X, Y: s.Bottom() - arm, W: ring, H: arm},
		{X: s.Right() - arm, Y: s.Bottom() - ring, W: arm, H: ring}, {X: s.Right() - ring, Y: s.Bottom() - arm, W: ring, H: arm},
	} {
		p.l.Fill(r, colFocusRing)
	}
	pct := m.tr("ui.gui.settings.val.pct", map[string]any{"pct": strconv.Itoa(e.pct)})
	dp := p.display(pct) // the percent is a number: the display face
	psize := dp.dsize()
	cy := s.Y + s.H/2 - LineHeight(psize)
	dp.text(s.X+(s.W-dp.width(psize, pct))/2, cy, psize, colText, pct)
	for i, line := range p.wrap(met.Secondary, m.tr("ui.gui.settings.calib_help", nil), s.W-2*arm, 2) {
		p.text(s.X+(s.W-p.width(met.Secondary, line))/2, cy+LineHeight(psize)+met.Unit+i*LineHeight(met.Secondary), met.Secondary, colDim, line)
	}
}

// infoLine is one logical line of the read-only panels: indented by indent units, in the warn color when warn, with a
// unit of space above when gap (it starts a group).
type infoLine struct {
	text   string
	indent int
	warn   bool
	gap    bool
}

// visualLine is one drawn line of an info panel, after wrapping.
type visualLine struct {
	text string
	x    int // from the left edge of the text
	c    draw.Color
	gap  int // space above, in pixels
}

// infoLines are the lines of System or About.
func (m Model) infoLines(p painter, row string) []infoLine {
	met := m.metrics
	u := met.Unit
	var lines []infoLine
	switch row {
	case "system":
		root := shortenPath(p, met.Secondary, app.SanitizeText(m.profile.RomRoot), min(met.Safe.W-8*u, 24*met.Body))
		lines = append(lines, infoLine{text: app.SanitizeText(m.profile.Name) + " · " + root})
		if m.profile.Verified && len(m.profile.Warnings) == 0 {
			lines = append(lines, infoLine{text: m.tr("ui.gui.settings.sys_verified", nil)})
		}
		for _, w := range append(append([]string{}, m.profile.Unverified...), m.profile.Warnings...) {
			lines = append(lines, infoLine{text: m.tr("ui.gui.settings.sys_unverified", map[string]any{"warning": app.SanitizeText(w)})})
		}
		// Where files go is not a verification problem (PRT-G11): a RAM disk is its own warning, with no "Not verified"
		// prefix, and the Verified line above does not depend on it.
		for _, w := range m.profile.StorageWarnings {
			lines = append(lines, infoLine{text: m.tr(library.StorageWarningKey(w), nil), warn: true})
		}
	default:
		lines = append(lines, infoLine{text: m.tr("ui.gui.settings.about_version", nil) + ": " + app.SanitizeText(m.o.Version)})
		if m.entriesText != "" {
			lines = append(lines, infoLine{text: m.tr("ui.gui.settings.about_entries", nil) + ": " + m.entriesText})
		}
		lines = append(lines, m.controllerLines()...)
		if m.o.LogPath != "" {
			lines = append(lines, infoLine{text: m.tr("ui.gui.settings.about_log", nil) + ": " + app.SanitizeText(m.o.LogPath), gap: true})
		}
	}
	return lines
}

// infoGeometry lays an info panel out: the wrapped lines that fit from offset on, the panel, and the largest offset (0
// when everything fits, so the panel does not scroll). The panel is at most as tall as the safe rectangle allows.
func (m Model) infoGeometry(p painter, row string, offset int) (panel draw.Rect, shown []visualLine, maxOffset int) {
	met := m.metrics
	u := met.Unit
	maxW := min(met.Safe.W-4*u, 28*met.Body)
	var all []visualLine
	for _, l := range m.infoLines(p, row) {
		for i, w := range p.wrap(met.Secondary, l.text, maxW-4*u-l.indent*2*u, 8) {
			vl := visualLine{text: w, x: l.indent * 2 * u, c: colText}
			if l.warn {
				vl.c = colWarn
			}
			if i == 0 && l.gap {
				vl.gap = u
			}
			all = append(all, vl)
		}
	}
	lh := LineHeight(met.Secondary)
	fixed := LineHeight(met.Body) + 5*u
	room := met.Safe.H - 4*u - fixed
	fits := func(from int) (n, h int) {
		for i := from; i < len(all); i++ {
			if h+lh+all[i].gap > room && i > from {
				break
			}
			h += lh + all[i].gap
			n++
		}
		return n, h
	}
	for maxOffset = 0; maxOffset < len(all); maxOffset++ { // the first offset from which everything left fits
		if n, _ := fits(maxOffset); maxOffset+n >= len(all) {
			break
		}
	}
	offset = min(max(offset, 0), maxOffset)
	n, h := fits(offset)
	shown = all[offset : offset+n]
	panelH := min(h+fixed, met.Safe.H-4*u)
	panel = draw.Rect{X: met.Safe.X + (met.Safe.W-maxW)/2, Y: met.Safe.Y + max((met.Safe.H-panelH)/2, 0), W: maxW, H: panelH}
	return panel, shown, maxOffset
}

// drawInfoPanel is the read-only panel of System and About. When its lines do not all fit, Up and Down scroll it and
// a small triangle at the edge says which way there is more.
func (m Model) drawInfoPanel(p painter, e *settingsEditor) {
	met := m.metrics
	u := met.Unit
	title := m.tr("ui.gui.settings.row."+e.row, nil)
	panel, shown, maxOff := m.infoGeometry(p, e.row, e.focus)
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, colScrim)
	p.window(panel)
	y := panel.Y + 2*u
	p.text(panel.X+2*u, y, met.Body, colText, p.fit(met.Body, title, panel.W-4*u))
	y += LineHeight(met.Body) + u
	for _, l := range shown {
		y += l.gap
		p.text(panel.X+2*u+l.x, y, met.Secondary, l.c, l.text)
		y += LineHeight(met.Secondary)
	}
	off := min(max(e.focus, 0), maxOff)
	tri := LineHeight(met.Secondary) / 2
	if off > 0 {
		drawTriangle(p.l, p.m.Pixel, panel.Right()-2*u-tri, panel.Y+u, tri, 'u', colDim)
	}
	if off < maxOff {
		drawTriangle(p.l, p.m.Pixel, panel.Right()-2*u-tri, panel.Bottom()-u-tri, tri, 'd', colDim)
	}
}

// infoScrolls reports whether the open info panel has more lines than fit.
func (m Model) infoScrolls(row string) bool {
	_, _, maxOff := m.infoGeometry(m.painter(nil), row, 0)
	return maxOff > 0
}

// controllerLines are the About lines for the joysticks present, for whoever has to add a mapping or report a problem.
// Each joystick is a group: its name at the left edge; under it, indented, for one SDL has no mapping for the line
// that says where to add one (the whole path, never shortened, in the warn color) and the GUID. With none present,
// "None connected".
func (m Model) controllerLines() []infoLine {
	pad := m.tr("ui.gui.settings.about_pad", nil)
	if len(m.pads) == 0 {
		return []infoLine{{text: pad + ": " + m.tr("ui.gui.settings.about_none", nil)}}
	}
	var lines []infoLine
	for i, p := range m.pads {
		lines = append(lines, infoLine{text: pad + ": " + app.SanitizeText(p.Name), gap: i > 0})
		if !p.Mapped {
			lines = append(lines, infoLine{text: m.tr("ui.gui.settings.about_unmapped", map[string]any{"path": app.SanitizeText(m.o.MappingsPath)}), indent: 1, warn: true})
		}
		guid := app.SanitizeText(p.GUID)
		if guid == "" {
			guid = m.tr("ui.gui.settings.about_guid_none", nil)
		}
		lines = append(lines, infoLine{text: m.tr("ui.gui.settings.about_guid", nil) + ": " + guid, indent: 1})
	}
	return lines
}
