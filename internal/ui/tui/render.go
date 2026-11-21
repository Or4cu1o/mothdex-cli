package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

// cells is the display width of s: wide characters count 2, escape sequences 0.
func cells(s string) int { return lipgloss.Width(s) }

// cutTo shortens s to w cells with a trailing "..." (three ASCII dots, safe on legacy consoles).
func cutTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "...")
}

// fit makes s exactly w cells: cut with "..." or padded with spaces on the right.
func fit(s string, w int) string {
	s = cutTo(s, w)
	if gap := w - cells(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// padLeft right-aligns s in w cells.
func padLeft(s string, w int) string {
	if gap := w - cells(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// clean is every string that came from the server before it is drawn (case 49): no control or
// format characters, so no escape sequence can reach the terminal.
func clean(s string) string { return app.SanitizeText(s) }

// frameSpec is what a screen puts inside the frame.
type frameSpec struct {
	title  string   // after "mothdex ─ "
	right  string   // the live queue summary, hidden when empty
	body   []string // already styled lines; cut and padded to the frame
	footer string   // text embedded in the separator line (Results page footer)
	status string   // the key help or a toast
}

// frame draws the screen frame of tui.md section 1 at w x h: a title bar, the body, a separator
// (with the footer text), the status bar and the bottom edge. It is exactly h lines.
func (st styles) frame(f frameSpec, w, h int) string {
	g := st.g
	inner := w - 2
	bodyH := h - 4
	line := func(mid string) string { return g.v + " " + fit(mid, inner-2) + " " + g.v }
	var out []string

	head := " " + st.heading.Render("mothdex") + " " + g.h + " " + f.title + " "
	tail := g.tr
	if f.right != "" {
		tail = " " + f.right + " " + g.tr
	}
	room := inner - cells(head) - (cells(tail) - 1)
	if room < 1 {
		head = " " + cutTo(f.title, max(inner-cells(tail)-4, 1)) + " "
		room = max(inner-cells(head)-(cells(tail)-1), 0)
	}
	out = append(out, g.tl+head+strings.Repeat(g.h, room)+tail)

	for i := 0; i < bodyH; i++ {
		row := ""
		if i < len(f.body) {
			row = f.body[i]
		}
		out = append(out, line(row))
	}

	if f.footer == "" {
		out = append(out, g.lt+strings.Repeat(g.h, inner)+g.rt)
	} else {
		foot := " " + cutTo(f.footer, inner-3) + " "
		out = append(out, g.lt+foot+strings.Repeat(g.h, max(inner-cells(foot), 0))+g.rt)
	}
	out = append(out, line(f.status))
	out = append(out, g.bl+strings.Repeat(g.h, inner)+g.br)
	return strings.Join(out, "\n")
}

// spinner frames of the loading indicator; the ASCII set serves non-UTF-8 locales.
var (
	spinnerUTF8  = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerASCII = []string{"|", "/", "-", "\\"}
)

// spin is the spinner frame for a counter.
func (st styles) spin(n int) string {
	frames := spinnerASCII
	if st.g.ok == "✔" {
		frames = spinnerUTF8
	}
	return frames[n%len(frames)]
}
