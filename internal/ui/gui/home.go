package gui

import (
	"strconv"
	"strings"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Ids of the Home tiles, in reading order.
const (
	tileSearch    = "search"
	tilePlatforms = "platforms"
	tileRandom    = "random"
	tileQueue     = "queue"
	tileHistory   = "history"
	tileSettings  = "settings"
	chipPrefix    = "recent:"
)

var homeTiles = []string{tileSearch, tilePlatforms, tileRandom, tileQueue, tileHistory, tileSettings}

// footerLines is how many lines the Home footer may take; the layout always reserves them.
const footerLines = 2

const (
	regionTiles = 0
	regionChips = 1
	regionKeys  = 2 // the keys of the virtual keyboard; chips above them are region 0
)

type tileBox struct {
	id   string
	rect rect
}

type chipBox struct {
	id    string
	label string
	rect  rect
}

// homeGeom is where everything on Home is. Drawing and focus movement both read it, so they cannot disagree.
type homeGeom struct {
	tiles       []tileBox
	chips       []chipBox
	recentLabel string
	recentX     int // left edge of the "Recent" label
	recentTop   int
	showRecent  bool
	footerTop   int
	elems       []Element
}

// homeGeometry lays out Home (docs/ux/gui.md section 5.2): tiles in a 3 x 2 grid (2 x 3 when narrow),
// then the recent searches when there is room, then the footer at the bottom of the content area. Tiles
// shrink before anything leaves the safe rectangle.
func (m Model) homeGeometry() homeGeom {
	met := m.metrics
	p := m.painter(nil)
	c := met.ContentRect()
	gap := met.Unit
	cols, rows := 3, 2
	if met.Narrow {
		cols, rows = 2, 3
	}

	footerH := footerLines * LineHeight(met.Secondary) // the layout reserves both lines
	chipH := LineHeight(met.Body) + 2*gap
	g := homeGeom{footerTop: c.Bottom() - footerH}
	tw := min(met.TileW, (c.W-(cols-1)*gap)/cols)

	// Recent searches have priority over the full tile height: tiles shrink to make room for them, and the
	// row is dropped only when even small tiles would not leave it any (and in compact when the screen has
	// fewer than 7 rows of space).
	crowded := met.Tier == Compact && c.H < 7*met.Row1
	minTile := 2*LineHeight(met.Body) + gap // a label may wrap to two lines
	tileHeight := func(withRecent bool) int {
		reserved := footerH + gap
		if withRecent {
			reserved += chipH + gap
		}
		return min(met.TileH, (c.H-reserved-(rows-1)*gap)/rows)
	}
	g.showRecent = len(m.recent) > 0 && !crowded
	th := tileHeight(g.showRecent)
	if g.showRecent && th < minTile {
		g.showRecent, th = false, tileHeight(false)
	}
	th = max(th, minTile)

	for i, id := range homeTiles {
		col, row := i%cols, i/cols
		r := rect{X: c.X + col*(tw+gap), Y: c.Y + row*(th+gap), W: tw, H: th}
		g.tiles = append(g.tiles, tileBox{id: id, rect: r})
		g.elems = append(g.elems, Element{ID: id, Rect: r, Region: regionTiles, WrapRow: true})
	}

	g.recentTop = c.Y + rows*th + (rows-1)*gap + gap
	if !g.showRecent {
		return g
	}

	g.recentLabel = m.tr("ui.gui.home.recent", nil)
	g.recentX = c.X
	x := c.X + p.width(met.Secondary, g.recentLabel) + 2*gap
	maxChip := max(c.W/3, 4*gap)
	for i, q := range m.recent {
		label := p.fit(met.Body, q, maxChip-4*gap)
		w := p.width(met.Body, label) + 4*gap
		if x+w > c.Right() {
			break
		}
		r := rect{X: x, Y: g.recentTop, W: w, H: chipH}
		id := chipPrefix + strconv.Itoa(i)
		g.chips = append(g.chips, chipBox{id: id, label: label, rect: r})
		g.elems = append(g.elems, Element{ID: id, Rect: r, Region: regionChips, WrapRow: true})
		x += w + gap
	}
	return g
}

func (m Model) homeElements() []Element { return m.homeGeometry().elems }

func (m Model) tileName(id string) string { return m.tr("ui.gui.home."+id, nil) }

func (m Model) onHomeAction(a input.Action, now time.Time) (Model, []Cmd) {
	if d, ok := dirOf(a); ok {
		m.focus = Navigate(m.homeElements(), m.focus, d)
		return m, nil
	}
	switch a {
	case input.Confirm:
		return m.activateHome(now)
	case input.Back:
		return m.askQuit(QuitFromBack, now)
	case input.Menu:
		return m.openMenu(), nil
	}
	return m, nil
}

// activateHome opens what has focus. The screens behind the tiles come with the next pull requests: until
// then a tile says so in a toast.
func (m Model) activateHome(now time.Time) (Model, []Cmd) {
	if strings.HasPrefix(m.focus, chipPrefix) { // a recent search runs again with no filters
		for _, c := range m.homeGeometry().chips {
			if c.id == m.focus {
				if i, err := strconv.Atoi(strings.TrimPrefix(c.id, chipPrefix)); err == nil && i < len(m.recent) {
					return m.startResults(app.SearchQuery{Text: m.recent[i]}, false, now)
				}
			}
		}
		return m, nil
	}
	switch m.focus {
	case tileSearch:
		return m.enterSearch(), nil
	case tilePlatforms:
		return m.openPicker(pickPlatform, true, now)
	case tileRandom:
		return m.startResults(app.SearchQuery{}, true, now)
	case tileQueue:
		return m.openQueue(), nil
	case tileHistory:
		return m.openHistory()
	case tileSettings:
		return m.openSettings()
	}
	return m.notYet(m.focus, now), nil
}

func (m Model) notYet(tile string, now time.Time) Model {
	return m.withToast(m.tr("ui.gui.not_yet", map[string]any{"name": m.tileName(tile)}), toastInfo, now)
}

func (m Model) hintsHome() []hint {
	return []hint{
		{role: input.Confirm, label: m.tr("ui.gui.hint.open", nil)},
		{role: input.Back, label: m.tr("ui.gui.hint.quit", nil)},
	}
}

// footer is the line at the bottom of Home and the color it takes.
func (m Model) footer(p painter) (string, bool) {
	root := shortenPath(p, m.metrics.Secondary, app.SanitizeText(m.profile.RomRoot), m.metrics.ContentRect().W)
	name := app.SanitizeText(m.profile.Name)
	switch {
	case m.db == dbFailed:
		return m.tr("ui.gui.home.footer_offline", nil), true
	case m.profile.ID == "generic":
		return m.tr("ui.gui.home.footer_generic", map[string]any{"path": root}), true
	case m.db == dbLoaded:
		return m.tr("ui.gui.home.footer", map[string]any{"entries": m.entriesText, "profile": name, "root": root}), false
	}
	return m.tr("ui.gui.home.footer_pending", map[string]any{"profile": name, "root": root}), false
}

func (m Model) drawHome(p painter) {
	met := m.metrics
	g := m.homeGeometry()
	p.drawSky(m.homeSkyAvoid(g))
	for _, t := range g.tiles {
		p.box(t.rect, m.focus == t.id)
		label := m.tileName(t.id)
		if t.id == tileQueue {
			if n := m.jobs.unfinished(); n > 0 {
				label += "  " + strconv.Itoa(n)
			}
		}
		size, lines := tileLabel(p, label, t.rect.W-2*met.Unit)
		top := t.rect.Y + (t.rect.H-len(lines)*LineHeight(size))/2
		for i, line := range lines {
			p.text(t.rect.X+(t.rect.W-p.width(size, line))/2, top+i*LineHeight(size), size, colText, line)
		}
	}
	if g.showRecent {
		top := g.recentTop + (LineHeight(met.Body)+2*met.Unit-LineHeight(met.Secondary))/2
		p.text(g.recentX, top, met.Secondary, colDim, g.recentLabel)
		for _, c := range g.chips {
			p.box(c.rect, m.focus == c.id)
			p.centered(c.rect, met.Body, colText, c.label)
		}
	}
	text, warn := m.footer(p)
	color := colDim
	if warn {
		color = colWarn
	}
	c := met.ContentRect()
	for i, line := range p.wrap(met.Secondary, text, c.W, footerLines) {
		p.text(c.X, g.footerTop+i*LineHeight(met.Secondary), met.Secondary, color, line)
	}
}

// homeSkyAvoid is what the sky of Home keeps clear: the bars, the tiles with their focus ring, the recent searches and
// the footer.
func (m Model) homeSkyAvoid(g homeGeom) []rect {
	met := m.metrics
	avoid := []rect{met.TopBarRect(), met.HintBarRect()}
	for _, t := range g.tiles {
		avoid = append(avoid, t.rect.Inset(-met.Ring))
	}
	for _, c := range g.chips {
		avoid = append(avoid, c.rect.Inset(-met.Ring))
	}
	if g.showRecent {
		avoid = append(avoid, rect{X: g.recentX, Y: g.recentTop, W: met.ContentRect().W, H: LineHeight(met.Body) + 2*met.Unit})
	}
	c := met.ContentRect()
	return append(avoid, rect{X: c.X, Y: g.footerTop, W: c.W, H: c.Bottom() - g.footerTop})
}

// tileLabel chooses how a tile label is drawn: at the body size, wrapped to at most two lines at its spaces. A
// single word wider than the tile is never split in the middle: the label drops to the secondary size, then to
// the minimum one (docs/ux/gui.md section 1.3), and only if even that does not fit is it cut.
func tileLabel(p painter, label string, maxW int) (size int, lines []string) {
	met := p.m
	for _, size := range []int{met.Body, met.Secondary, met.Minimum} {
		lines = p.wrap(size, label, maxW, 2)
		if wrappedWhole(lines, label) {
			return size, lines
		}
	}
	return met.Minimum, lines
}

// wrappedWhole reports whether wrapping only broke the text at its spaces: no word was cut or split.
func wrappedWhole(lines []string, text string) bool {
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ") == strings.Join(strings.Fields(text), " ") &&
		len(strings.Fields(strings.Join(lines, " "))) == len(strings.Fields(text))
}
