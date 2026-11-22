//go:build gui

package sdl

import (
	"testing"
	"time"

	gdraw "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// pixelsOf marks the pixels the rectangles cover, failing if two of them overlap.
func pixelsOf(t *testing.T, r gdraw.Rect, parts []gdraw.Rect) map[[2]int]bool {
	t.Helper()
	covered := map[[2]int]bool{}
	for _, p := range parts {
		if p.Empty() || !p.Within(r) {
			t.Fatalf("part %+v is empty or outside %+v", p, r)
		}
		for y := p.Y; y < p.Bottom(); y++ {
			for x := p.X; x < p.Right(); x++ {
				if covered[[2]int{x, y}] {
					t.Fatalf("pixel %d,%d is covered twice", x, y)
				}
				covered[[2]int{x, y}] = true
			}
		}
	}
	return covered
}

func TestRoundedRectsCoverTheShapeWithoutOverlap(t *testing.T) {
	for _, radius := range []int{0, 1, 3, 6, 8, 12, 18} {
		r := gdraw.Rect{X: 10, Y: 20, W: 100, H: 60}
		parts := roundedRects(r, radius)
		covered := pixelsOf(t, r, parts)
		area := r.W * r.H
		if radius >= 3 && len(covered) >= area { // a radius of 1 or 2 is too small to cut a whole pixel
			t.Errorf("radius %d: the corners must be cut, but all %d pixels are covered", radius, area)
		}
		if radius >= 3 && covered[[2]int{r.X, r.Y}] {
			t.Errorf("radius %d: the top left pixel must be cut", radius)
		}
		for _, c := range [][2]int{{r.X + r.W/2, r.Y}, {r.X, r.Y + r.H/2}, {r.Right() - 1, r.Y + r.H/2}, {r.X + r.W/2, r.Bottom() - 1}} {
			if !covered[c] {
				t.Errorf("radius %d: the edge pixel %v must be filled", radius, c)
			}
		}
		// The four corners are alike.
		for y := 0; y < radius; y++ {
			for x := 0; x < radius; x++ {
				tl := covered[[2]int{r.X + x, r.Y + y}]
				if tl != covered[[2]int{r.Right() - 1 - x, r.Y + y}] || tl != covered[[2]int{r.X + x, r.Bottom() - 1 - y}] || tl != covered[[2]int{r.Right() - 1 - x, r.Bottom() - 1 - y}] {
					t.Fatalf("radius %d: the corners are not symmetric at %d,%d", radius, x, y)
				}
			}
		}
		if len(parts) > 2*radius+1 {
			t.Errorf("radius %d needs %d fills, at most %d expected", radius, len(parts), 2*radius+1)
		}
	}
}

func TestRoundedRectsClampTheRadiusToHalfTheShorterSide(t *testing.T) {
	r := gdraw.Rect{W: 20, H: 8}
	parts := roundedRects(r, 50)
	pixelsOf(t, r, parts)
	if got := roundedRects(gdraw.Rect{W: 5, H: 5}, 0); len(got) != 1 {
		t.Errorf("no radius is one fill: %v", got)
	}
}

func TestSpinnerHasADotsRingWithABrightHeadAndAFadingTail(t *testing.T) {
	op := gdraw.Op{CX: 100, CY: 100, R: 12, Thick: 3, Turn: 0.25}
	var alphas [spinnerDots]uint8
	for k := range alphas {
		x, y, side, alpha := spinnerDot(op, k)
		if side < 2 || x < 100-12-side || x > 100+12 || y < 100-12-side || y > 100+12 {
			t.Fatalf("dot %d at %d,%d side %d is off the ring", k, x, y, side)
		}
		alphas[k] = alpha
	}
	head := int(op.Turn * spinnerDots) // dot 3 is the head at a quarter turn
	if alphas[head] != 255 {
		t.Errorf("the head dot is opaque, got %d", alphas[head])
	}
	for k, a := range alphas {
		if a < 40 || a > alphas[head] {
			t.Errorf("dot %d has alpha %d, outside the fade", k, a)
		}
	}
	if alphas[(head+1)%spinnerDots] >= alphas[head] {
		t.Error("the dot just ahead of the head must be the faintest, behind it the tail fades")
	}
}

func TestWaitMillisRoundsUpAndNegativeIsForever(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want int32
	}{
		{-1, -1}, {0, 0}, {time.Nanosecond, 1}, {time.Millisecond, 1}, {1500 * time.Microsecond, 2},
		{400 * time.Millisecond, 400}, {time.Hour * 24 * 365 * 100, 1<<31 - 1},
	}
	for _, tc := range tests {
		if got := waitMillis(tc.in); got != tc.want {
			t.Errorf("waitMillis(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestKeyboardKeysMapToTheFallbackOfTheSpec(t *testing.T) {
	tests := []struct {
		sym  int32
		code input.KeyCode
		r    rune
	}{
		{KeycodeUp, input.KeyUp, 0}, {KeycodeDown, input.KeyDown, 0}, {KeycodeLeft, input.KeyLeft, 0}, {KeycodeRight, input.KeyRight, 0},
		{KeycodeReturn, input.KeyEnter, 0}, {KeycodeKPEnter, input.KeyEnter, 0}, {KeycodeEscape, input.KeyEscape, 0},
		{KeycodeBackspace, input.KeyBackspace, 0}, {KeycodePageUp, input.KeyPageUp, 0}, {KeycodePageDown, input.KeyPageDown, 0},
		{KeycodeTab, input.KeyTab, 0},
		{'x', input.KeyRune, 'x'}, {'y', input.KeyRune, 'y'}, {'q', input.KeyRune, 'q'}, {'a', input.KeyRune, 'a'},
		{'A', input.KeyNone, 0}, {' ', input.KeyNone, 0}, {0x4000003A, input.KeyNone, 0}, // F1
	}
	for _, tc := range tests {
		if code, r := keyFromSym(tc.sym); code != tc.code || r != tc.r {
			t.Errorf("sym %#x -> %d %q, want %d %q", tc.sym, code, r, tc.code, tc.r)
		}
	}
}

func TestControllerButtonsMapByPosition(t *testing.T) {
	want := map[uint8]input.Button{
		ControllerButtonA: input.South, ControllerButtonB: input.East, ControllerButtonX: input.West, ControllerButtonY: input.North,
		ControllerButtonBack: input.Select, ControllerButtonStart: input.Start,
		ControllerButtonLeftShoulder: input.LeftShoulder, ControllerButtonRightShoulder: input.RightShoulder,
		ControllerButtonDPadUp: input.DPadUp, ControllerButtonDPadDown: input.DPadDown,
		ControllerButtonDPadLeft: input.DPadLeft, ControllerButtonDPadRight: input.DPadRight,
	}
	for b, btn := range want {
		if got, ok := buttonFromSDL(b); !ok || got != btn {
			t.Errorf("SDL button %d -> %d %v, want %d", b, got, ok, btn)
		}
	}
	for _, unused := range []uint8{5, 7, 8, 15, 20, 255} { // guide, stick clicks, misc, touchpad, beyond
		if _, ok := buttonFromSDL(unused); ok {
			t.Errorf("SDL button %d must not be mapped", unused)
		}
	}
	if a, ok := axisFromSDL(ControllerAxisLeftY); !ok || a != input.LeftY {
		t.Error("left stick Y")
	}
	if _, ok := axisFromSDL(2); ok { // the right stick
		t.Error("the right stick must be ignored")
	}
}
