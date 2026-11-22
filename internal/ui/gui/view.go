package gui

import (
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
)

const (
	toastStripe = 4 // px, the stripe in the state color (docs/ux/gui.md section 4)
	maxHints    = 5 // hint bar: at most five actions, four in compact
	brand       = "mothdex"
	crumb       = " › "
)

// painter draws into a list with the font and the metrics of the model. With a nil list it only measures,
// which is how layout shares its code with drawing.
type painter struct {
	l    *draw.List
	m    Metrics
	font *text.Font
}

func (m Model) painter(l *draw.List) painter { return painter{l: l, m: m.metrics, font: m.o.Font} }

func (p painter) face(size int) *text.Face {
	if p.font == nil {
		return nil
	}
	f, err := p.font.Face(size)
	if err != nil {
		return nil
	}
	return f
}

// width is the width of a line in pixels.
func (p painter) width(size int, s string) int {
	if f := p.face(size); f != nil {
		return f.Width(s)
	}
	return len([]rune(s)) * size / 2
}

// fit cuts a line with an ellipsis so it is at most w pixels wide.
func (p painter) fit(size int, s string, w int) string {
	if f := p.face(size); f != nil {
		return f.Fit(s, w)
	}
	return s
}

// wrap breaks text into lines of at most w pixels, and at most maxLines when that is above zero.
func (p painter) wrap(size int, s string, w, maxLines int) []string {
	if f := p.face(size); f != nil {
		return f.Wrap(s, w, maxLines)
	}
	return []string{s}
}

// base is the baseline of a line whose box starts at top and is LineHeight(size) tall.
func (p painter) base(top, size int) int {
	ascent, descent := size*4/5, size/5
	if f := p.face(size); f != nil {
		ascent, descent = f.Ascent(), f.Descent()
	}
	return top + (LineHeight(size)-(ascent+descent))/2 + ascent
}

// text draws a line with its box starting at (x, top).
func (p painter) text(x, top, size int, c draw.Color, s string) {
	p.l.Text(x, p.base(top, size), size, c, s)
}

// centered draws a line centered in r on both axes.
func (p painter) centered(r rect, size int, c draw.Color, s string) {
	w := p.width(size, s)
	p.text(r.X+(r.W-w)/2, r.Y+(r.H-LineHeight(size))/2, size, c, s)
}

// rightAligned draws a line that ends at right, vertically centered in a band starting at top.
func (p painter) rightAligned(right, top, bandH, size int, c draw.Color, s string) int {
	x := right - p.width(size, s)
	p.text(x, top+(bandH-LineHeight(size))/2, size, c, s)
	return x
}

// box draws a focusable surface: the surface color, or the focus fill with the ring outside it.
func (p painter) box(r rect, focused bool) {
	if focused {
		p.l.Fill(r.Inset(-p.m.Ring), p.m.Radius+p.m.Ring, colFocusRing)
		p.l.Fill(r, p.m.Radius, colFocusBG)
		return
	}
	p.l.Fill(r, p.m.Radius, colSurface)
}

// spinner draws the ring of docs/ux/gui.md section 4 at its place in the turn.
func (p painter) spinner(cx, cy, r int, now time.Time) {
	turn := float64(now.UnixNano()%int64(spinnerPeriod)) / float64(spinnerPeriod)
	p.l.Spinner(cx, cy, r, max(2, r/4), turn, colAccent)
}

