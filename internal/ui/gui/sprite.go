package gui

import (
	"bytes"
	"image/png"
	"sync"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// The moth sprite of DESIGN.md: 16 x 12 cells, one cell is the pixel cell P. Aquarela drew the two wing frames (T-83a).
const (
	spriteCellsW = 16
	spriteCellsH = 12
	wingFrame    = 250 * time.Millisecond // the splash moth: two wing frames, 250 ms each
)

// sprite is a picture in cells: color per cell, with alpha 0 for a transparent one.
type sprite struct {
	w, h  int
	cells []draw.Color
}

var (
	mothOnce   sync.Once
	mothFrames [2]sprite // wings down, wings up
)

// loadSprite reads an embedded PNG into cells. A file that does not decode gives an empty sprite, which draws nothing;
// a test checks that the embedded ones decode.
func loadSprite(data []byte) sprite {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return sprite{}
	}
	b := img.Bounds()
	s := sprite{w: b.Dx(), h: b.Dy(), cells: make([]draw.Color, b.Dx()*b.Dy())}
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			s.cells[y*s.w+x] = draw.Color{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8)} //nolint:gosec // 16-bit channels shifted to a byte
		}
	}
	return s
}

// moth returns the wing frame: 0 is wings down, 1 is wings up.
func moth(frame int) sprite {
	mothOnce.Do(func() {
		mothFrames = [2]sprite{loadSprite(assets.MothWingsDown), loadSprite(assets.MothWingsUp)}
	})
	return mothFrames[frame&1]
}

// drawSprite draws the sprite with its top left at (x, y), one rectangle per run of equal cells in a row.
func drawSprite(l *draw.List, cell, x, y int, s sprite) {
	for row := 0; row < s.h; row++ {
		for col := 0; col < s.w; {
			c := s.cells[row*s.w+col]
			end := col + 1
			for end < s.w && s.cells[row*s.w+end] == c {
				end++
			}
			l.Fill(draw.Rect{X: x + col*cell, Y: y + row*cell, W: (end - col) * cell, H: cell}, c) // a Fill with alpha 0 draws nothing
			col = end
		}
	}
}

// mothSize is the width and height of the sprite in pixels.
func (p painter) mothSize() (w, h int) { return spriteCellsW * p.m.Pixel, spriteCellsH * p.m.Pixel }

// drawMoth draws the moth centered vertically in a band that starts at top and is bandH tall. frame is 0 or 1.
func (p painter) drawMoth(x, top, bandH, frame int) {
	_, h := p.mothSize()
	drawSprite(p.l, p.m.Pixel, x, top+(bandH-h)/2, moth(frame))
}

// wingFrameAt is the splash wing frame at time now.
func wingFrameAt(now time.Time) int { return int(now.UnixNano()/int64(wingFrame)) & 1 }
