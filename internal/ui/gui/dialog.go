package gui

import (
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Ids of the dialog buttons.
const (
	btnStay         = "stay"
	btnQuit         = "quit"
	btnKeep         = "keep"
	btnWhenFinished = "when_finished"
	btnCancelQuit   = "cancel_quit"
	btnCancel       = "cancel"
	btnDownload     = "download"
	btnAgain        = "again"
	btnCancelAll    = "cancel_all"
)

// dialogKind says what a dialog is about, so its buttons know what to do.
type dialogKind int

const (
	dialogQuit            dialogKind = iota // the quit questions of section 5.13
	dialogConfirmDownload                   // a download above 500 MB with interface.confirm_downloads on
	dialogDownloadAgain                     // the link was downloaded before
	dialogCancelAll                         // the Queue's Cancel all downloads
)

type dialogButton struct {
	id    string
	label string
}

// dialogState is one dialog (docs/ux/gui.md section 5.12): a title, an optional detail, up to three buttons.
// The first button is the safe one: it has the initial focus and B activates it.
type dialogState struct {
	kind    dialogKind
	title   string
	detail  string
	buttons []dialogButton
	initial int      // the button that has focus first; 0, the safe one, unless a screen says otherwise
	pending download // the download a confirmation is about
}

// askQuit starts a quit by the table of section 5.13.
func (m Model) askQuit(from QuitRequest, now time.Time) (Model, []Cmd) {
	unfinished := m.jobs.unfinished()
	switch DecideQuit(from, unfinished) {
	case QuitNow:
		return m.startExit(false, false, now)
	case QuitConfirm:
		title := m.tr("ui.gui.quit.ask_generic", nil)
		if m.profile.ID != "generic" {
			title = m.tr("ui.gui.quit.ask", map[string]any{"frontend": app.SanitizeText(m.profile.Name)})
		}
		return m.openDialog(dialogState{title: title, buttons: []dialogButton{
			{btnStay, m.tr("ui.gui.quit.stay", nil)},
			{btnQuit, m.tr("ui.gui.quit.quit", nil)},
		}}, now), nil
	}
	return m.openDialog(dialogState{
		title: m.o.T.N("ui.gui.quit.busy", unfinished, i18n.Args{"count": unfinished}),
		buttons: []dialogButton{
			{btnKeep, m.tr("ui.gui.quit.keep", nil)},
			{btnWhenFinished, m.tr("ui.gui.quit.when_finished", nil)},
			{btnCancelQuit, m.tr("ui.gui.quit.cancel_and_quit", nil)},
		},
	}, now), nil
}

// openDialog shows a dialog with focus on its first button and ignores input for a moment, so a button the
// person was still holding does not answer it.
func (m Model) openDialog(d dialogState, now time.Time) Model {
	m.overlay, m.dlg, m.dlgFocus = overlayDialog, &d, d.buttons[min(d.initial, len(d.buttons)-1)].id
	m.ignoreUntil = now.Add(dialogIgnore)
	m.epoch++
	return m
}

func (m Model) notYetNamed(name string, now time.Time) Model {
	return m.withToast(m.tr("ui.gui.not_yet", map[string]any{"name": name}), toastInfo, now)
}

func (m Model) onDialogAction(a input.Action, now time.Time) (Model, []Cmd) {
	if d, ok := dirOf(a); ok {
		m.dlgFocus = Navigate(m.dialogGeometry().elems, m.dlgFocus, d)
		return m, nil
	}
	switch a {
	case input.Confirm:
		return m.activateDialog(m.dlgFocus, now)
	case input.Back:
		return m.activateDialog(m.dlg.buttons[0].id, now) // B equals the cancel option
	}
	return m, nil
}

func (m Model) activateDialog(id string, now time.Time) (Model, []Cmd) {
	d := m.dlg
	m = m.closeOverlay()
	switch d.kind {
	case dialogConfirmDownload, dialogDownloadAgain:
		if id == btnDownload || id == btnAgain {
			return m.enqueue(d.pending, now)
		}
		return m, nil
	case dialogCancelAll:
		if id == btnCancelAll {
			return m, []Cmd{m.cancelAllCmd()}
		}
		return m, nil
	}
	switch id {
	case btnQuit:
		return m.startExit(false, false, now)
	case btnWhenFinished:
		if m.jobs.unfinished() == 0 { // the last job ended while the dialog was open: nothing is left to wait for
			return m.startExit(false, false, now)
		}
		m.quitPending = true
		return m.openQueue(now), nil // the Queue opens so the person sees what is being waited for
	case btnCancelQuit:
		return m.startExit(false, true, now)
	}
	return m, nil
}

type buttonBox struct {
	button dialogButton
	rect   rect
}

type dialogGeom struct {
	panel       rect
	titleLines  []string
	titleTop    int
	detailLines []string
	detailTop   int
	buttons     []buttonBox
	elems       []Element
}

const dialogMaxLines = 3

// dialogGeometry lays out the open dialog: the text wrapped to a comfortable measure, then the buttons in a
// row, or stacked when the row would not fit the safe rectangle.
func (m Model) dialogGeometry() dialogGeom {
	met := m.metrics
	p := m.painter(nil)
	d := m.dlg
	u := met.Unit
	pad := 2 * u
	g := dialogGeom{}

	maxText := min(met.Safe.W-4*u-2*pad, 24*met.Body)
	g.titleLines = p.wrap(met.Body, d.title, maxText, dialogMaxLines)
	if d.detail != "" {
		g.detailLines = p.wrap(met.Secondary, d.detail, maxText, dialogMaxLines)
	}
	textW := 0
	for _, l := range g.titleLines {
		textW = max(textW, p.width(met.Body, l))
	}
	for _, l := range g.detailLines {
		textW = max(textW, p.width(met.Secondary, l))
	}

	btnH := met.Row1
	widths := make([]int, len(d.buttons))
	rowW := u * (len(d.buttons) - 1)
	widest := 0
	for i, b := range d.buttons {
		widths[i] = p.width(met.Body, b.label) + 4*u
		rowW += widths[i]
		widest = max(widest, widths[i])
	}
	stacked := rowW > met.Safe.W-4*u-2*pad
	inner := max(textW, rowW)
	btnBlockH := btnH
	if stacked {
		inner = max(textW, widest)
		btnBlockH = len(d.buttons)*btnH + (len(d.buttons)-1)*u
	}

	textH := len(g.titleLines) * LineHeight(met.Body)
	if len(g.detailLines) > 0 {
		textH += u + len(g.detailLines)*LineHeight(met.Secondary)
	}
	panelW := min(inner+2*pad, met.Safe.W-4*u)
	panelH := pad + textH + 2*u + btnBlockH + pad
	g.panel = rect{
		X: met.Safe.X + (met.Safe.W-panelW)/2,
		Y: met.Safe.Y + (met.Safe.H-panelH)/2,
		W: panelW, H: panelH,
	}
	g.titleTop = g.panel.Y + pad
	g.detailTop = g.titleTop + len(g.titleLines)*LineHeight(met.Body) + u
	y := g.panel.Y + pad + textH + 2*u
	x := g.panel.X + pad
	for i, b := range d.buttons {
		var r rect
		if stacked {
			r = rect{X: x, Y: y + i*(btnH+u), W: inner, H: btnH}
		} else {
			r = rect{X: x, Y: y, W: widths[i], H: btnH}
			x += widths[i] + u
		}
		g.buttons = append(g.buttons, buttonBox{button: b, rect: r})
		g.elems = append(g.elems, Element{ID: b.id, Rect: r})
	}
	return g
}

func (m Model) drawDialog(p painter) {
	met := m.metrics
	p.l.Fill(rect{W: met.W, H: met.H}, 0, colScrim)
	g := m.dialogGeometry()
	p.l.Fill(g.panel, met.Radius, colSurface)
	for i, line := range g.titleLines {
		p.text(g.panel.X+2*met.Unit, g.titleTop+i*LineHeight(met.Body), met.Body, colText, line)
	}
	for i, line := range g.detailLines {
		p.text(g.panel.X+2*met.Unit, g.detailTop+i*LineHeight(met.Secondary), met.Secondary, colDim, line)
	}
	for _, b := range g.buttons {
		if m.dlgFocus == b.button.id {
			p.box(b.rect, true)
		} else {
			p.l.Fill(b.rect, met.Radius, colBG)
		}
		p.centered(b.rect, met.Body, colText, p.fit(met.Body, b.button.label, b.rect.W-met.Unit))
	}
}