// Render draws the whole frame for time now into l.
func (m Model) Render(now time.Time, l *draw.List) {
	met := m.metrics
	l.Reset(met.W, met.H)
	l.Fill(rect{W: met.W, H: met.H}, 0, colBG)
	p := m.painter(l)
	switch m.screen {
	case screenSplash:
		m.drawSplash(p, now)
	case screenHome:
		m.drawFrame(p, frameSpec{hints: m.hintsHome()})
		m.drawHome(p)
	case screenExit:
		m.drawExit(p, now)
	case screenSearch:
		m.drawFrame(p, m.searchFrame())
		m.drawSearch(p)
	case screenPicker:
		m.drawFrame(p, m.pickerFrame())
		m.drawPicker(p, now)
	case screenResults:
		m.drawFrame(p, m.resultsFrame())
		m.drawResults(p, now)
	case screenEntry:
		m.drawFrame(p, m.entryFrame())
		m.drawEntry(p, now)
	case screenQueue:
		m.drawFrame(p, m.queueFrame())
		m.drawQueue(p, now)
	case screenHistory:
		m.drawFrame(p, m.historyFrame())
		m.drawHistory(p)
	case screenSettings:
		m.drawFrame(p, m.settingsFrame())
		m.drawSettings(p)
	}
	switch m.overlay {
	case overlayMenu:
		m.drawMenu(p)
	case overlayButtons:
		m.drawButtons(p)
	case overlayDialog:
		m.drawDialog(p)
	}
	m.drawToast(p, now)
}

// hint is one entry of the hint bar: the role of a button and the word for what it does.
type hint struct {
	role  input.Action
	role2 input.Action // a second button sharing the word, such as (L)(R) Cursor; input.None for one
	label string
	// optional hints are dropped first when the bar has no room (compact shows four).
	optional bool
}

// frameSpec is what a screen puts in the frame (docs/ux/gui.md section 4): the crumbs after the brand, an
// extra word on the right of the top bar ("2 selected"), the hints, and the word of the Start hint.
type frameSpec struct {
	crumbs []string
	extra  string
	hints  []hint
	start  string // "" is Menu
}

// drawFrame draws the top bar and the hint bar of docs/ux/gui.md section 4.
func (m Model) drawFrame(p painter, spec frameSpec) {
	m.drawTopBar(p, spec)
	m.drawHintBar(p, spec.hints, spec.start, true)
}

func (m Model) queueBadge() string {
	active, queued := m.jobs.counts()
	switch {
	case queued > 0 && active == 0: // nothing runs yet: "1 queued", not "0 · 1 queued"
		return m.tr("ui.gui.bar.badge_queued_only", map[string]any{"queued": queued})
	case queued > 0:
		return m.tr("ui.gui.bar.badge_queued", map[string]any{"active": active, "queued": queued})
	case active > 0:
		return m.tr("ui.gui.bar.badge_active", map[string]any{"active": active})
	}
	return ""
}

// topBarItem is something on the right of the top bar. Arrow items draw a down arrow before their text.
type topBarItem struct {
	s     string
	c     draw.Color
	arrow bool
}

// topBarItems chooses what the right of the top bar shows so that it leaves the title at least minTitle pixels
// (docs/ux/gui.md section 4, PR-71 review M1): Offline and the queue badge are never cut; the pending-quit
// notice switches to its short text when the full one does not fit, and is cut with an ellipsis only if even
// that does not. They are returned in reading order with the total width they take.
func (m Model) topBarItems(p painter, avail int, extra string) ([]topBarItem, int) {
	met := m.metrics
	gap := 2 * met.Unit
	size := met.Secondary
	var fixedBefore, fixedAfter []topBarItem
	if extra != "" {
		fixedBefore = append(fixedBefore, topBarItem{s: extra, c: colDim})
	}
	if m.offline {
		fixedBefore = append(fixedBefore, topBarItem{s: m.tr("ui.gui.bar.offline", nil), c: colWarn})
	}
	if badge := m.queueBadge(); badge != "" && m.screen != screenQueue { // the Queue shows its own counts
		fixedAfter = append(fixedAfter, topBarItem{s: badge, c: colAccent, arrow: true})
	}
	widthOf := func(it topBarItem) int {
		if it.arrow {
			return p.arrowLabelWidth(size, it.s)
		}
		return p.width(size, it.s)
	}
	total := func(items []topBarItem) int {
		w := 0
		for i, it := range items {
			w += widthOf(it)
			if i > 0 {
				w += gap
			}
		}
		return w
	}
	assemble := func(notice string) []topBarItem {
		items := append([]topBarItem{}, fixedBefore...)
		if notice != "" {
			items = append(items, topBarItem{s: notice, c: colWarn})
		}
		return append(items, fixedAfter...)
	}
	if !m.quitPending {
		items := assemble("")
		return items, total(items)
	}
	for _, key := range []string{"ui.gui.bar.will_quit", "ui.gui.bar.will_quit_short"} {
		if items := assemble(m.tr(key, nil)); total(items) <= avail {
			return items, total(items)
		}
	}
	// Even the short text does not fit: cut only that item, to what is left after the fixed ones.
	rest := total(assemble("x")) - p.width(size, "x")
	room := max(avail-rest-gap, 0)
	items := assemble(p.fit(size, m.tr("ui.gui.bar.will_quit_short", nil), room))
	return items, total(items)
}

