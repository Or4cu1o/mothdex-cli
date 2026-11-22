package gui

// The Results list is continuous: pages of the service are appended as the focus nears the end, and the
// window keeps at most maxWindowRows rows. Beyond that the page farthest from the focus drops off, and comes
// back from the service cache if the person scrolls back to it (docs/ux/gui.md section 5.6).
const (
	servicePageSize = 25  // Search is asked with PageSize 25
	maxWindowRows   = 200 // at most eight pages stay loaded
	fetchAhead      = 5   // the next page is asked when the focus is in the last five rows
)

// window is the loaded part of a paged list: whole service pages first..first+len(rows)/pageSize-1.
type window[T any] struct {
	rows       []T
	first      int // the 1-based service page of rows[0]
	pageSize   int
	totalPages int
}

func newWindow[T any](pageSize int) window[T] { return window[T]{first: 1, pageSize: pageSize} }

// pagesHeld is how many service pages the window holds (the last may be short).
func (w window[T]) pagesHeld() int { return (len(w.rows) + w.pageSize - 1) / w.pageSize }

// lastPage is the highest service page in the window, 0 when it is empty.
func (w window[T]) lastPage() int {
	if len(w.rows) == 0 {
		return 0
	}
	return w.first + w.pagesHeld() - 1
}

// wantNext says whether the page after the window must be fetched for a focus at row focus.
func (w window[T]) wantNext(focus int) bool {
	return len(w.rows) > 0 && w.lastPage() < w.totalPages && focus >= len(w.rows)-fetchAhead
}

// wantPrev says whether the page before the window must be fetched.
func (w window[T]) wantPrev(focus int) bool {
	return w.first > 1 && focus < fetchAhead
}

// withNext appends the page after the window and returns the new window, the rows dropped from its front
// (the focus index moves back by that many) and the total pages the service reported.
func (w window[T]) withNext(page []T, totalPages int) (next window[T], dropped int) {
	w.totalPages = totalPages
	rows := make([]T, 0, len(w.rows)+len(page))
	rows = append(append(rows, w.rows...), page...)
	if len(rows) > maxWindowRows {
		dropped = len(rows) - maxWindowRows
		rows = rows[dropped:]
		w.first += dropped / w.pageSize
	}
	w.rows = rows
	return w, dropped
}

// withPrev puts the page before the window in front of it and returns the new window and how many rows were
// added in front (the focus index moves forward by that many); rows beyond the limit drop off the end.
func (w window[T]) withPrev(page []T) (next window[T], added int) {
	rows := make([]T, 0, len(w.rows)+len(page))
	rows = append(append(rows, page...), w.rows...)
	if len(rows) > maxWindowRows {
		rows = rows[:maxWindowRows]
	}
	w.rows = rows
	w.first--
	return w, len(page)
}
