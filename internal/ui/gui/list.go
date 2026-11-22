package gui

// listState is the cursor of a vertical list that has more rows than fit: which row has focus and which is the
// first one shown. Lists do not wrap (docs/ux/gui.md section 3).
type listState struct {
	focus int
	top   int
}

// contextRows is how many rows of context stay visible above and below the focused one, except at the ends.
const contextRows = 1

// scrollTop returns the first visible row so that the focused row keeps a row of context on each side (when
// the window has room for that) and never scrolls past the ends.
func scrollTop(top, focus, visible, n int) int {
	if visible <= 0 || n <= visible {
		return 0
	}
	ctx := contextRows
	if visible < 2*ctx+1 {
		ctx = 0
	}
	if focus < top+ctx {
		top = focus - ctx
	}
	if focus > top+visible-1-ctx {
		top = focus - (visible - 1 - ctx)
	}
	return min(max(top, 0), n-visible)
}

// clampFocus keeps focus inside a list of n rows (-1 for an empty one is returned as 0).
func clampFocus(focus, n int) int { return min(max(focus, 0), max(n-1, 0)) }

// moved moves focus by delta rows (no wrap) and scrolls to keep it in view.
func (l listState) moved(delta, visible, n int) listState {
	l.focus = clampFocus(l.focus+delta, n)
	l.top = scrollTop(l.top, l.focus, visible, n)
	return l
}

// paged moves focus by one visible page of rows (L and R), keeping the focused row's place in the window.
func (l listState) paged(dir, visible, n int) listState {
	step := max(visible-2*contextRows, 1)
	return l.moved(dir*step, visible, n)
}

// shown clamps a stored list state to n rows and returns the range of rows to draw.
func (l listState) shown(visible, n int) (listState, int, int) {
	l.focus = clampFocus(l.focus, n)
	l.top = scrollTop(l.top, l.focus, visible, n)
	return l, l.top, min(l.top+visible, n)
}
