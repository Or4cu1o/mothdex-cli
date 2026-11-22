package gui

import "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"

// The font has no arrows, check marks or crosses (assets/PINS.md), so the few symbols the GUI needs are
// drawn as shapes.

// arrowSize is the box of a down arrow beside text of the given size: about the height of a capital.
func arrowSize(textSize int) (w, h int) { return max(textSize/2, 6), max(textSize*7/10, 8) }

// arrowDown draws a down arrow (a stem and a triangular head) in a w x h box with its top left at (x, y), on the pixel
// grid: the stem is whole cells wide and the head narrows by whole cells.
func arrowDown(l *draw.List, cell, x, y, w, h int, c draw.Color) {
	stemW := max(cell, snapDown(w/4, cell))
	headRows := max(h/cell/2, 1)
	stemRows := max(h/cell-headRows, 1)
	stemX := snapDown(x+(w-stemW)/2, cell)
	l.Fill(draw.Rect{X: stemX, Y: y, W: stemW, H: stemRows * cell}, c)
	for i := 0; i < headRows; i++ { // the head narrows one row at a time
		rowW := max(stemW, snapDown(w*(headRows-i)/headRows, 2*cell)+stemW%(2*cell))
		l.Fill(draw.Rect{X: stemX + stemW/2 - rowW/2, Y: y + (stemRows+i)*cell, W: rowW, H: cell}, c)
	}
}

// arrowLabel draws a down arrow followed by a label, the label ending at right, in a band that starts at top
// and is bandH tall. It returns the left edge of what it drew.
func (p painter) arrowLabel(right, top, bandH, size int, c draw.Color, label string) int {
	aw, ah := arrowSize(size)
	gap := max(p.m.Unit/2, 3)
	x := p.rightAligned(right, top, bandH, size, c, label)
	x -= gap + aw
	arrowDown(p.l, p.m.Pixel, x, top+(bandH-ah)/2, aw, ah, c)
	return x
}

// arrowLabelWidth is how wide arrowLabel draws.
func (p painter) arrowLabelWidth(size int, label string) int {
	aw, _ := arrowSize(size)
	return aw + max(p.m.Unit/2, 3) + p.width(size, label)
}
