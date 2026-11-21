package gui

import (
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

func TestEveryLayoutIsAFullGridOfKeys(t *testing.T) {
	for _, l := range []kbLayout{layoutABC, layoutAccents, layoutCyrillic} {
		seen := map[string]bool{}
		for r := range l.rows {
			for c, k := range l.rows[r] {
				if k == "" {
					t.Errorf("%s: key %d,%d is empty", l.name, r, c)
				}
				seen[k] = true
			}
		}
		if len(seen) < 38 {
			t.Errorf("%s repeats keys: %d different out of 40", l.name, len(seen))
		}
	}
	if layoutABC.rows[1][0] != "q" || layoutABC.rows[0][9] != "0" || layoutABC.rows[3][9] != "&" || layoutABC.rows[2][9] != "'" {
		t.Error("the abc grid is not the one of section 5.4")
	}
	if layoutAccents.rows[0][0] != "á" || layoutAccents.rows[3][9] != "_" || layoutCyrillic.rows[0][0] != "й" || layoutCyrillic.rows[3][0] != "б" {
		t.Error("the accent and Cyrillic grids are not the ones of section 5.4")
	}
}

// The Cyrillic layout is in the cycle only for Russian or when a Cyrillic letter is already in the field.
func TestLayoutCycle(t *testing.T) {
	names := func(c []kbLayout) string {
		var out []string
		for _, l := range c {
			out = append(out, l.name)
		}
		return strings.Join(out, ",")
	}
	tests := []struct {
		locale string
		text   string
		want   string
	}{
		{"en_us", "zelda", "abc,àé"},
		{"pt_br", "", "abc,àé"},
		{"ru_ru", "", "abc,àé,абв"},
		{"en_us", "привет", "abc,àé,абв"},
		{"en_us", "zeldaй", "abc,àé,абв"},
	}
	for _, tc := range tests {
		if got := names(layoutCycle(tc.locale, []rune(tc.text))); got != tc.want {
			t.Errorf("%s %q: %s, want %s", tc.locale, tc.text, got, tc.want)
		}
	}
}

func TestGridKeyIDsRoundTripAndFunctionKeysAreNotGridKeys(t *testing.T) {
	for r := 0; r < kbRows; r++ {
		for c := 0; c < kbCols; c++ {
			if gr, gc, ok := gridKeyAt(gridKeyID(r, c)); !ok || gr != r || gc != c {
				t.Fatalf("%d,%d: %d,%d %v", r, c, gr, gc, ok)
			}
		}
	}
	for _, id := range []string{keyLayout, keySpace, keyDelete, keySearch, "", "k:9:9", "k:1:x", "chip:region"} {
		if _, _, ok := gridKeyAt(id); ok {
			t.Errorf("%q is not a grid key", id)
		}
	}
	if keyText(layoutABC, gridKeyID(1, 0)) != "q" || keyText(layoutABC, keySpace) != "" {
		t.Error("keyText")
	}
}

// The keyboard fits the area it is given and its keys never overlap; the function row spans the grid width.
func TestKeyboardGeometryFitsTheAreaWithoutOverlap(t *testing.T) {
	for _, tc := range []struct{ w, h, key, gap int }{{600, 280, 48, 8}, {1100, 400, 60, 10}, {1700, 600, 88, 16}, {300, 120, 48, 8}} {
		area := draw.Rect{X: 20, Y: 30, W: tc.w, H: tc.h}
		g := newKBGeometry(area, tc.key, tc.gap)
		if len(g.keys) != kbRows*kbCols+len(functionRow) {
			t.Fatalf("%d keys", len(g.keys))
		}
		for i, a := range g.keys {
			if !a.Rect.Within(area) {
				t.Errorf("%dx%d: key %s %+v is outside %+v", tc.w, tc.h, a.ID, a.Rect, area)
			}
			for _, b := range g.keys[i+1:] {
				if a.Rect.X < b.Rect.Right() && b.Rect.X < a.Rect.Right() && a.Rect.Y < b.Rect.Bottom() && b.Rect.Y < a.Rect.Bottom() {
					t.Errorf("keys %s and %s overlap", a.ID, b.ID)
				}
			}
		}
		last := g.cells[keySearch]
		if last.Right() != g.rect.Right() {
			t.Errorf("the function row ends at %d, the grid at %d", last.Right(), g.rect.Right())
		}
	}
	g := newKBGeometry(draw.Rect{W: 2000, H: 2000}, 60, 10)
	if g.cells[gridKeyID(0, 0)].W != 60 || g.cells[gridKeyID(0, 0)].H != 60 {
		t.Error("keys are never larger than the size of the tier")
	}
	if g.cells[keySpace].W != 4*60+3*10 || g.cells[keyLayout].W != 2*60+10 {
		t.Error("the function keys are 2, 4 and 3 units wide")
	}
}

// Wrapping on the keyboard works through Navigate: rows wrap, columns do not, and Up from row 1 leaves.
func TestKeyboardFocusMovement(t *testing.T) {
	g := newKBGeometry(draw.Rect{W: 600, H: 280}, 48, 8)
	first, last := gridKeyID(1, 0), gridKeyID(1, kbCols-1)
	if got := Navigate(g.keys, first, DirLeft); got != last {
		t.Errorf("left from q wraps to %s, got %s", last, got)
	}
	if got := Navigate(g.keys, last, DirRight); got != first {
		t.Errorf("right wraps to %s, got %s", first, got)
	}
	if got := Navigate(g.keys, gridKeyID(0, 3), DirUp); got != gridKeyID(0, 3) {
		t.Errorf("up from the top row stays inside the keyboard: %s", got)
	}
	if got := Navigate(g.keys, gridKeyID(3, 4), DirDown); got != keySpace {
		t.Errorf("down from column 4 of row 4 is the space key, got %s", got)
	}
	if got := Navigate(g.keys, keySearch, DirDown); got != keySearch {
		t.Errorf("no wrap down: %s", got)
	}
}

func TestEditFieldInsertDeleteAndCursor(t *testing.T) {
	f := editField{}
	f, _ = f.insert("zelda")
	f = f.moved(-5)
	f, ok := f.insert("link ")
	if !ok || f.String() != "link zelda" || f.cursor != 5 {
		t.Fatalf("%q %d", f.String(), f.cursor)
	}
	f, _ = f.backspace()
	if f.String() != "linkzelda" || f.cursor != 4 {
		t.Fatalf("%q %d", f.String(), f.cursor)
	}
	if f.moved(-50).cursor != 0 || f.moved(50).cursor != len(f.text) {
		t.Error("the cursor stays inside the text")
	}
	if _, ok := (editField{}).backspace(); ok {
		t.Error("nothing to delete")
	}
	if g, ok := f.insert("a\x1b[31m\n\tb"); !ok || strings.ContainsAny(g.String(), "\x1b\n\t") {
		t.Errorf("control characters never enter the field: %q", g.String())
	}
	// A character with accents and Cyrillic count as one character each.
	h, _ := editField{}.insert("ñ я")
	h, _ = h.backspace()
	if h.String() != "ñ " || h.cursor != 2 {
		t.Errorf("%q %d", h.String(), h.cursor)
	}
}

func TestEditFieldStopsAtEightyCharacters(t *testing.T) {
	f, _ := editField{}.insert(strings.Repeat("a", 79))
	f, ok := f.insert("bcd")
	if !ok || len(f.text) != maxQueryRunes || f.String()[79] != 'b' || f.full() != true {
		t.Fatalf("%d runes, ok %v: only the first of three fits", len(f.text), ok)
	}
	if g, ok := f.insert("x"); ok || len(g.text) != maxQueryRunes {
		t.Error("at the limit nothing is inserted")
	}
	f, _ = f.backspace()
	if f.full() {
		t.Error("deleting makes room again")
	}
}

// A long text scrolls inside the field so that the cursor stays visible.
func TestFieldWindowKeepsTheCursorInView(t *testing.T) {
	width := func(s string) int { return len([]rune(s)) * 10 }
	if shown, x := fieldWindow([]rune("zelda"), 5, 200, width); shown != "zelda" || x != 50 {
		t.Errorf("a short text is whole: %q %d", shown, x)
	}
	long := []rune("the legend of zelda a link to the past")
	for _, cursor := range []int{0, 10, 25, len(long)} {
		shown, x := fieldWindow(long, cursor, 200, width)
		if width(shown) > 200 {
			t.Errorf("cursor %d: %q is %d px, wider than the field", cursor, shown, width(shown))
		}
		if x < 0 || x > width(shown) {
			t.Errorf("cursor %d: caret at %d outside the shown text %q", cursor, x, shown)
		}
		if cursor > 0 && cursor < len(long) && !strings.ContainsRune(shown, long[cursor-1]) {
			t.Errorf("cursor %d: the character before it is not shown in %q", cursor, shown)
		}
	}
	if shown, _ := fieldWindow(long, len(long), 200, width); !strings.HasPrefix(shown, "…") {
		t.Errorf("a window that is cut at the left starts with an ellipsis: %q", shown)
	}
}
