package gui

import (
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// The font has no check mark or cross (assets/PINS.md): they are drawn as short strokes of small squares.

// strokeSquares draws a straight stroke from (x0,y0) to (x1,y1) as squares of the given thickness.
func strokeSquares(l *draw.List, x0, y0, x1, y1, thick int, c draw.Color) {
	steps := max(abs(x1-x0), abs(y1-y0))
	if steps == 0 {
		l.Fill(draw.Rect{X: x0, Y: y0, W: thick, H: thick}, 0, c)
		return
	}
	for i := 0; i <= steps; i++ {
		x := x0 + (x1-x0)*i/steps
		y := y0 + (y1-y0)*i/steps
		l.Fill(draw.Rect{X: x - thick/2, Y: y - thick/2, W: thick, H: thick}, 0, c)
	}
}

// drawCheck draws a check mark in a size x size box with its top left at (x, y).
func drawCheck(l *draw.List, x, y, size int, c draw.Color) {
	thick := max(2, size/8)
	strokeSquares(l, x+size/8, y+size*11/20, x+size*2/5, y+size*17/20, thick, c)
	strokeSquares(l, x+size*2/5, y+size*17/20, x+size*7/8, y+size/6, thick, c)
}

// drawCross draws a cross in a size x size box with its top left at (x, y).
func drawCross(l *draw.List, x, y, size int, c draw.Color) {
	thick := max(2, size/8)
	strokeSquares(l, x+size/6, y+size/6, x+size*5/6, y+size*5/6, thick, c)
	strokeSquares(l, x+size*5/6, y+size/6, x+size/6, y+size*5/6, thick, c)
}

// drawBang draws an exclamation mark in a size x size box with its top left at (x, y): a job that waits for a
// person is a warning, not a failure, so it does not get the cross.
func drawBang(l *draw.List, x, y, size int, c draw.Color) {
	thick := max(2, size/6)
	cx := x + (size-thick)/2
	l.Fill(draw.Rect{X: cx, Y: y + size/10, W: thick, H: size * 11 / 20}, 0, c)
	l.Fill(draw.Rect{X: cx, Y: y + size*3/4, W: thick, H: thick}, 0, c)
}

// arrowLabels are the labels the hint bar uses for the D-pad directions. The font has no arrows (assets/PINS.md), so
// the pill draws them as small triangles.
var arrowLabels = map[string]byte{"←": 'l', "→": 'r', "↑": 'u', "↓": 'd'}

// drawTriangle draws a filled triangle pointing in dir ('l', 'r', 'u' or 'd') in a size x size box with its top left
// at (x, y), as one-pixel lines.
func drawTriangle(l *draw.List, x, y, size int, dir byte, c draw.Color) {
	for i := 0; i < size; i++ {
		span := max(size-i, 1) // the line shrinks to the tip
		half := max(span/2, 1)
		switch dir {
		case 'r': // a vertical line that gets shorter towards the right
			l.Fill(draw.Rect{X: x + i, Y: y + size/2 - half, W: 1, H: 2 * half}, 0, c)
		case 'l':
			l.Fill(draw.Rect{X: x + size - 1 - i, Y: y + size/2 - half, W: 1, H: 2 * half}, 0, c)
		case 'd':
			l.Fill(draw.Rect{X: x + size/2 - half, Y: y + i, W: 2 * half, H: 1}, 0, c)
		case 'u':
			l.Fill(draw.Rect{X: x + size/2 - half, Y: y + size - 1 - i, W: 2 * half, H: 1}, 0, c)
		}
	}
}
