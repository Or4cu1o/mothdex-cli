package gui

import (
	"slices"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// The moth is 16 x 12 cells in the three moth colors of DESIGN.md and nothing else, and its two frames differ.
func TestTheMothFramesAre16x12CellsInTheMothColors(t *testing.T) {
	allowed := map[draw.Color]bool{colAccent: true, colMothDeep: true, colMothLight: true}
	for frame := 0; frame < 2; frame++ {
		s := moth(frame)
		if s.w != spriteCellsW || s.h != spriteCellsH {
			t.Fatalf("frame %d is %dx%d cells, want %dx%d", frame, s.w, s.h, spriteCellsW, spriteCellsH)
		}
		opaque := 0
		for _, c := range s.cells {
			if c.A == 0 {
				continue
			}
			opaque++
			if !allowed[c] {
				t.Errorf("frame %d has the color %+v, outside the moth palette", frame, c)
			}
		}
		if opaque == 0 {
			t.Errorf("frame %d draws nothing", frame)
		}
	}
	if slices.Equal(moth(0).cells, moth(1).cells) {
		t.Error("the wings-up frame equals the wings-down frame")
	}
}

// The splash alternates the frames every 250 ms; the sprite is drawn on the grid of the pixel cell.
func TestTheSplashMothFlapsEvery250msOnTheGrid(t *testing.T) {
	t0 := time.Unix(1_700_000_000, 0)
	if wingFrameAt(t0) == wingFrameAt(t0.Add(wingFrame)) || wingFrameAt(t0) != wingFrameAt(t0.Add(2*wingFrame)) {
		t.Error("the wing frame must change every 250 ms")
	}
	var l draw.List
	l.Reset(100, 100)
	drawSprite(&l, 3, 30, 30, moth(0))
	for _, op := range l.Ops {
		if op.Rect.X%3 != 0 || op.Rect.Y%3 != 0 || op.Rect.W%3 != 0 || op.Rect.H != 3 {
			t.Fatalf("sprite run %+v is off the 3-pixel grid", op.Rect)
		}
	}
}

// The sky is the same list for the same size, one speck per about 5,200 px², on the grid and inside the screen.
func TestTheSkyIsSeededSparseAndOnTheGrid(t *testing.T) {
	a, b := skySpecks(1280, 720, 3), skySpecks(1280, 720, 3)
	if len(a) != 1280*720/skyArea {
		t.Fatalf("%d specks, want %d", len(a), 1280*720/skyArea)
	}
	for i, s := range a {
		if s != b[i] {
			t.Fatal("the same size must give the same sky")
		}
		if s.X%3 != 0 || s.Y%3 != 0 || s.W != 3 || !s.Within(draw.Rect{W: 1280, H: 720}) {
			t.Fatalf("speck %+v is off the grid or the screen", s)
		}
	}
}