// fitCrumbs writes the title of the top bar for a width: "mothdex › Results › zelda" when it fits, otherwise
// crumbs are dropped from the left behind an ellipsis ("… › Results › zelda", "… › zelda"), and only the last
// crumb itself is cut when even that does not fit (docs/ux/gui.md section 4).
func fitCrumbs(p painter, size int, crumbs []string, maxW int) string {
	full := brand
	for _, c := range crumbs {
		full += crumb + c
	}
	if p.width(size, full) <= maxW {
		return full
	}
	for from := 1; from < len(crumbs); from++ {
		t := "…"
		for _, c := range crumbs[from:] {
			t += crumb + c
		}
		if p.width(size, t) <= maxW {
			return t
		}
	}
	last := crumbs[len(crumbs)-1]
	return "…" + crumb + p.fit(size, last, max(maxW-p.width(size, "…"+crumb), p.width(size, "…")))
}

// minTitleWidth is the width the title always keeps: the brand on Home, and on other screens the last crumb
// after an ellipsis.
func minTitleWidth(p painter, size int, crumbs []string) int {
	if len(crumbs) == 0 {
		return p.width(size, brand)
	}
	return p.width(size, "…"+crumb+crumbs[len(crumbs)-1])
}

func (m Model) drawTopBar(p painter, spec frameSpec) {
	met := m.metrics
	bar := met.TopBarRect()
	p.l.Fill(bar, 0, colSurface)
	pad := 2 * met.Unit

	full := brand
	for _, c := range spec.crumbs {
		full += crumb + c
	}
	// The title always keeps its minimum, and the right side takes the rest.
	minTitle := min(p.width(met.Title, full), minTitleWidth(p, met.Title, spec.crumbs))
	avail := max(bar.W-2*pad-minTitle-pad, 0)
	right, _ := m.topBarItems(p, avail, spec.extra)

	x := bar.Right() - pad
	for i := len(right) - 1; i >= 0; i-- {
		if right[i].arrow {
			x = p.arrowLabel(x, bar.Y, bar.H, met.Secondary, right[i].c, right[i].s) - pad
			continue
		}
		x = p.rightAligned(x, bar.Y, bar.H, met.Secondary, right[i].c, right[i].s) - pad
	}
	title := fitCrumbs(p, met.Title, spec.crumbs, max(x-(bar.X+pad), 0))
	p.text(bar.X+pad, bar.Y+(bar.H-LineHeight(met.Title))/2, met.Title, colText, title)
}

// pillWidth is how wide a pill for label is: a disc for a one-letter label, and a pill with 1u of air at each
// end for a word.
func (p painter) pillWidth(h int, label string) int {
	if _, ok := arrowLabels[label]; ok {
		return h
	}
	if len([]rune(label)) > 1 {
		return max(h, p.width(p.m.Secondary, label)+2*p.m.Unit)
	}
	return max(h, p.width(p.m.Secondary, label)+p.m.Unit)
}

