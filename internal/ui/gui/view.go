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
		m.drawFrame(p, "", m.hintsHome())
		m.drawHome(p)
	case screenExit:
		m.drawExit(p, now)
	}
	switch m.overlay {
	case overlayMenu:
		m.drawMenu(p)
	case overlayDialog:
		m.drawDialog(p)
	}
	m.drawToast(p, now)
}

// hint is one entry of the hint bar: the role of a button and the word for what it does.
type hint struct {
	role  input.Action
	label string
}

// drawFrame draws the top bar and the hint bar of docs/ux/gui.md section 4. screen is the crumb after the
// brand (empty on Home).
func (m Model) drawFrame(p painter, screen string, hints []hint) {
	m.drawTopBar(p, screen)
	m.drawHintBar(p, hints, true)
}

func (m Model) queueBadge() string {
	active, queued := m.jobs.counts()
	switch {
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
func (m Model) topBarItems(p painter, avail int) ([]topBarItem, int) {
	met := m.metrics
	gap := 2 * met.Unit
	size := met.Secondary
	var fixedBefore, fixedAfter []topBarItem
	if m.offline {
		fixedBefore = append(fixedBefore, topBarItem{s: m.tr("ui.gui.bar.offline", nil), c: colWarn})
	}
	if badge := m.queueBadge(); badge != "" {
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

func (m Model) drawTopBar(p painter, screen string) {
	met := m.metrics
	bar := met.TopBarRect()
	p.l.Fill(bar, 0, colSurface)
	pad := 2 * met.Unit

	title := brand
	if screen != "" {
		title += crumb + screen
	}
	// The title always keeps the width of the screen's own name, and the right side takes the rest.
	minTitle := min(p.width(met.Title, title), p.width(met.Title, brand))
	if screen != "" {
		minTitle = min(p.width(met.Title, title), p.width(met.Title, brand+crumb+"…"))
	}
	avail := max(bar.W-2*pad-minTitle-pad, 0)
	right, _ := m.topBarItems(p, avail)

	x := bar.Right() - pad
	for i := len(right) - 1; i >= 0; i-- {
		if right[i].arrow {
			x = p.arrowLabel(x, bar.Y, bar.H, met.Secondary, right[i].c, right[i].s) - pad
			continue
		}
		x = p.rightAligned(x, bar.Y, bar.H, met.Secondary, right[i].c, right[i].s) - pad
	}
	title = p.fit(met.Title, title, max(x-(bar.X+pad), 0))
	p.text(bar.X+pad, bar.Y+(bar.H-LineHeight(met.Title))/2, met.Title, colText, title)
}

// pillWidth is how wide a pill for label is: a disc for a one-letter label, and a pill with 1u of air at each
// end for a word.
func (p painter) pillWidth(h int, label string) int {
	if len([]rune(label)) > 1 {
		return max(h, p.width(p.m.Secondary, label)+2*p.m.Unit)
	}
	return max(h, p.width(p.m.Secondary, label)+p.m.Unit)
}

// pill draws a button label on a dark disc (a pill for a longer label) and returns its width.
func (p painter) pill(x, y, h int, label string) int {
	w := p.pillWidth(h, label)
	p.l.Fill(rect{X: x, Y: y, W: w, H: h}, h/2, colBG)
	p.centered(rect{X: x, Y: y, W: w, H: h}, p.m.Secondary, colText, label)
	return w
}

func (m Model) drawHintBar(p painter, hints []hint, withStart bool) {
	met := m.metrics
	bar := met.HintBarRect()
	p.l.Fill(bar, 0, colSurface)
	pad := 2 * met.Unit
	h := max(bar.H-met.Unit, 20)
	y := bar.Y + (bar.H-h)/2
	size := met.Secondary

	// Start Menu, at the left; compact keeps the disc and drops the word.
	x := bar.X + pad
	if withStart {
		x += p.pill(x, y, h, input.PositionLabel(input.Start, m.pad)) + met.Unit/2
		if met.Tier != Compact {
			p.text(x, bar.Y+(bar.H-LineHeight(size))/2, size, colDim, m.tr("ui.gui.hint.menu", nil))
		}
	}

	limit := maxHints
	if met.Tier == Compact {
		limit = 4
	}
	if len(hints) > limit {
		hints = hints[:limit]
	}
	gap := 2 * met.Unit
	total := 0
	type placed struct{ label, word string }
	items := make([]placed, 0, len(hints))
	for _, hnt := range hints {
		label := input.Hint(hnt.role, m.pad, m.cfg.confirmEast)
		items = append(items, placed{label, hnt.label})
		total += p.pillWidth(h, label) + met.Unit/2 + p.width(size, hnt.label) + gap
	}
	x = bar.Right() - pad - (total - gap)
	for _, it := range items {
		x += p.pill(x, y, h, it.label) + met.Unit/2
		p.text(x, bar.Y+(bar.H-LineHeight(size))/2, size, colDim, it.word)
		x += p.width(size, it.word) + gap
	}
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
