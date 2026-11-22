package gui

import "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"

// The night sky of DESIGN.md (Home and splash only): still specks of P x P in color.frame, about one per skyArea
// square pixels, at fixed positions from a seeded list. They are drawn on the background only, never over a surface or
// next to text, and never move.
const (
	skyArea = 5200
	skySeed = 0x6d6f7468 // "moth"
)

// skySpecks is the seeded list of speck positions for a logical size, snapped to the pixel cell. The same size always
// gives the same list.
func skySpecks(w, h, cell int) []draw.Rect {
	n := w * h / skyArea
	state := uint64(skySeed)
	next := func(limit int) int { // a small linear congruential generator: deterministic, no clock
		state = state*6364136223846793005 + 1442695040888963407
		return int((state >> 33) % uint64(max(limit, 1))) //nolint:gosec // a bounded value
	}
	out := make([]draw.Rect, 0, n)
	for i := 0; i < n; i++ {
		x, y := snapDown(next(w-cell), cell), snapDown(next(h-cell), cell) // the speck stays whole inside the screen
		out = append(out, draw.Rect{X: x, Y: y, W: cell, H: cell})
	}
	return out
}

// drawSky draws the specks that are inside the safe rectangle (like everything else, so a TV that crops its edges loses
// nothing) and do not touch any of the rectangles in avoid (surfaces and text), each grown by two cells.
func (p painter) drawSky(avoid []rect) {
	for _, s := range skySpecks(p.m.W, p.m.H, p.m.Pixel) {
		hit := !s.Within(p.m.Safe)
		for _, a := range avoid {
			if intersects(s, a.Inset(-2*p.m.Pixel)) {
				hit = true
				break
			}
		}
		if !hit {
			p.l.Fill(s, colFrame)
		}
	}
}
