package gui

import (
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The Buttons overlay (docs/ux/gui.md section 5.11): the button map of the screen underneath, with the labels printed
// on the pad in hand. It lists only the buttons that screen uses: A, then B, then Start, then the rest in the order of the
// hint bar (with the words the hint bar uses, including the ones its fit rule drops), then L and R, Select and the D-pad.

// screenHints are the hints of the screen under the overlay, before the hint bar drops any.
func (m Model) screenHints() []hint {
	switch m.screen {
	case screenSearch:
		return m.searchFrame().hints
	case screenPicker:
		return m.pickerFrame().hints
	case screenResults:
		return m.resultsFrame().hints
	case screenEntry:
		return m.entryFrame().hints
	case screenQueue:
		return m.queueFrame().hints
	case screenHistory:
		return m.historyFrame().hints
	case screenSettings:
		return m.settingsFrame().hints
	}
	return m.hintsHome()
}

// buttonRow is one line of the overlay: the labels of one or two buttons and the word for what they do.
type buttonRow struct {
	labels []string
	word   string
}

func (m Model) buttonRows() []buttonRow {
	pad := func(role input.Action) string { return input.Hint(role, m.pad, m.cfg.confirmEast) }
	hints := m.screenHints()
	var first, rest, shoulders []buttonRow
	for _, h := range hints {
		if h.role == input.None { // a note, not a button
			continue
		}
		row := buttonRow{labels: []string{pad(h.role)}, word: h.label}
		if h.role2 != input.None {
			row.labels = append(row.labels, pad(h.role2))
		}
		switch h.role {
		case input.Confirm:
			first = append([]buttonRow{row}, first...)
		case input.Back:
			first = append(first, row)
		case input.PageUp:
			shoulders = append(shoulders, row)
		default:
			rest = append(rest, row)
		}
	}
	startRow := buttonRow{labels: []string{input.PositionLabel(input.Start, m.pad)}, word: m.tr("ui.gui.hint.menu", nil)}
	rows := append(first, startRow) // A, B, then Start, then the rest in the order of the hint bar
	rows = append(append(rows, rest...), shoulders...)
	if m.screen != screenQueue { // Select opens the Queue from any screen but the Queue itself
		rows = append(rows, buttonRow{labels: []string{input.PositionLabel(input.Select, m.pad)}, word: m.tr("ui.gui.menu.queue", nil)})
	}
	return append(rows, buttonRow{labels: []string{"↑", "↓", "←", "→"}, word: m.tr("ui.gui.buttons.move", nil)})
}

func (m Model) openButtons() Model {
	m.overlay = overlayButtons
	m.epoch++
	return m
}

func (m Model) drawButtons(p painter) {
	met := m.metrics
	u := met.Unit
	rows := m.buttonRows()
	title := m.tr("ui.gui.menu.buttons", nil)
	pillH := max(LineHeight(met.Secondary)+u, 20)
	rowH := pillH + u
	widest := p.width(met.Body, title)
	for _, r := range rows {
		w := 0
		for _, l := range r.labels {
			w += p.pillWidth(pillH, l) + u/4
		}
		widest = max(widest, w+u+p.width(met.Secondary, r.word)+4*u)
	}
	panelW := min(widest+4*u, met.Safe.W-4*u)
	panelH := LineHeight(met.Body) + 3*u + len(rows)*rowH
	panel := draw.Rect{X: met.Safe.X + (met.Safe.W-panelW)/2, Y: met.Safe.Y + max((met.Safe.H-panelH)/2, 0), W: panelW, H: min(panelH, met.Safe.H)}
	p.l.Fill(draw.Rect{W: met.W, H: met.H}, 0, colScrim)
	p.l.Fill(panel, met.Radius, colSurface)
	p.text(panel.X+2*u, panel.Y+u, met.Body, colText, title)
	y := panel.Y + LineHeight(met.Body) + 2*u
	for _, r := range rows {
		x := panel.X + 2*u
		for _, l := range r.labels {
			x += p.pill(x, y, pillH, l) + u/4
		}
		p.text(x+u/2, y+(pillH-LineHeight(met.Secondary))/2, met.Secondary, colDim, r.word)
		y += rowH
	}
}
