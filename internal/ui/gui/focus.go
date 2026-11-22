package gui

import "github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"

// Element is something the pad can focus: an id that survives relayout (a slug, a setting key, a tile name),
// where it is, and which region of the screen it belongs to (docs/ux/gui.md section 3).
type Element struct {
	ID     string
	Rect   draw.Rect
	Region int
	// WrapRow makes Left on the first element of its row go to the last, and Right the other way round:
	// rows of tiles and keyboard keys wrap, lists do not.
	WrapRow bool
}

// Dir is a direction of the D-pad.
type Dir int

// Directions.
const (
	DirUp Dir = iota
	DirDown
	DirLeft
	DirRight
)

// Navigate returns the id that gets focus after moving from current in direction d. Inside and across
// regions alike the target is chosen by geometry:
//   - Up and Down pick the nearest row in that direction, then the element in it whose center is closest
//     to the current one horizontally, so leaving a region enters the next one at the closest position;
//   - Left and Right stay on the current row (the rectangles overlap vertically) and pick the nearest;
//     with nothing further, a WrapRow element goes to the other end of its row, and any other stays.
//
// It returns current when there is nowhere to go, and the first element when current is not in the list.
func Navigate(elems []Element, current string, d Dir) string {
	cur, ok := find(elems, current)
	if !ok {
		return First(elems)
	}
	var best string
	var found bool
	if d == DirUp || d == DirDown {
		best, found = nextRow(elems, cur, d)
	} else {
		best, found = nextInRow(elems, cur, d)
	}
	if !found && cur.WrapRow && (d == DirLeft || d == DirRight) {
		best, found = wrapRow(elems, cur, d)
	}
	if !found {
		return current
	}
	return best
}

// First is the id of the first element, or "" when there is none.
func First(elems []Element) string {
	if len(elems) == 0 {
		return ""
	}
	return elems[0].ID
}

// Restore returns id when it is still on the screen, and otherwise the element nearest to where it was
// (prev), so a list that lost the focused row keeps the person close to it.
func Restore(elems []Element, id string, prev draw.Rect) string {
	if _, ok := find(elems, id); ok {
		return id
	}
	if len(elems) == 0 {
		return ""
	}
	px, py := prev.Center()
	best, bestDist := elems[0].ID, -1
	for _, e := range elems {
		cx, cy := e.Rect.Center()
		dist := (cx-px)*(cx-px) + (cy-py)*(cy-py)
		if bestDist < 0 || dist < bestDist {
			best, bestDist = e.ID, dist
		}
	}
	return best
}

func find(elems []Element, id string) (Element, bool) {
	for _, e := range elems {
		if e.ID == id {
			return e, true
		}
	}
	return Element{}, false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// nextRow finds the nearest row above or below and, in it, the element horizontally closest to cur.
func nextRow(elems []Element, cur Element, d Dir) (string, bool) {
	cx, cy := cur.Rect.Center()
	var cands []Element
	nearest := -1
	for _, e := range elems {
		if e.ID == cur.ID {
			continue
		}
		_, ey := e.Rect.Center()
		dist := ey - cy
		if d == DirUp {
			dist = -dist
		}
		if dist <= 0 {
			continue
		}
		cands = append(cands, e)
		if nearest < 0 || dist < nearest {
			nearest = dist
		}
	}
	best, bestDX, found := "", 0, false
	for _, e := range cands {
		ex, ey := e.Rect.Center()
		dist := ey - cy
		if d == DirUp {
			dist = -dist
		}
		if dist > nearest+cur.Rect.H/2 { // not the nearest row
			continue
		}
		if dx := abs(ex - cx); !found || dx < bestDX {
			best, bestDX, found = e.ID, dx, true
		}
	}
	return best, found
}

// sameRow reports whether two rectangles overlap vertically.
func sameRow(a, b draw.Rect) bool { return a.Y < b.Bottom() && b.Y < a.Bottom() }

// nextInRow finds the nearest element to the left or right on the same row.
func nextInRow(elems []Element, cur Element, d Dir) (string, bool) {
	cx, cy := cur.Rect.Center()
	best, bestDist, bestDY, found := "", 0, 0, false
	for _, e := range elems {
		if e.ID == cur.ID || !sameRow(cur.Rect, e.Rect) {
			continue
		}
		ex, ey := e.Rect.Center()
		dist := ex - cx
		if d == DirLeft {
			dist = -dist
		}
		if dist <= 0 {
			continue
		}
		dy := abs(ey - cy)
		if !found || dist < bestDist || (dist == bestDist && dy < bestDY) {
			best, bestDist, bestDY, found = e.ID, dist, dy, true
		}
	}
	return best, found
}

// wrapRow finds the far end of the current row, in the same region, for a wrapping move.
func wrapRow(elems []Element, cur Element, d Dir) (string, bool) {
	best, bestX, found := "", 0, false
	for _, e := range elems {
		if e.ID == cur.ID || e.Region != cur.Region || !sameRow(cur.Rect, e.Rect) {
			continue
		}
		ex, _ := e.Rect.Center()
		// Right wraps to the leftmost element, Left to the rightmost.
		if !found || (d == DirRight && ex < bestX) || (d == DirLeft && ex > bestX) {
			best, bestX, found = e.ID, ex, true
		}
	}
	return best, found
}
