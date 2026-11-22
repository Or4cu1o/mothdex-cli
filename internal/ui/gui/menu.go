package gui

import (
	"strconv"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Ids of the Start menu items (docs/ux/gui.md section 5.11).
const (
	menuHome     = "menu:home"
	menuQueue    = "menu:queue"
	menuHistory  = "menu:history"
	menuSettings = "menu:settings"
	menuButtons  = "menu:buttons"
	menuDontQuit = "menu:dont_quit"
	menuQuit     = "menu:quit"
)

type menuItem struct {
	id    string
	label string
	badge string
}

// menuItems are the items for the state of the app: the screen actions sit between Buttons and Quit, and
// today the only one is Don't quit, while a quit is pending.
func (m Model) menuItems() []menuItem {
	queue := menuItem{id: menuQueue, label: m.tr("ui.gui.menu.queue", nil)}
	if n := m.jobs.unfinished(); n > 0 {
		queue.badge = strconv.Itoa(n)
	}
	items := []menuItem{
		{id: menuHome, label: m.tr("ui.gui.menu.home", nil)},
		queue,
		{id: menuHistory, label: m.tr("ui.gui.menu.history", nil)},
		{id: menuSettings, label: m.tr("ui.gui.menu.settings", nil)},
		{id: menuButtons, label: m.tr("ui.gui.menu.buttons", nil)},
	}
	if m.quitPending {
		items = append(items, menuItem{id: menuDontQuit, label: m.tr("ui.gui.menu.dont_quit", nil)})
	}
	return append(items, menuItem{id: menuQuit, label: m.tr("ui.gui.menu.quit", nil)})
}

type menuRowBox struct {
	item menuItem
	rect rect
}

type menuGeom struct {
	panel rect
	rows  []menuRowBox
	elems []Element
}

// menuGeometry centers the panel in the safe rectangle; rows share the panel, one element each.
func (m Model) menuGeometry() menuGeom {
	met := m.metrics
	p := m.painter(nil)
	u := met.Unit
	items := m.menuItems()

	widest := 0
	for _, it := range items {
		w := p.width(met.Body, it.label)
		if it.badge != "" {
			w += 3*u + p.arrowLabelWidth(met.Secondary, it.badge)
		}
		widest = max(widest, w)
	}
	panelW := min(max(widest+6*u, 12*met.Body), met.Safe.W-4*u)
	rowH := min(met.Row1, (met.Safe.H-4*u)/len(items))
	panelH := len(items)*rowH + 2*u
	g := menuGeom{panel: rect{
		X: met.Safe.X + (met.Safe.W-panelW)/2,
		Y: met.Safe.Y + (met.Safe.H-panelH)/2,
		W: panelW, H: panelH,
	}}
	for i, it := range items {
		r := rect{X: g.panel.X + u, Y: g.panel.Y + u + i*rowH, W: panelW - 2*u, H: rowH}
		g.rows = append(g.rows, menuRowBox{item: it, rect: r})
		g.elems = append(g.elems, Element{ID: it.id, Rect: r})
	}
	return g
}

func (m Model) openMenu() Model {
	m.overlay, m.menuFocus = overlayMenu, menuHome
	m.epoch++
	return m
}

func (m Model) closeOverlay() Model {
	m.overlay, m.dlg = overlayNone, nil
	m.epoch++
	return m
}

func (m Model) onMenuAction(a input.Action, now time.Time) (Model, []Cmd) {
	if d, ok := dirOf(a); ok {
		m.menuFocus = Navigate(m.menuGeometry().elems, m.menuFocus, d)
		return m, nil
	}
	switch a {
	case input.Confirm:
		return m.activateMenu(m.menuFocus, now)
	case input.Back, input.Menu:
		return m.closeOverlay(), nil
	}
	return m, nil
}

func (m Model) activateMenu(id string, now time.Time) (Model, []Cmd) {
	m = m.closeOverlay()
	switch id {
	case menuQueue:
		return m.notYetNamed(m.tr("ui.gui.menu.queue", nil), now), nil
	case menuHistory:
		return m.notYetNamed(m.tr("ui.gui.menu.history", nil), now), nil
	case menuSettings:
		return m.notYetNamed(m.tr("ui.gui.menu.settings", nil), now), nil
	case menuButtons:
		return m.notYetNamed(m.tr("ui.gui.menu.buttons", nil), now), nil
	case menuDontQuit:
		m.quitPending = false
	case menuQuit:
		return m.askQuit(QuitFromMenu, now)
	}
	return m, nil
}

func (m Model) drawMenu(p painter) {
	met := m.metrics
	p.l.Fill(rect{W: met.W, H: met.H}, 0, colScrim)
	g := m.menuGeometry()
	p.l.Fill(g.panel, met.Radius, colSurface)
	for _, row := range g.rows {
		focused := m.menuFocus == row.item.id
		if focused {
			p.box(row.rect, true)
		}
		top := row.rect.Y + (row.rect.H-LineHeight(met.Body))/2
		p.text(row.rect.X+met.Unit*2, top, met.Body, colText, row.item.label)
		if row.item.badge != "" {
			p.arrowLabel(row.rect.Right()-2*met.Unit, row.rect.Y, row.rect.H, met.Secondary, colAccent, row.item.badge)
		}
	}
}