// pill draws a button label on a dark disc (a pill for a longer label) and returns its width.
func (p painter) pill(x, y, h int, label string) int { return p.pillOn(x, y, h, label, colBG) }

// pillOn draws a pill with the given fill: bg on a surface bar, surface on the bg screen.
func (p painter) pillOn(x, y, h int, label string, fill draw.Color) int {
	w := p.pillWidth(h, label)
	p.l.Fill(rect{X: x, Y: y, W: w, H: h}, h/2, fill)
	if dir, ok := arrowLabels[label]; ok {
		s := h * 2 / 5
		drawTriangle(p.l, x+(w-s)/2, y+(h-s)/2, s, dir, colText)
		return w
	}
	p.centered(rect{X: x, Y: y, W: w, H: h}, p.m.Secondary, colText, label)
	return w
}

func (m Model) drawHintBar(p painter, hints []hint, start string, withStart bool) {
	met := m.metrics
	bar := met.HintBarRect()
	p.l.Fill(bar, 0, colSurface)
	pad := 2 * met.Unit
	h := max(bar.H-met.Unit, 20)
	y := bar.Y + (bar.H-h)/2
	size := met.Secondary
	gap := 2 * met.Unit
	textY := bar.Y + (bar.H-LineHeight(size))/2

	// Start at the left. Its word is kept where Start is not the menu (Search, Done): it is the only clue there.
	// Where Start is the menu, compact drops the word, and so does a bar with no room (docs/ux/gui.md section 4).
	startIsMenu := start == ""
	startWord := start
	if startIsMenu {
		startWord = m.tr("ui.gui.hint.menu", nil)
	}
	showWord := withStart && (met.Tier != Compact || !startIsMenu)
	startLabel := input.PositionLabel(input.Start, m.pad)

	limit := maxHints
	if met.Tier == Compact {
		limit = 4
	}
	hints = shrinkHints(hints, func(hs []hint) bool { return len(hs) <= limit })

	type placed struct {
		labels []string
		word   string
	}
	place := func(hs []hint) ([]placed, int) {
		items := make([]placed, 0, len(hs))
		total := 0
		for _, hnt := range hs {
			var labels []string // a hint with no button is only words, such as "Set by MOTHDEX_DOWNLOAD_CONCURRENCY"
			if hnt.role != input.None {
				labels = append(labels, input.Hint(hnt.role, m.pad, m.cfg.confirmEast))
				if hnt.role2 != input.None {
					labels = append(labels, input.Hint(hnt.role2, m.pad, m.cfg.confirmEast))
				}
			}
			items = append(items, placed{labels, hnt.label})
			for _, l := range labels {
				total += p.pillWidth(h, l) + met.Unit/4
			}
			total += met.Unit/2 + p.width(size, hnt.label)
			if len(items) > 1 {
				total += gap
			}
		}
		return items, total
	}
	leftWidth := func(word bool) int {
		if !withStart {
			return 0
		}
		w := p.pillWidth(h, startLabel)
		if word {
			w += met.Unit/2 + p.width(size, startWord)
		}
		return w
	}
	fits := func(hs []hint, word bool) bool {
		_, total := place(hs)
		return leftWidth(word)+gap+total <= bar.W-2*pad
	}
	// Too wide: drop L/R first, then the Start word when it means Menu, then Y, then X. A and B always stay
	// and no word is ever cut.
	hints = shrinkHints(hints, func(hs []hint) bool { return fits(hs, showWord) || !hasRank(hs, 0) })
	if !fits(hints, showWord) && showWord && startIsMenu {
		showWord = false
	}
	hints = shrinkHints(hints, func(hs []hint) bool { return fits(hs, showWord) })

	x := bar.X + pad
	if withStart {
		x += p.pill(x, y, h, startLabel) + met.Unit/2
		if showWord {
			p.text(x, textY, size, colDim, startWord)
		}
	}
	items, total := place(hints)
	x = bar.Right() - pad - total
	for _, it := range items {
		for _, l := range it.labels {
			x += p.pill(x, y, h, l) + met.Unit/4
		}
		x += met.Unit / 2
		p.text(x, textY, size, colDim, it.word)
		x += p.width(size, it.word) + gap
	}
}

