package gui

import "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"

// rgb makes an opaque color from 0xRRGGBB.
func rgb(v uint32) draw.Color {
	return draw.Color{R: uint8((v >> 16) & 0xff), G: uint8((v >> 8) & 0xff), B: uint8(v & 0xff), A: 0xff}
}

// The color tokens of DESIGN.md, "Night moth arcade" (D30). They replace the values of docs/ux/gui.md section 1.5.
var (
	colBG        = rgb(0x0D0D10)                                  // color.bg: the night sky
	colSurface   = rgb(0x1C1C1E)                                  // color.surface: bars, rows, tiles, keys
	colRaised    = rgb(0x28282A)                                  // color.surface.raised: windows over the scrim
	colFrame     = rgb(0x3A3D52)                                  // color.frame: the P frame of windows; sky specks; decorative only
	colFocusBG   = rgb(0x2A2E44)                                  // color.focus.bg
	colFocusRing = rgb(0xA8ACC2)                                  // color.focus.ring: focus frame and menu cursor
	colText      = rgb(0xE7E6E8)                                  // color.text
	colDim       = rgb(0xA3A3A8)                                  // color.text.dim
	colAccent    = rgb(0x8F95C4)                                  // color.moth (accent): wordmark, progress fill, badges, selected marks
	colMothDeep  = rgb(0x595D7A)                                  // color.moth.deep: sprite wing shading only, never text
	colMothLight = rgb(0xC9CCE0)                                  // color.moth.light: sprite highlights only
	colOK        = rgb(0xA1E698)                                  // color.ok
	colWarn      = rgb(0xE6CF8F)                                  // color.warn
	colError     = rgb(0xE698A3)                                  // color.error
	colScrim     = draw.Color{R: 0x0D, G: 0x0D, B: 0x10, A: 0xCC} // color.scrim: bg at 80 %
)
