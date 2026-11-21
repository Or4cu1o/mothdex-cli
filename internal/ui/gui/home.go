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

const (
	regionTiles = 0
	regionChips = 1
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

	footerH := LineHeight(met.Secondary)
	chipH := LineHeight(met.Body) + 2*gap
	g := homeGeom{footerTop: c.Bottom() - footerH}
	tw := min(met.TileW, (c.W-(cols-1)*gap)/cols)

	// Recent searches have priority over the full tile height: tiles shrink to make room for them, and the
	// row is dropped only when even small tiles would not leave it any (and in compact when the screen has
	// fewer than 7 rows of space).
	crowded := met.Tier == Compact && c.H < 7*met.Row1
	minTile := 2 * LineHeight(met.Body)
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
		return m.activateHome(now), nil
	case input.Back:
		return m.askQuit(QuitFromBack, now)
	case input.Menu:
		return m.openMenu(), nil
	case input.Queue:
		return m.notYet(tileQueue, now), nil
	}
	return m, nil
}

// activateHome opens what has focus. The screens behind the tiles come with the next pull requests: until
// then a tile says so in a toast.
func (m Model) activateHome(now time.Time) Model {
	if strings.HasPrefix(m.focus, chipPrefix) {
		return m.notYet(tileSearch, now)
	}
	return m.notYet(m.focus, now)
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
func (m Model) footer() (string, bool) {
	root := app.SanitizeText(m.profile.RomRoot)
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
	for _, t := range g.tiles {
		p.box(t.rect, m.focus == t.id)
		label := m.tileName(t.id)
		if t.id == tileQueue {
			if n := m.jobs.unfinished(); n > 0 {
				label += "  " + strconv.Itoa(n)
			}
		}
		p.centered(t.rect, met.Body, colText, p.fit(met.Body, label, t.rect.W-2*met.Unit))
	}
	if g.showRecent {
		top := g.recentTop + (LineHeight(met.Body)+2*met.Unit-LineHeight(met.Secondary))/2
		p.text(g.recentX, top, met.Secondary, colDim, g.recentLabel)
		for _, c := range g.chips {
			p.box(c.rect, m.focus == c.id)
			p.centered(c.rect, met.Body, colText, c.label)
		}
	}
	text, warn := m.footer()
	color := colDim
	if warn {
		color = colWarn
	}
	c := met.ContentRect()
	p.text(c.X, g.footerTop, met.Secondary, color, p.fit(met.Secondary, text, c.W))
}
