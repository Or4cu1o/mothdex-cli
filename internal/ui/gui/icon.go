package gui

import "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"

// The font has no arrows, check marks or crosses (assets/PINS.md), so the few symbols the GUI needs are
// drawn as shapes.

// arrowSize is the box of a down arrow beside text of the given size: about the height of a capital.
func arrowSize(textSize int) (w, h int) { return max(textSize/2, 6), max(textSize*7/10, 8) }

// arrowDown draws a down arrow (a stem and a triangular head) in a w x h box with its top left at (x, y).
func arrowDown(l *draw.List, x, y, w, h int, c draw.Color) {
	stemW := max(2, w/4)
	headH := (h + 1) / 2
	stemH := h - headH
	l.Fill(draw.Rect{X: x + (w-stemW)/2, Y: y, W: stemW, H: stemH}, 0, c)
	for i := 0; i < headH; i++ { // the head narrows one row at a time
		rowW := max(1, w*(headH-i)/headH)
		l.Fill(draw.Rect{X: x + (w-rowW)/2, Y: y + stemH + i, W: rowW, H: 1}, 0, c)
	}
}

// arrowLabel draws a down arrow followed by a label, the label ending at right, in a band that starts at top
// and is bandH tall. It returns the left edge of what it drew.
func (p painter) arrowLabel(right, top, bandH, size int, c draw.Color, label string) int {
	aw, ah := arrowSize(size)
	gap := max(p.m.Unit/2, 3)
	x := p.rightAligned(right, top, bandH, size, c, label)
	x -= gap + aw
	arrowDown(p.l, x, top+(bandH-ah)/2, aw, ah, c)
	return x
}

// arrowLabelWidth is how wide arrowLabel draws.
func (p painter) arrowLabelWidth(size int, label string) int {
	aw, _ := arrowSize(size)
	return aw + max(p.m.Unit/2, 3) + p.width(size, label)
}
