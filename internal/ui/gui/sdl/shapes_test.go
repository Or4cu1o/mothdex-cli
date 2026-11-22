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

func TestNotchedRectsCutEachCornerInStepsWithoutOverlap(t *testing.T) {
	r := gdraw.Rect{X: 10, Y: 20, W: 100, H: 60}
	for _, c := range []struct{ steps, cell int }{{2, 2}, {2, 3}, {2, 4}, {3, 2}, {3, 4}} {
		covered := pixelsOf(t, r, notchedRects(r, c.steps, c.cell))
		for row := 0; row < c.steps; row++ {
			inset := (c.steps - row) * c.cell
			for x := 0; x < inset; x++ { // cut: the first inset pixels of the row, at every corner
				for _, pt := range [][2]int{{r.X + x, r.Y + row*c.cell}, {r.Right() - 1 - x, r.Y + row*c.cell}, {r.X + x, r.Bottom() - 1 - row*c.cell}, {r.Right() - 1 - x, r.Bottom() - 1 - row*c.cell}} {
					if covered[pt] {
						t.Fatalf("steps %d cell %d: pixel %v is in the corner and must be cut", c.steps, c.cell, pt)
					}
				}
			}
			if !covered[[2]int{r.X + inset, r.Y + row*c.cell}] {
				t.Errorf("steps %d cell %d: row %d must start at inset %d", c.steps, c.cell, row, inset)
			}
		}
		for _, pt := range [][2]int{{r.X + r.W/2, r.Y}, {r.X, r.Y + r.H/2}, {r.Right() - 1, r.Y + r.H/2}, {r.X + r.W/2, r.Bottom() - 1}} {
			if !covered[pt] {
				t.Errorf("steps %d cell %d: the edge pixel %v must be filled", c.steps, c.cell, pt)
			}
		}
	}
}

func TestNotchedRectsClampTheStepsAndKeepPlainRectangles(t *testing.T) {
	r := gdraw.Rect{W: 20, H: 8}
	pixelsOf(t, r, notchedRects(r, 3, 4)) // too small for 3 steps of 4: clamped, still no overlap
	if got := notchedRects(gdraw.Rect{W: 5, H: 5}, 0, 0); len(got) != 1 {
		t.Errorf("no steps is one fill: %v", got)
	}
}

func TestSpinnerHasEightBlocksWithAFullHeadAndAFadeToAQuarter(t *testing.T) {
	op := gdraw.Op{CX: 100, CY: 100, Cell: 3, Step: 3}
	seen := map[[2]int]bool{}
	var alphas [spinnerBlocks]uint8
	for k := range alphas {
		x, y, side, alpha := spinnerBlock(op, k)
		if side != 6 || x < 100-9 || x+side > 100+9 || y < 100-9 || y+side > 100+9 {
			t.Fatalf("block %d at %d,%d side %d is outside the 6-cell square", k, x, y, side)
		}
		if x == 100-3 && y == 100-3 {
			t.Fatalf("block %d is in the middle of the ring", k)
		}
		if seen[[2]int{x, y}] {
			t.Fatalf("block %d shares its place", k)
		}
		seen[[2]int{x, y}] = true
		alphas[k] = alpha
	}
	if alphas[3] != 255 {
		t.Errorf("the block at the step is opaque, got %d", alphas[3])
	}
	if alphas[4] != spinnerFadeFloor {
		t.Errorf("the block just ahead of the head is the oldest, at 25 %%: got %d", alphas[4])
	}
	for k := 1; k < spinnerBlocks; k++ {
		if a, b := alphas[(3-k+spinnerBlocks)%spinnerBlocks], alphas[(3-k+1+spinnerBlocks)%spinnerBlocks]; a >= b {
			t.Errorf("the fade must fall behind the head: %d then %d", b, a)
		}
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
