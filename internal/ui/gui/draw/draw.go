// Package draw holds the draw list: what the GUI model produces and the SDL backend executes. It is plain
// data with no behavior, so the model never imports SDL and the backend never imports the model.
// Coordinates are logical pixels; the backend applies the render scale or the logical size.
package draw

import "image"

// Rect is a rectangle in logical pixels.
type Rect struct{ X, Y, W, H int }

// Right is the first column outside the rectangle.
func (r Rect) Right() int { return r.X + r.W }

// Bottom is the first row outside the rectangle.
func (r Rect) Bottom() int { return r.Y + r.H }

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Inset shrinks the rectangle by n on every edge (n < 0 grows it).
func (r Rect) Inset(n int) Rect { return Rect{r.X + n, r.Y + n, r.W - 2*n, r.H - 2*n} }

// Contains reports whether the point is inside the rectangle.
func (r Rect) Contains(x, y int) bool { return x >= r.X && x < r.Right() && y >= r.Y && y < r.Bottom() }

// Within reports whether r lies entirely inside outer.
func (r Rect) Within(outer Rect) bool {
	return r.X >= outer.X && r.Y >= outer.Y && r.Right() <= outer.Right() && r.Bottom() <= outer.Bottom()
}

// Center is the middle of the rectangle.
func (r Rect) Center() (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

// Color is a straight (not premultiplied) RGBA color.
type Color struct{ R, G, B, A uint8 }

// Kind is the kind of an Op.
type Kind int

// Op kinds.
const (
	KindRect    Kind = iota // a filled rectangle, with stepped corners when Steps > 0
	KindText                // one line of text
	KindSpinner             // eight blocks around a square, one step per 100 ms
	KindImage               // a picture (a cover) scaled to fill Rect
)

// Op is one drawing operation. Only the fields of its Kind are used.
type Op struct {
	Kind  Kind
	Color Color

	// KindRect: Steps is how many steps each corner is cut by (0 for a plain rectangle), each Cell pixels wide
	// and tall: a 2-step corner removes 2 cells from the first row and 1 from the second.
	Rect  Rect
	Steps int
	Cell  int

	// KindText: X is the left edge of the first character and Base the baseline row; Size is in pixels.
	// Display draws the run in the display face (DESIGN.md Type) instead of the body face.
	X, Base, Size int
	Text          string
	Display       bool

	// KindSpinner: the center of the square, Cell (the pixel cell P; the blocks are 2 cells and the square 6) and
	// Step, the block that is at full color now (0..7); the others fade behind it to 25 %.
	CX, CY, Step int

	// KindImage: Rect is where the picture goes. ImageID names the picture for the backend's texture cache
	// (the same id is the same pixels); Pixels is the picture, not modified after it was made.
	ImageID uint64
	Pixels  *image.RGBA
}

// Layout says how the logical coordinates of a List map to the display: a render scale for sizes above
// 1200 px tall, or a logical size that SDL letterboxes for displays below 640x480.
type Layout struct {
	W, H  int  // the logical size
	Scale int  // integer render scale; 1 when Fit is set
	Fit   bool // draw the W x H layout scaled to fit the display, with bars
}

// List is one frame: the background first, then each layer in the order it must be drawn.
type List struct {
	W, H int // the logical size the coordinates belong to
	Ops  []Op
}

// Reset empties the list for the next frame and keeps its capacity.
func (l *List) Reset(w, h int) { l.W, l.H, l.Ops = w, h, l.Ops[:0] }

// Fill appends a filled rectangle.
func (l *List) Fill(r Rect, c Color) {
	if r.Empty() || c.A == 0 {
		return
	}
	l.Ops = append(l.Ops, Op{Kind: KindRect, Rect: r, Color: c})
}

// Notch appends a filled rectangle with stepped corners: steps steps of cell pixels (docs/ux DESIGN.md, Shape).
func (l *List) Notch(r Rect, steps, cell int, c Color) {
	if r.Empty() || c.A == 0 {
		return
	}
	l.Ops = append(l.Ops, Op{Kind: KindRect, Rect: r, Steps: steps, Cell: cell, Color: c})
}

// Text appends one line of text.
func (l *List) Text(x, base, size int, c Color, s string) {
	if s == "" || c.A == 0 {
		return
	}
	l.Ops = append(l.Ops, Op{Kind: KindText, X: x, Base: base, Size: size, Color: c, Text: s})
}

// DisplayText appends one line of text in the display face.
func (l *List) DisplayText(x, base, size int, c Color, s string) {
	if s == "" || c.A == 0 {
		return
	}
	l.Ops = append(l.Ops, Op{Kind: KindText, X: x, Base: base, Size: size, Color: c, Text: s, Display: true})
}

// Spinner appends a spinner: eight blocks of two cells around a square of six, the one at step at full color.
func (l *List) Spinner(cx, cy, cell, step int, c Color) {
	l.Ops = append(l.Ops, Op{Kind: KindSpinner, CX: cx, CY: cy, Cell: cell, Step: step, Color: c})
}

// Image appends a picture to be scaled into r.
func (l *List) Image(r Rect, id uint64, pixels *image.RGBA) {
	if r.Empty() || pixels == nil {
		return
	}
	l.Ops = append(l.Ops, Op{Kind: KindImage, Rect: r, ImageID: id, Pixels: pixels})
}

// Texts returns the text of every text op, in drawing order, for tests that assert on content.
func (l *List) Texts() []string {
	var out []string
	for _, op := range l.Ops {
		if op.Kind == KindText {
			out = append(out, op.Text)
		}
	}
	return out
}
