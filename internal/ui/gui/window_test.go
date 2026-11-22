package gui

import "testing"

func page(n, from int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = from + i
	}
	return out
}

// The continuous list asks for the next page in the last five rows, and the previous one in the first five.
func TestWindowFetchTriggers(t *testing.T) {
	w := newWindow[int](25)
	w.rows, w.totalPages = page(25, 0), 4
	if w.wantNext(19) || !w.wantNext(20) || !w.wantNext(24) {
		t.Error("the next page is wanted from the fifth row from the end")
	}
	if w.wantPrev(0) {
		t.Error("nothing before page 1")
	}
	w.first, w.rows = 3, page(25, 50)
	if !w.wantPrev(4) || w.wantPrev(5) {
		t.Error("the previous page is wanted in the first five rows")
	}
	w.first = 1
	w.rows = page(100, 0)
	if w.wantNext(99) {
		t.Error("page 4 is the last: nothing to fetch")
	}
	if (window[int]{pageSize: 25}).wantNext(0) {
		t.Error("an empty window waits for its first page")
	}
}

// At most 200 rows stay: the first page drops off when a ninth arrives, and the focus index follows.
func TestWindowDropsTheFarthestPageAtTwoHundredRows(t *testing.T) {
	w := newWindow[int](25)
	w.rows, w.totalPages = page(200, 0), 20
	if w.lastPage() != 8 || w.pagesHeld() != 8 {
		t.Fatalf("8 pages held, last %d", w.lastPage())
	}
	next, dropped := w.withNext(page(25, 200), 20)
	if len(next.rows) != 200 || dropped != 25 || next.first != 2 || next.rows[0] != 25 || next.rows[199] != 224 || next.lastPage() != 9 {
		t.Fatalf("rows %d dropped %d first %d [%d..%d] last %d", len(next.rows), dropped, next.first, next.rows[0], next.rows[199], next.lastPage())
	}
	if len(w.rows) != 200 || w.rows[0] != 0 {
		t.Fatal("the old window must not change")
	}
	back, added := next.withPrev(page(25, 0))
	if len(back.rows) != 200 || added != 25 || back.first != 1 || back.rows[0] != 0 || back.rows[199] != 199 {
		t.Fatalf("rows %d added %d first %d [%d..%d]", len(back.rows), added, back.first, back.rows[0], back.rows[199])
	}
}

func TestWindowGrowsUntilTheLimitWithoutDropping(t *testing.T) {
	w := newWindow[int](25)
	for p := 1; p <= 8; p++ {
		var dropped int
		w, dropped = w.withNext(page(25, (p-1)*25), 12)
		if dropped != 0 {
			t.Fatalf("page %d dropped %d rows below the limit", p, dropped)
		}
	}
	if len(w.rows) != 200 || w.first != 1 || w.totalPages != 12 {
		t.Fatalf("%d rows, first %d, total %d", len(w.rows), w.first, w.totalPages)
	}
	short, _ := newWindow[int](25).withNext(page(7, 0), 1) // a last, short page
	if short.pagesHeld() != 1 || short.lastPage() != 1 || short.wantNext(6) {
		t.Fatal("a single short page is complete")
	}
}
