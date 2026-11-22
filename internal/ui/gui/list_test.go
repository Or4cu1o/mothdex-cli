package gui

import "testing"

// A focused row keeps a row of context above and below, except at the ends (section 3).
func TestScrollTopKeepsOneRowOfContext(t *testing.T) {
	const visible, n = 6, 50
	tests := []struct {
		name             string
		top, focus, want int
	}{
		{"the top of the list needs no context above", 0, 0, 0},
		{"moving down inside the window does not scroll", 0, 3, 0},
		{"the last visible row has no context below, so it scrolls", 0, 5, 1},
		{"one row of context below stays", 0, 4, 0},
		{"moving up to the first visible row scrolls to keep context above", 10, 10, 9},
		{"inside the window above does not scroll", 10, 12, 10},
		{"the end of the list stops scrolling", 40, 49, 44},
		{"a jump far down puts the focus at the second to last row", 0, 30, 26},
		{"a jump far up puts the focus at the second row", 30, 2, 1},
		{"focus on the very first row", 5, 0, 0},
	}
	for _, tc := range tests {
		if got := scrollTop(tc.top, tc.focus, visible, n); got != tc.want {
			t.Errorf("%s: scrollTop(%d, %d) = %d, want %d", tc.name, tc.top, tc.focus, got, tc.want)
		}
	}
	for _, focus := range []int{0, 1, 7, 25, 48, 49} { // the focus is always inside the window
		top := scrollTop(0, focus, visible, n)
		if focus < top || focus >= top+visible {
			t.Errorf("focus %d is outside the window starting at %d", focus, top)
		}
	}
}

func TestScrollTopHandlesShortListsAndTinyWindows(t *testing.T) {
	if got := scrollTop(3, 2, 10, 4); got != 0 {
		t.Errorf("a list that fits never scrolls: %d", got)
	}
	if got := scrollTop(0, 0, 0, 10); got != 0 {
		t.Errorf("no room: %d", got)
	}
	if got := scrollTop(0, 4, 2, 10); got != 3 { // a window of two rows has no room for context
		t.Errorf("tiny window: %d", got)
	}
	if clampFocus(-3, 5) != 0 || clampFocus(9, 5) != 4 || clampFocus(2, 0) != 0 {
		t.Error("clampFocus")
	}
}

func TestListMovesDoNotWrapAndPageByAWindow(t *testing.T) {
	l := listState{}
	l = l.moved(-1, 6, 30)
	if l.focus != 0 {
		t.Fatalf("up at the top: %d", l.focus)
	}
	for i := 0; i < 40; i++ {
		l = l.moved(1, 6, 30)
	}
	if l.focus != 29 || l.top != 24 {
		t.Fatalf("down at the bottom: focus %d top %d", l.focus, l.top)
	}
	l = listState{}.paged(1, 6, 30)
	if l.focus != 4 { // a page is the window minus the rows of context
		t.Fatalf("a page down from the top moves 4 rows, got focus %d", l.focus)
	}
	if l = l.paged(-1, 6, 30); l.focus != 0 {
		t.Fatalf("a page up returns: %d", l.focus)
	}
	got, from, to := listState{focus: 99, top: 99}.shown(5, 8)
	if got.focus != 7 || from != 3 || to != 8 {
		t.Fatalf("a stale state is clamped: %+v %d %d", got, from, to)
	}
}
