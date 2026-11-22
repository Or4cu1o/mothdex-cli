package gui

import (
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// The font has no check mark or cross (assets/PINS.md): they are drawn on the pixel grid, in squares of the cell P
// (DESIGN.md Components, status marks), so every stroke, gap and corner is a multiple of P.

// snapDown rounds n down to a multiple of cell.
func snapDown(n, cell int) int { return n - ((n%cell)+cell)%cell }

// strokeSquares draws a straight stroke from (x0,y0) to (x1,y1) as squares of the given thickness, which is at least
// one cell and a multiple of it; each square sits on the grid.
func strokeSquares(l *draw.List, cell, x0, y0, x1, y1, thick int, c draw.Color) {
	thick = max(cell, snapDown(thick, cell))
	steps := max(abs(x1-x0), abs(y1-y0)) / cell
	for i := 0; i <= max(steps, 0); i++ {
		x, y := x0, y0
		if steps > 0 {
			x, y = x0+(x1-x0)*i/steps, y0+(y1-y0)*i/steps
		}
		l.Fill(draw.Rect{X: snapDown(x-thick/2, cell), Y: snapDown(y-thick/2, cell), W: thick, H: thick}, c)
	}
}

// drawCheck draws a check mark in a size x size box with its top left at (x, y).
func drawCheck(l *draw.List, cell, x, y, size int, c draw.Color) {
	thick := max(cell, size/8)
	strokeSquares(l, cell, x+size/8, y+size*11/20, x+size*2/5, y+size*17/20, thick, c)
	strokeSquares(l, cell, x+size*2/5, y+size*17/20, x+size*7/8, y+size/6, thick, c)
}

// drawCross draws a cross in a size x size box with its top left at (x, y).
func drawCross(l *draw.List, cell, x, y, size int, c draw.Color) {
	thick := max(cell, size/8)
	strokeSquares(l, cell, x+size/6, y+size/6, x+size*5/6, y+size*5/6, thick, c)
	strokeSquares(l, cell, x+size*5/6, y+size/6, x+size/6, y+size*5/6, thick, c)
}

// drawBang draws an exclamation mark in a size x size box with its top left at (x, y): a job that waits for a
// person is a warning, not a failure, so it does not get the cross.
func drawBang(l *draw.List, cell, x, y, size int, c draw.Color) {
	thick := max(cell, snapDown(size/6, cell))
	cx := snapDown(x+(size-thick)/2, cell)
	l.Fill(draw.Rect{X: cx, Y: snapDown(y+size/10, cell), W: thick, H: max(cell, snapDown(size*11/20, cell))}, c)
	l.Fill(draw.Rect{X: cx, Y: snapDown(y+size*3/4, cell), W: thick, H: thick}, c)
}

// drawDash draws the dash of a skipped record in a size x size box with its top left at (x, y): one cell tall.
func drawDash(l *draw.List, cell, x, y, size int, c draw.Color) {
	l.Fill(draw.Rect{X: snapDown(x+size/6, cell), Y: snapDown(y+size/2, cell), W: max(cell, snapDown(size*2/3, cell)), H: cell}, c)
}

// arrowLabels are the labels the hint bar uses for the D-pad directions. The font has no arrows (assets/PINS.md), so
// the pill draws them as small triangles.
var arrowLabels = map[string]byte{"←": 'l', "→": 'r', "↑": 'u', "↓": 'd'}

// drawTriangle draws a filled triangle pointing in dir ('l', 'r', 'u' or 'd') in a size x size box with its top left
// at (x, y), as a pixel triangle: the base is an even number of cells, and each step towards the tip is one cell
// long and two cells shorter.
func drawTriangle(l *draw.List, cell, x, y, size int, dir byte, c draw.Color) {
	n := max(snapDown(size/cell, 2), 2) // cells along the base
	for i := 0; i < n/2; i++ {
		long := (n - 2*i) * cell // the length of the line across the base direction
		off := i * cell          // how far it is from the base
		switch dir {
		case 'r':
			l.Fill(draw.Rect{X: x + off, Y: y + (n*cell-long)/2, W: cell, H: long}, c)
		case 'l':
			l.Fill(draw.Rect{X: x + (n/2-1-i)*cell, Y: y + (n*cell-long)/2, W: cell, H: long}, c)
		case 'd':
			l.Fill(draw.Rect{X: x + (n*cell-long)/2, Y: y + off, W: long, H: cell}, c)
		case 'u':
			l.Fill(draw.Rect{X: x + (n*cell-long)/2, Y: y + (n/2-1-i)*cell, W: long, H: cell}, c)
		}
	}
}
