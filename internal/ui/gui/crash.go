package gui

import (
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The crash screen is what the person sees after a panic of the render thread (docs/contracts/cli/gui.md
// section 5): the ref, the log, and the two things left to do, export the logs or quit.

// Ids of the crash screen buttons.
const (
	crashExport = "crash_export"
	crashQuit   = "crash_quit"
)

// NewCrash is the model of the crash screen for the failure the log holds as ref. It never calls the service:
// whatever broke is left alone. settings are the configuration as ConfigList gives it, so the confirm button and
// the sizes stay the person's; nil keeps the defaults.
func NewCrash(o Options, settings []app.Setting, ref string, now time.Time) Model {
	m := Model{
		o:      o,
		cfg:    settingsFromOptions(o),
		screen: screenCrash,
		start:  now,
		focus:  crashQuit,
	}
	if o.ExportLogs != nil {
		m.focus = crashExport
	}
	m.metrics = NewMetrics(o.Width, o.Height, m.cfg.safe, m.cfg.largeText)
	if settings != nil {
		m = m.onConfig(configMsg{list: settings})
	}
	path := m.logPath()
	m.crash = m.failureWith(&app.Error{Code: app.CodeInternal}, path)
	m.crash.ref, m.crash.log, m.crash.hint = ref, path, ""
	return m
}

func settingsFromOptions(o Options) settings {
	return settings{confirmEast: o.ConfirmEast, largeText: o.LargeText, safe: o.Safe}
}

func (m Model) updateCrash(msg Msg, now time.Time) (Model, []Cmd) {
	switch msg := msg.(type) {
	case InputMsg:
		if m.overlay == overlayDialog {
			if now.Before(m.ignoreUntil) {
				return m, nil
			}
			return m.onDialogAction(msg.Action, now)
		}
		if d, ok := dirOf(msg.Action); ok {
			m.focus = Navigate(m.crashElements(), m.focus, d)
			return m, nil
		}
		switch msg.Action {
		case input.Confirm:
			if m.focus == crashExport {
				return m.askExport(now), nil
			}
			m.done = true
		case input.Back:
			m.done = true
		}
	case exportDoneMsg:
		return m.onExportDone(msg, now), nil
	case ResizeMsg:
		m.metrics = NewMetrics(msg.W, msg.H, m.cfg.safe, m.cfg.largeText)
	case ControllerMsg:
		m.pad = msg.Type
	case SignalMsg, QuitRequestMsg:
		m.done = true
	}
	return m, nil
}

func (m Model) crashFrame() frameSpec {
	return frameSpec{hints: []hint{
		{role: input.Confirm, label: m.tr("ui.gui.hint.choose", nil)},
		{role: input.Back, label: m.tr("ui.gui.quit.quit", nil)},
	}}
}

type crashGeom struct {
	lines []visualLine
	x, y  int
	boxes []buttonBox
}

// crashGeometry stacks the text from the top of the content area and puts the buttons under it.
func (m Model) crashGeometry(p painter) crashGeom {
	met := m.metrics
	u := met.Unit
	g := crashGeom{x: met.Safe.X + 2*u, y: met.Safe.Y + LineHeight(met.Title) + 3*u}
	w := met.Safe.W - 4*u
	add := func(size int, c draw.Color, gap int, lines ...string) {
		for i, line := range lines {
			l := visualLine{text: line, x: size, c: c} // x carries the text size here
			if i == 0 {
				l.gap = gap
			}
			g.lines = append(g.lines, l)
		}
	}
	add(met.Body, colError, 0, p.wrap(met.Body, m.tr("ui.gui.crash.title", nil), w, 2)...)
	add(met.Secondary, colText, u, p.wrap(met.Secondary, m.tr("ui.gui.crash.body", nil), w, 3)...)
	add(met.Secondary, colDim, u, m.supportLines(p, m.crash, w)...)

	y := g.y
	for _, l := range g.lines {
		y += l.gap + LineHeight(l.x)
	}
	y += 2 * u
	buttons := []dialogButton{{crashQuit, m.tr("ui.gui.quit.quit", nil)}}
	if m.o.ExportLogs != nil {
		buttons = append([]dialogButton{{crashExport, m.tr("ui.gui.logs.export", nil)}}, buttons...)
	}
	x := g.x
	for _, b := range buttons {
		r := rect{X: x, Y: y, W: p.width(met.Body, b.label) + 4*u, H: met.Row1}
		g.boxes = append(g.boxes, buttonBox{button: b, rect: r})
		x += r.W + 2*u
	}
	return g
}

func (m Model) crashElements() []Element {
	var out []Element
	for _, b := range m.crashGeometry(m.painter(nil)).boxes {
		out = append(out, Element{ID: b.button.id, Rect: b.rect})
	}
	return out
}

func (m Model) drawCrash(p painter) {
	met := m.metrics
	g := m.crashGeometry(p)
	y := g.y
	for _, l := range g.lines {
		y += l.gap
		p.text(g.x, y, l.x, l.c, l.text)
		y += LineHeight(l.x)
	}
	for _, b := range g.boxes {
		p.box(b.rect, m.focus == b.button.id && m.overlay == overlayNone)
		p.centered(b.rect, met.Body, colText, b.button.label)
	}
}
