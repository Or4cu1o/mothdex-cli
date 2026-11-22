//go:build gui

package sdl

import (
	"math"
	"time"

	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Pure helpers of the backend: nothing here calls SDL, so they are tested without it.

// notchedRects splits a rectangle with stepped corners into the plain rectangles that SDL_RenderFillRect can
// fill: the first row of a corner starts steps cells in, the next one fewer, and so on until the edge is
// straight. The steps are clamped so the corners never meet. The parts do not overlap, so a color with
// alpha is blended once.
func notchedRects(r gdraw.Rect, steps, cell int) []gdraw.Rect {
	if cell > 0 {
		steps = min(steps, min(r.W, r.H)/(2*cell))
	}
	if steps <= 0 || cell <= 0 {
		return []gdraw.Rect{r}
	}
	out := make([]gdraw.Rect, 0, 2*steps+1)
	for i := 0; i < steps; i++ {
		inset := (steps - i) * cell
		w := r.W - 2*inset
		out = append(out,
			gdraw.Rect{X: r.X + inset, Y: r.Y + i*cell, W: w, H: cell},
			gdraw.Rect{X: r.X + inset, Y: r.Bottom() - (i+1)*cell, W: w, H: cell},
		)
	}
	if mid := r.H - 2*steps*cell; mid > 0 {
		out = append(out, gdraw.Rect{X: r.X, Y: r.Y + steps*cell, W: r.W, H: mid})
	}
	return out
}

// spinnerBlocks is how many blocks make the spinner.
const spinnerBlocks = 8

// spinnerFadeFloor is the opacity of the oldest block, 25 %.
const spinnerFadeFloor = 64

// spinnerRing is the cells of the 3 x 3 grid that the blocks take, clockwise from the top left.
var spinnerRing = [spinnerBlocks][2]int{{0, 0}, {1, 0}, {2, 0}, {2, 1}, {2, 2}, {1, 2}, {0, 2}, {0, 1}}

// spinnerBlock is block k of a spinner: its top left corner, its side and its opacity. The block at the step is
// opaque and the ones behind it fade to a quarter.
func spinnerBlock(op gdraw.Op, k int) (x, y, side int, alpha uint8) {
	side = op.Side
	half := (3*side + 2*op.Cell) / 2
	x = op.CX - half + spinnerRing[k][0]*(side+op.Cell)
	y = op.CY - half + spinnerRing[k][1]*(side+op.Cell)
	age := ((op.Step-k)%spinnerBlocks + spinnerBlocks) % spinnerBlocks
	alpha = uint8(255 - age*(255-spinnerFadeFloor)/(spinnerBlocks-1))
	return x, y, side, alpha
}

// waitMillis converts a wait into the milliseconds SDL_WaitEventTimeout takes: negative waits with no
// limit, and a wait that is not zero is at least one millisecond.
func waitMillis(d time.Duration) int32 {
	switch {
	case d < 0:
		return -1
	case d == 0:
		return 0
	}
	ms := (d + time.Millisecond - 1) / time.Millisecond
	return int32(min(ms, math.MaxInt32)) //nolint:gosec // clamped to the int32 range on the line above
}

// keyFromSym maps an SDL_Keycode to the keyboard fallback of docs/ux/gui.md section 2.4. Letters come as
// KeyRune with the lowercase letter.
func keyFromSym(sym int32) (input.KeyCode, rune) {
	switch sym {
	case KeycodeUp:
		return input.KeyUp, 0
	case KeycodeDown:
		return input.KeyDown, 0
	case KeycodeLeft:
		return input.KeyLeft, 0
	case KeycodeRight:
		return input.KeyRight, 0
	case KeycodeReturn, KeycodeKPEnter:
		return input.KeyEnter, 0
	case KeycodeEscape:
		return input.KeyEscape, 0
	case KeycodeBackspace:
		return input.KeyBackspace, 0
	case KeycodePageUp:
		return input.KeyPageUp, 0
	case KeycodePageDown:
		return input.KeyPageDown, 0
	case KeycodeTab:
		return input.KeyTab, 0
	}
	if sym >= 'a' && sym <= 'z' {
		return input.KeyRune, rune(sym)
	}
	return input.KeyNone, 0
}

// buttonFromSDL maps an SDL_GameControllerButton to a position on the pad. Buttons the GUI does not use
// (the guide button, the stick clicks) are not mapped.
func buttonFromSDL(b uint8) (input.Button, bool) {
	switch b {
	case ControllerButtonA:
		return input.South, true
	case ControllerButtonB:
		return input.East, true
	case ControllerButtonX:
		return input.West, true
	case ControllerButtonY:
		return input.North, true
	case ControllerButtonBack:
		return input.Select, true
	case ControllerButtonStart:
		return input.Start, true
	case ControllerButtonLeftShoulder:
		return input.LeftShoulder, true
	case ControllerButtonRightShoulder:
		return input.RightShoulder, true
	case ControllerButtonDPadUp:
		return input.DPadUp, true
	case ControllerButtonDPadDown:
		return input.DPadDown, true
	case ControllerButtonDPadLeft:
		return input.DPadLeft, true
	case ControllerButtonDPadRight:
		return input.DPadRight, true
	}
	return 0, false
}

// axisFromSDL maps an SDL_GameControllerAxis to the left stick; the right stick and the triggers are not used.
func axisFromSDL(a uint8) (input.Axis, bool) {
	switch a {
	case ControllerAxisLeftX:
		return input.LeftX, true
	case ControllerAxisLeftY:
		return input.LeftY, true
	}
	return 0, false
}