// dropRank says how early a hint goes when the bar has no room: the optional L/R pair first, then Y, then X.
// A and B (and anything else) are never dropped.
func dropRank(h hint) int {
	switch {
	case h.optional:
		return 0
	case h.role == input.Info:
		return 1
	case h.role == input.Download:
		return 2
	}
	return -1
}

// hasRank reports whether some hint has exactly the given drop rank.
func hasRank(hs []hint, rank int) bool {
	for _, h := range hs {
		if dropRank(h) == rank {
			return true
		}
	}
	return false
}

// shrinkHints drops hints, in the order of dropRank, until done reports the set fits or nothing droppable is
// left. The hints that stay keep their order.
func shrinkHints(hints []hint, done func([]hint) bool) []hint {
	out := append([]hint(nil), hints...)
	for !done(out) {
		drop, best := -1, 1<<30
		for i, h := range out {
			if r := dropRank(h); r >= 0 && r < best {
				drop, best = i, r
			}
		}
		if drop < 0 {
			break
		}
		out = append(out[:drop], out[drop+1:]...)
	}
	return out
}

func (k toastKind) color() draw.Color {
	switch k {
	case toastOK:
		return colOK
	case toastWarn:
		return colWarn
	case toastError:
		return colError
	}
	return colAccent
}

// toastMaxPercent is how much of the safe width a toast may take, so it does not cover the screen.
const toastMaxPercent = 60

// toastLines are the lines of the toast text: wrapped to two lines inside 60 % of the safe width, the second
// ending in an ellipsis if the text still does not fit.
func (m Model) toastLines(p painter) []string {
	met := m.metrics
	inner := met.Safe.W*toastMaxPercent/100 - toastStripe - 2*met.Unit
	return p.wrap(met.Secondary, m.toast.text, max(inner, 40), 2)
}

// toastRect places the toast right-aligned under the top bar, and above the hint bar instead when it would
// cover the focused element (section 3: focus is always visible).
func (m Model) toastRect(p painter, lines []string) rect {
	met := m.metrics
	size := met.Secondary
	w := toastStripe + 2*met.Unit
	widest := 0
	for _, l := range lines {
		widest = max(widest, p.width(size, l))
	}
	w += widest
	h := len(lines)*LineHeight(size) + met.Unit
	r := rect{X: met.Safe.Right() - 2*met.Unit - w, Y: met.TopBarRect().Bottom() + met.Unit/2, W: w, H: h}
	if focus, ok := m.focusRect(); ok && intersects(r, focus.Inset(-met.Ring)) {
		r.Y = met.HintBarRect().Y - met.Unit/2 - h
	}
	return r
}

func intersects(a, b rect) bool {
	return a.X < b.Right() && b.X < a.Right() && a.Y < b.Bottom() && b.Y < a.Bottom()
}

// drawToast draws the toast under the top bar, right-aligned: the surface with a stripe in the color of its
// state, at most two lines.
func (m Model) drawToast(p painter, now time.Time) {
	if m.toast.text == "" || !now.Before(m.toast.until) {
		return
	}
	met := m.metrics
	size := met.Secondary
	lines := m.toastLines(p)
	r := m.toastRect(p, lines)
	p.l.Fill(r, 0, colSurface)
	p.l.Fill(rect{X: r.X, Y: r.Y, W: toastStripe, H: r.H}, 0, m.toast.kind.color())
	for i, line := range lines {
		p.text(r.X+toastStripe+met.Unit, r.Y+met.Unit/2+i*LineHeight(size), size, colText, line)
	}
}
