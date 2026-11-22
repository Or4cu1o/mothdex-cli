package gui

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
)

// maxQueryRunes is the longest search text (docs/ux/gui.md section 5.4).
const maxQueryRunes = 80

// counterWarnAt is where the counter next to the field turns to the warn color.
const counterWarnAt = 70

const (
	kbCols = 10 // keys in a row of the grid
	kbRows = 4  // rows of the grid, above the function row
)

// kbLayout is one set of keys: the grid of kbRows x kbCols and its name.
type kbLayout struct {
	name string
	rows [kbRows][kbCols]string
}

func row(keys string) [kbCols]string {
	var out [kbCols]string
	for i, k := range strings.Fields(keys) {
		out[i] = k
	}
	return out
}

var (
	layoutABC = kbLayout{name: "abc", rows: [kbRows][kbCols]string{
		row("1 2 3 4 5 6 7 8 9 0"),
		row("q w e r t y u i o p"),
		row("a s d f g h j k l '"),
		row("z x c v b n m - . &"),
	}}
	layoutAccents = kbLayout{name: "àé", rows: [kbRows][kbCols]string{
		row("á à â ã ä å æ ç é è"),
		row("ê ë í ì î ï ñ ó ò ô"),
		row("õ ö ø œ ú ù û ü ý ß"),
		row(": ! ? , ( ) / + # _"),
	}}
	layoutCyrillic = kbLayout{name: "абв", rows: [kbRows][kbCols]string{
		row("й ц у к е н г ш щ з"),
		row("х ъ ф ы в а п р о л"),
		row("д ж э я ч с м и т ь"),
		row("б ю ё . - ' & : ! ?"),
	}}
)

// layoutCycle is the layouts Y cycles through: abc and àé, plus the Cyrillic one when the interface language is
// Russian or a Cyrillic letter is already in the field.
func layoutCycle(locale string, text []rune) []kbLayout {
	cycle := []kbLayout{layoutABC, layoutAccents}
	if locale == "ru_ru" || hasCyrillic(text) {
		cycle = append(cycle, layoutCyrillic)
	}
	return cycle
}

func hasCyrillic(text []rune) bool {
	for _, r := range text {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// Ids of the keys: a key of the grid is "k:<row>:<col>" so that focus stays at the same place when the layout
// changes; the function row has its own.
const (
	keyLayout = "k:layout"
	keySpace  = "k:space"
	keyDelete = "k:del"
	keySearch = "k:search"
)

func gridKeyID(row, col int) string {
	return "k:" + strconv.Itoa(row) + ":" + strconv.Itoa(col)
}

// gridKeyAt parses the id of a grid key.
func gridKeyAt(id string) (row, col int, ok bool) {
	if len(id) != 5 || !strings.HasPrefix(id, "k:") || id[3] != ':' {
		return 0, 0, false
	}
	row, col = int(id[2]-'0'), int(id[4]-'0')
	return row, col, row >= 0 && row < kbRows && col >= 0 && col < kbCols
}

// The function row is kbCols units wide: layout (2), space (4), delete (1), search (3).
var functionRow = []struct {
	id    string
	units int
}{{keyLayout, 2}, {keySpace, 4}, {keyDelete, 1}, {keySearch, 3}}

// kbGeometry places the keys inside area: a square cell each (never larger than keyMin), the grid rows then
// the function row, with gap between them. Cells shrink to fit the width and the height of the area.
type kbGeometry struct {
	keys  []Element
	cells map[string]draw.Rect
	rect  draw.Rect
}

func newKBGeometry(area draw.Rect, keyMin, gap int) kbGeometry {
	cellW := min(keyMin, (area.W-(kbCols-1)*gap)/kbCols)
	cellH := min(keyMin, (area.H-kbRows*gap)/(kbRows+1))
	cellW, cellH = max(cellW, 8), max(cellH, 8)
	g := kbGeometry{cells: map[string]draw.Rect{}}
	width := kbCols*cellW + (kbCols-1)*gap
	x0 := area.X
	add := func(id string, r draw.Rect) {
		g.cells[id] = r
		g.keys = append(g.keys, Element{ID: id, Rect: r, Region: regionKeys, WrapRow: true})
	}
	for r := 0; r < kbRows; r++ {
		for c := 0; c < kbCols; c++ {
			add(gridKeyID(r, c), draw.Rect{X: x0 + c*(cellW+gap), Y: area.Y + r*(cellH+gap), W: cellW, H: cellH})
		}
	}
	y := area.Y + kbRows*(cellH+gap)
	x := x0
	for _, f := range functionRow {
		w := f.units*cellW + (f.units-1)*gap
		add(f.id, draw.Rect{X: x, Y: y, W: w, H: cellH})
		x += w + gap
	}
	g.rect = draw.Rect{X: x0, Y: area.Y, W: width, H: (kbRows+1)*cellH + kbRows*gap}
	return g
}

// keyText is what a key types, or "" for a function key.
func keyText(l kbLayout, id string) string {
	if r, c, ok := gridKeyAt(id); ok {
		return l.rows[r][c]
	}
	return ""
}

// editField is the search text and its cursor, as a value.
type editField struct {
	text   []rune
	cursor int // 0..len(text)
}

// insert puts s at the cursor, refusing characters that are not printable and anything past the limit. It
// returns the new field and whether anything was inserted.
func (f editField) insert(s string) (editField, bool) {
	var add []rune
	for _, r := range s {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			continue
		}
		if len(f.text)+len(add) >= maxQueryRunes {
			break
		}
		add = append(add, r)
	}
	if len(add) == 0 {
		return f, false
	}
	text := make([]rune, 0, len(f.text)+len(add))
	text = append(append(append(text, f.text[:f.cursor]...), add...), f.text[f.cursor:]...)
	return editField{text: text, cursor: f.cursor + len(add)}, true
}

// backspace removes the character before the cursor.
func (f editField) backspace() (editField, bool) {
	if f.cursor == 0 {
		return f, false
	}
	text := make([]rune, 0, len(f.text)-1)
	text = append(append(text, f.text[:f.cursor-1]...), f.text[f.cursor:]...)
	return editField{text: text, cursor: f.cursor - 1}, true
}

// moved moves the cursor by delta characters, within the text.
func (f editField) moved(delta int) editField {
	f.cursor = min(max(f.cursor+delta, 0), len(f.text))
	return f
}

func (f editField) String() string { return string(f.text) }

// full reports whether the field is at the character limit.
func (f editField) full() bool { return len(f.text) >= maxQueryRunes }

// fieldWindow chooses the part of a text to show in a field width pixels wide so that the cursor stays in
// view: the whole text when it fits, and otherwise a window that ends a little after the cursor, with an
// ellipsis for what is cut off at the left. width measures a string in pixels. It returns the text to draw
// and the pixel offset of the cursor inside it.
func fieldWindow(text []rune, cursor, maxW int, width func(string) int) (string, int) {
	whole := string(text)
	if width(whole) <= maxW {
		return whole, width(string(text[:cursor]))
	}
	const ellipsis = "…"
	start := 0
	for start < cursor && width(ellipsis+string(text[start:cursor])) > maxW-width("m") {
		start++
	}
	end := cursor
	for end < len(text) && width(ellipsis+string(text[start:end+1])) <= maxW {
		end++
	}
	shown := ellipsis + string(text[start:end])
	if start == 0 {
		shown = string(text[:end])
		return shown, width(string(text[:cursor]))
	}
	return shown, width(ellipsis + string(text[start:cursor]))
}
