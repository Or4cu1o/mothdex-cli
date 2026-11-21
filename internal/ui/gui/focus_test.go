package gui

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// homeLike is a 3 x 2 grid of tiles that wrap, a row of chips and a list of rows below, like Home.
func homeLike() []Element {
	var els []Element
	for row := 0; row < 2; row++ {
		for col := 0; col < 3; col++ {
			els = append(els, Element{
				ID:      string(rune('a' + row*3 + col)),
				Rect:    draw.Rect{X: 10 + col*200, Y: 10 + row*110, W: 180, H: 100},
				Region:  0,
				WrapRow: true,
			})
		}
	}
	els = append(els,
		Element{ID: "chip1", Rect: draw.Rect{X: 10, Y: 240, W: 80, H: 40}, Region: 1, WrapRow: true},
		Element{ID: "chip2", Rect: draw.Rect{X: 100, Y: 240, W: 120, H: 40}, Region: 1, WrapRow: true},
		Element{ID: "chip3", Rect: draw.Rect{X: 230, Y: 240, W: 200, H: 40}, Region: 1, WrapRow: true},
	)
	return els
}

func TestNavigateMovesByGeometry(t *testing.T) {
	els := homeLike()
	tests := []struct {
		from string
		d    Dir
		want string
	}{
		{"a", DirRight, "b"}, {"b", DirRight, "c"}, {"c", DirLeft, "b"},
		{"a", DirDown, "d"}, {"e", DirUp, "b"}, {"f", DirDown, "chip3"},
		{"d", DirDown, "chip1"}, {"e", DirDown, "chip3"}, // the chip whose center is closest to the tile's
		{"chip1", DirUp, "d"}, {"chip3", DirUp, "e"},
		{"a", DirUp, "a"},           // top edge: nothing happens
		{"chip2", DirDown, "chip2"}, // bottom edge
	}
	for _, tc := range tests {
		if got := Navigate(els, tc.from, tc.d); got != tc.want {
			t.Errorf("%s %d -> %s, want %s", tc.from, tc.d, got, tc.want)
		}
	}
}

// Rows of tiles wrap horizontally, and a right move on the last tile never jumps to a chip on another row.
func TestRowsWrapAndStayOnTheirRow(t *testing.T) {
	els := homeLike()
	for from, want := range map[string]string{"c": "a", "a": "c", "f": "d", "d": "f", "chip3": "chip1", "chip1": "chip3"} {
		d := DirRight
		if from == "a" || from == "d" || from == "chip1" {
			d = DirLeft
		}
		if got := Navigate(els, from, d); got != want {
			t.Errorf("%s %d -> %s, want %s", from, d, got, want)
		}
	}
}

func TestListsDoNotWrap(t *testing.T) {
	rows := []Element{
		{ID: "r1", Rect: draw.Rect{Y: 0, W: 300, H: 40}},
		{ID: "r2", Rect: draw.Rect{Y: 40, W: 300, H: 40}},
		{ID: "r3", Rect: draw.Rect{Y: 80, W: 300, H: 40}},
	}
	if got := Navigate(rows, "r3", DirDown); got != "r3" {
		t.Errorf("down at the end: %s", got)
	}
	if got := Navigate(rows, "r1", DirUp); got != "r1" {
		t.Errorf("up at the top: %s", got)
	}
	if got := Navigate(rows, "r2", DirLeft); got != "r2" {
		t.Errorf("left on a list: %s", got)
	}
	if got := Navigate(rows, "r2", DirDown); got != "r3" {
		t.Errorf("down: %s", got)
	}
}

func TestButtonRowDoesNotWrapAndSkipsNothing(t *testing.T) {
	buttons := []Element{
		{ID: "stay", Rect: draw.Rect{X: 0, Y: 0, W: 100, H: 40}},
		{ID: "quit", Rect: draw.Rect{X: 110, Y: 0, W: 100, H: 40}},
		{ID: "cancel", Rect: draw.Rect{X: 220, Y: 0, W: 100, H: 40}},
	}
	for _, step := range []struct {
		from string
		d    Dir
		want string
	}{{"stay", DirRight, "quit"}, {"quit", DirRight, "cancel"}, {"cancel", DirRight, "cancel"}, {"stay", DirLeft, "stay"}, {"stay", DirDown, "stay"}} {
		if got := Navigate(buttons, step.from, step.d); got != step.want {
			t.Errorf("%s %d -> %s, want %s", step.from, step.d, got, step.want)
		}
	}
}

// Entering the next region lands on the element closest to the current horizontal position.
func TestLeavingARegionEntersTheClosestElement(t *testing.T) {
	els := []Element{
		{ID: "top", Rect: draw.Rect{X: 300, Y: 0, W: 100, H: 40}, Region: 0},
		{ID: "k1", Rect: draw.Rect{X: 0, Y: 60, W: 50, H: 50}, Region: 1},
		{ID: "k2", Rect: draw.Rect{X: 60, Y: 60, W: 50, H: 50}, Region: 1},
		{ID: "k3", Rect: draw.Rect{X: 300, Y: 60, W: 50, H: 50}, Region: 1},
		{ID: "k4", Rect: draw.Rect{X: 380, Y: 60, W: 50, H: 50}, Region: 1},
		{ID: "far", Rect: draw.Rect{X: 300, Y: 130, W: 50, H: 50}, Region: 2},
	}
	if got := Navigate(els, "top", DirDown); got != "k4" && got != "k3" {
		t.Errorf("entered at %s, want one of the keys under it", got)
	}
	if got := Navigate(els, "top", DirDown); got != "k3" { // 350 vs the center of top at 350
		t.Errorf("closest horizontally is k3, got %s", got)
	}
	if got := Navigate(els, "k3", DirDown); got != "far" {
		t.Errorf("down to the next row: %s", got)
	}
}

func TestNavigateWithAnUnknownCurrentFocusesTheFirst(t *testing.T) {
	els := homeLike()
	if got := Navigate(els, "gone", DirDown); got != "a" {
		t.Errorf("got %s", got)
	}
	if got := Navigate(nil, "a", DirDown); got != "" {
		t.Errorf("empty list: %q", got)
	}
	if First(nil) != "" || First(els) != "a" {
		t.Error("First")
	}
}

// Focus is restored by id, and when the element is gone it goes to the nearest remaining one.
func TestRestoreByIDOrNearest(t *testing.T) {
	els := homeLike()
	if got := Restore(els, "e", draw.Rect{}); got != "e" {
		t.Errorf("present id: %s", got)
	}
	gone := draw.Rect{X: 410, Y: 120, W: 180, H: 100}   // where "f" was
	without := els[:5]                                  // "f" removed
	if got := Restore(without, "f", gone); got != "c" { // the tile above it is the nearest left
		t.Errorf("nearest to a removed element: %s", got)
	}
	if got := Restore(nil, "f", gone); got != "" {
		t.Errorf("no elements: %q", got)
	}
}
