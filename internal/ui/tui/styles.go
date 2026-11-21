package tui

import (
	"charm.land/lipgloss/v2"
)

// glyphs are the frame and symbol characters, with an ASCII set for locales that are not UTF-8.
type glyphs struct {
	tl, tr, bl, br, h, v, lt, rt string
	ok, fail, down, up, dots     string
	bar, barEmpty                string
}

func newGlyphs(utf8 bool) glyphs {
	if utf8 {
		return glyphs{"┌", "┐", "└", "┘", "─", "│", "├", "┤", "✔", "✘", "↓", "↑", "…", "█", "░"}
	}
	return glyphs{"+", "+", "+", "+", "-", "|", "+", "+", "[ok]", "[x]", "v", "^", "...", "#", "-"}
}

// styles is the one style table of the TUI, with the roles of cli-conventions section 4. Without
// color only bold, faint and reverse are used, so nothing depends on color.
type styles struct {
	g                                       glyphs
	heading, dim, accent, ok, warn, bad, on lipgloss.Style
	color                                   bool
}

func newStyles(noColor, utf8 bool) styles {
	base := lipgloss.NewStyle()
	fg := func(c string) lipgloss.Style {
		if noColor {
			return base
		}
		return base.Foreground(lipgloss.Color(c))
	}
	return styles{
		g:       newGlyphs(utf8),
		heading: base.Bold(true),
		dim:     base.Faint(true),
		accent:  fg("6"),
		ok:      fg("2"),
		warn:    fg("3"),
		bad:     fg("1").Bold(true),
		on:      base.Reverse(true), // the cursor row, always marked with > as well
		color:   !noColor,
	}
}

// render applies a style to text, leaving empty text alone.
func render(st lipgloss.Style, s string) string {
	if s == "" {
		return s
	}
	return st.Render(s)
}
