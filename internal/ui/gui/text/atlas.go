package text

import (
	"image"
	"image/draw"

	"golang.org/x/image/math/fixed"
)

// zeroDot is the pen position glyphs are rasterized at: the origin, so the entry offsets are relative to it.
var zeroDot fixed.Point26_6

// Atlas packs rasterized glyphs into one 8-bit coverage bitmap, on demand, with the simplest packer that
// works: rows of equal-height shelves. The backend uploads the dirty rectangle as a white texture whose
// alpha is the coverage, and draws each glyph from its entry. When the bitmap is full the atlas starts
// again and Generation changes, so the backend knows every cached entry is gone.
type Atlas struct {
	width, height int
	pix           []byte
	entries       map[glyphKey]Entry
	shelfY        int // top of the current shelf
	shelfH        int // height of the current shelf
	cursorX       int
	dirty         image.Rectangle
	generation    int
}

type glyphKey struct {
	px   int
	r    rune
	face *Face
}

// Entry says where a glyph is in the atlas and where to draw it: its origin is the pen position on the
// baseline, so the glyph goes at (pen.X+OffX, baseline+OffY). A glyph with no ink (a space) has W = H = 0.
type Entry struct {
	X, Y, W, H int
	OffX, OffY int
}

// Padding between glyphs in the atlas, so linear filtering never reads a neighbour.
const padding = 1

// NewAtlas returns an empty atlas of the given size in pixels.
func NewAtlas(width, height int) *Atlas {
	return &Atlas{width: width, height: height, pix: make([]byte, width*height), entries: map[glyphKey]Entry{}}
}

// Size is the width and height of the bitmap.
func (a *Atlas) Size() (int, int) { return a.width, a.height }

// Generation changes each time the atlas starts again after being full.
func (a *Atlas) Generation() int { return a.generation }

// Glyph returns the entry for a rune of a face, rasterizing and packing it the first time. It reports false
// when the glyph is larger than the whole atlas.
func (a *Atlas) Glyph(f *Face, r rune) (Entry, bool) {
	key := glyphKey{px: f.px, r: r, face: f}
	if e, ok := a.entries[key]; ok {
		return e, true
	}
	e, ok := a.render(f, r)
	if !ok {
		return Entry{}, false
	}
	a.entries[key] = e
	return e, true
}

func (a *Atlas) render(f *Face, r rune) (Entry, bool) {
	rect, mask, maskp, _, ok := f.face.Glyph(zeroDot, r)
	if !ok {
		return Entry{}, false
	}
	w, h := rect.Dx(), rect.Dy()
	e := Entry{W: w, H: h, OffX: rect.Min.X, OffY: rect.Min.Y}
	if w == 0 || h == 0 {
		return Entry{OffX: rect.Min.X, OffY: rect.Min.Y}, true
	}
	if w+2*padding > a.width || h+2*padding > a.height {
		return Entry{}, false
	}
	x, y, placed := a.place(w, h)
	if !placed {
		a.restart()
		if x, y, placed = a.place(w, h); !placed {
			return Entry{}, false
		}
	}
	e.X, e.Y = x, y
	a.blit(e, mask, maskp)
	return e, true
}

// place finds room for a w x h glyph with padding and returns the top left of its pixels.
func (a *Atlas) place(w, h int) (int, int, bool) {
	if a.cursorX+w+2*padding > a.width { // next shelf
		a.shelfY += a.shelfH
		a.shelfH, a.cursorX = 0, 0
	}
	if h+2*padding > a.shelfH {
		a.shelfH = h + 2*padding
	}
	if a.shelfY+a.shelfH > a.height {
		return 0, 0, false
	}
	x, y := a.cursorX+padding, a.shelfY+padding
	a.cursorX += w + 2*padding
	return x, y, true
}

// restart empties the atlas.
func (a *Atlas) restart() {
	clear(a.pix)
	clear(a.entries)
	a.shelfY, a.shelfH, a.cursorX = 0, 0, 0
	a.dirty = image.Rectangle{}
	a.generation++
}

func (a *Atlas) blit(e Entry, mask image.Image, maskp image.Point) {
	dst := &image.Alpha{Pix: a.pix, Stride: a.width, Rect: image.Rect(0, 0, a.width, a.height)}
	r := image.Rect(e.X, e.Y, e.X+e.W, e.Y+e.H)
	draw.Draw(dst, r, mask, maskp, draw.Src)
	a.dirty = a.dirty.Union(r)
}

// TakeDirty returns the rectangle that changed since the last call and forgets it. The generation is
// unchanged by this call: after Generation changes, upload the whole atlas.
func (a *Atlas) TakeDirty() (image.Rectangle, bool) {
	r := a.dirty
	a.dirty = image.Rectangle{}
	return r, !r.Empty()
}

// MarkAllDirty makes the next TakeDirty return the whole bitmap, for a texture that was just created.
func (a *Atlas) MarkAllDirty() { a.dirty = image.Rect(0, 0, a.width, a.height) }

// AppendARGB appends the pixels of rect as 32-bit white pixels whose alpha is the coverage, row by row with
// no padding, in the byte order SDL's ARGB8888 has on little-endian machines (B, G, R, A). The rectangle is
// clipped to the atlas. It returns the slice and the pitch of a row in bytes.
func (a *Atlas) AppendARGB(dst []byte, rect image.Rectangle) ([]byte, int) {
	rect = rect.Intersect(image.Rect(0, 0, a.width, a.height))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for _, cov := range a.pix[y*a.width+rect.Min.X : y*a.width+rect.Max.X] {
			dst = append(dst, 0xff, 0xff, 0xff, cov)
		}
	}
	return dst, rect.Dx() * 4
}

// Coverage is the atlas bitmap, for tests.
func (a *Atlas) Coverage() []byte { return a.pix }
