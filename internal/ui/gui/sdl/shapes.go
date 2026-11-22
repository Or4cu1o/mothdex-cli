//go:build gui

package sdl

import (
	"math"
	"time"

	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Pure helpers of the backend: nothing here calls SDL, so they are tested without it.

// cornerInset is how far in from the side the first pixel of a rounded corner's row starts: row 0 is the top
// row of the corner and row radius-1 the last, where the edge is straight again.
func cornerInset(radius, row int) int {
	dy := float64(radius-row) - 0.5
	dx := math.Sqrt(float64(radius*radius) - dy*dy)
	return max(radius-int(math.Round(dx)), 0)
}

// roundedRects splits a rounded rectangle into the plain rectangles that SDL_RenderFillRect can fill:
// bands of rows with the same inset at the top and bottom, and one block in the middle. The radius is
// clamped so it never exceeds half the shorter side.
func roundedRects(r gdraw.Rect, radius int) []gdraw.Rect {
	radius = min(radius, min(r.W, r.H)/2)
	if radius <= 0 {
		return []gdraw.Rect{r}
	}
	out := make([]gdraw.Rect, 0, 2*radius+1)
	for row := 0; row < radius; {
		inset := cornerInset(radius, row)
		end := row + 1
		for end < radius && cornerInset(radius, end) == inset {
			end++
		}
		w := r.W - 2*inset
		out = append(out,
			gdraw.Rect{X: r.X + inset, Y: r.Y + row, W: w, H: end - row},
			gdraw.Rect{X: r.X + inset, Y: r.Bottom() - end, W: w, H: end - row},
		)
		row = end
	}
	if mid := r.H - 2*radius; mid > 0 {
		out = append(out, gdraw.Rect{X: r.X, Y: r.Y + radius, W: r.W, H: mid})
	}
	return out
}

// spinnerDots is how many dots make the ring of a spinner.
const spinnerDots = 12

// spinnerDot is dot k of a spinner: its top left corner, the side of its square and its opacity. The
// brightest dot is the one the turn has reached and the others fade behind it.
func spinnerDot(op gdraw.Op, k int) (x, y, side int, alpha uint8) {
	angle := 2 * math.Pi * float64(k) / spinnerDots
	rad := float64(op.R) - float64(op.Thick)/2
	side = max(op.Thick, 2)
	x = op.CX + int(math.Round(rad*math.Cos(angle))) - side/2
	y = op.CY + int(math.Round(rad*math.Sin(angle))) - side/2
	behind := math.Mod(op.Turn*spinnerDots-float64(k)+spinnerDots, spinnerDots) / spinnerDots // 0 at the head
	alpha = uint8(255 * (1 - 0.8*behind))
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
