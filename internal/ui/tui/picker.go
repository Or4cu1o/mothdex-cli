package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// pickItem is one choice of a picker: the id sent to the service and the text shown.
type pickItem struct{ id, label string }

// pickResult says what a key did to a picker.
type pickResult int

const (
	pickNone pickResult = iota
	pickConfirm
	pickCancel
)

// picker is the filterable multi-select list of the Search screen (tui.md section 2): type to filter,
// Space toggles, Enter confirms, Esc cancels. It is a value; every method returns the next one.
type picker struct {
	title   string
	items   []pickItem
	marked  map[string]bool
	filter  textinput.Model
	cur     int
	loading bool
}

func newPicker(title string, items []pickItem, marked []string, loading bool) picker {
	f := textinput.New()
	f.Prompt = ""
	f.SetWidth(24)
	f.Focus()
	set := map[string]bool{}
	for _, id := range marked {
		set[id] = true
	}
	return picker{title: title, items: items, marked: set, filter: f, loading: loading}
}

// filterItems keeps the items whose id or label contain the text, ignoring case.
func filterItems(items []pickItem, text string) []pickItem {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return items
	}
	var out []pickItem
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.id), text) || strings.Contains(strings.ToLower(it.label), text) {
			out = append(out, it)
		}
	}
	return out
}

func (p picker) visible() []pickItem { return filterItems(p.items, p.filter.Value()) }

// withItems fills the list once it has loaded.
func (p picker) withItems(items []pickItem) picker {
	p.items, p.loading, p.cur = items, false, 0
	return p
}

// toggle flips the mark of the item under the cursor.
func (p picker) toggle() picker {
	vis := p.visible()
	if p.cur < 0 || p.cur >= len(vis) {
		return p
	}
	next := make(map[string]bool, len(p.marked)+1)
	for k, v := range p.marked {
		next[k] = v
	}
	id := vis[p.cur].id
	if next[id] {
		delete(next, id)
	} else {
		next[id] = true
	}
	p.marked = next
	return p
}

// selected returns the marked ids in the order of the list.
func (p picker) selected() []string {
	var out []string
	for _, it := range p.items {
		if p.marked[it.id] {
			out = append(out, it.id)
		}
	}
	return out
}

// move shifts the cursor, staying inside the visible list.
func (p picker) move(delta int) picker {
	n := len(p.visible())
	if n == 0 {
		p.cur = 0
		return p
	}
	p.cur = max(0, min(p.cur+delta, n-1))
	return p
}

// handle reads one key. Typing goes to the filter; the cursor returns to the top when it changes.
func (p picker) handle(k tea.KeyPressMsg) (picker, pickResult) {
	switch normalize(k.String()) {
	case "esc":
		return p, pickCancel
	case "enter":
		return p, pickConfirm
	case "up":
		return p.move(-1), pickNone
	case "down":
		return p.move(1), pickNone
	case "pgup":
		return p.move(-8), pickNone
	case "pgdown":
		return p.move(8), pickNone
	case "space":
		return p.toggle(), pickNone
	}
	before := p.filter.Value()
	p.filter, _ = p.filter.Update(k)
	if p.filter.Value() != before {
		p.cur = 0
	}
	return p, pickNone
}

// scrollTop keeps the cursor inside a window of height rows over n items: it returns the first row
// to show.
func scrollTop(top, cur, height, n int) int {
	if height <= 0 || n <= height {
		return 0
	}
	switch {
	case cur < top:
		top = cur
	case cur >= top+height:
		top = cur - height + 1
	}
	return max(0, min(top, n-height))
}
