package gui

import "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"

// rgb makes an opaque color from 0xRRGGBB.
func rgb(v uint32) draw.Color {
	return draw.Color{R: uint8((v >> 16) & 0xff), G: uint8((v >> 8) & 0xff), B: uint8(v & 0xff), A: 0xff}
}

// The color tokens of docs/ux/gui.md section 1.5 (functional baseline, dark).
var (
	colBG        = rgb(0x0F1317)                                  // color.bg
	colSurface   = rgb(0x1A2027)                                  // color.surface
	colFocusBG   = rgb(0x123447)                                  // color.focus.bg
	colFocusRing = rgb(0x5CC8FF)                                  // color.focus.ring
	colText      = rgb(0xF1F4F6)                                  // color.text
	colDim       = rgb(0xA3AFBA)                                  // color.text.dim
	colAccent    = rgb(0x5CC8FF)                                  // color.accent
	colOK        = rgb(0x5DD68C)                                  // color.ok
	colWarn      = rgb(0xF4B740)                                  // color.warn
	colError     = rgb(0xFF7A7A)                                  // color.error
	colScrim     = draw.Color{R: 0x0F, G: 0x13, B: 0x17, A: 0xCC} // color.scrim: bg at 80 %
)
