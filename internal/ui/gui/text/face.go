// Package text measures, truncates, wraps and rasterizes text for the GUI with the embedded Noto Sans
// (docs/contracts/cli/gui.md section 3). It is pure Go on golang.org/x/image and has no SDL: the backend
// uploads the atlas it fills. A Face is not safe for concurrent use; the GUI draws and measures on one
// goroutine.
package text

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Ellipsis is the character that ends a cut line. The subset font has it (U+2026).
const Ellipsis = "…"

// fallbackRune stands in for a character the font does not have (CJK, emoji): measuring and drawing agree
// on it, so a layout never depends on what was missing.
const fallbackRune = '?'

// Sizes the faces accept, in pixels.
const (
	minSize = 4
	maxSize = 200
)

// Font is a parsed font file. It hands out one Face per pixel size.
type Font struct {
	parsed *opentype.Font
	faces  map[int]*Face
}

// Parse reads a TrueType or OpenType file.
func Parse(ttf []byte) (*Font, error) {
	parsed, err := opentype.Parse(ttf)
	if err != nil {
		return nil, fmt.Errorf("text: parse font: %w", err)
	}
	return &Font{parsed: parsed, faces: map[int]*Face{}}, nil
}

// Face is the font at one size in pixels.
type Face struct {
	face    font.Face
	px      int
	ascent  int
	descent int
	height  int
	advance map[rune]fixed.Int26_6
}

// Face returns the face for a size in pixels, 4 to 200, and keeps it for the next call.
func (f *Font) Face(px int) (*Face, error) {
	if px < minSize || px > maxSize {
		return nil, errors.New("text: size out of range")
	}
	if cached, ok := f.faces[px]; ok {
		return cached, nil
	}
	face, err := opentype.NewFace(f.parsed, &opentype.FaceOptions{Size: float64(px), DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		return nil, fmt.Errorf("text: face at %d px: %w", px, err)
	}
	m := face.Metrics()
	made := &Face{
		face:    face,
		px:      px,
		ascent:  m.Ascent.Ceil(),
		descent: m.Descent.Ceil(),
		height:  m.Height.Ceil(),
		advance: map[rune]fixed.Int26_6{},
	}
	f.faces[px] = made
	return made, nil
}

// Size is the size in pixels.
func (f *Face) Size() int { return f.px }

// Ascent is the distance from the baseline up to the top of the tallest glyph, in pixels.
func (f *Face) Ascent() int { return f.ascent }

// Descent is the distance from the baseline down to the bottom of the lowest glyph, in pixels.
func (f *Face) Descent() int { return f.descent }

// LineHeight is the distance between two baselines, in pixels.
func (f *Face) LineHeight() int { return f.height }

// resolve returns the rune that is actually drawn for r.
func (f *Face) resolve(r rune) rune {
	if _, ok := f.face.GlyphAdvance(r); ok {
		return r
	}
	return fallbackRune
}

func (f *Face) advanceOf(r rune) fixed.Int26_6 {
	if a, ok := f.advance[r]; ok {
		return a
	}
	a, _ := f.face.GlyphAdvance(r)
	f.advance[r] = a
	return a
}

// Covers reports whether the font has a glyph for every drawable character of s, so a label can fall back to another font
// when it does not (the display face is only used for labels it can draw whole).
func (f *Face) Covers(s string) bool {
	for _, r := range s {
		if isControl(r) {
			continue
		}
		if _, ok := f.face.GlyphAdvance(r); !ok {
			return false
		}
	}
	return true
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) }

// Width is the width of a single line in pixels. Control characters count as nothing.
func (f *Face) Width(s string) int {
	var w fixed.Int26_6
	for _, r := range s {
		if !isControl(r) {
			w += f.advanceOf(f.resolve(r))
		}
	}
	return w.Round()
}

// Placed is one character with the pixel column of its origin, as Layout reports it.
type Placed struct {
	Rune rune // the rune to rasterize: already replaced when the font lacks the original
	X    int
}

// Layout appends the position of each drawable character of a line to out. The origins accumulate the
// font's fractional advances and round once, so the line is as wide as Width says.
func (f *Face) Layout(s string, out []Placed) []Placed {
	var pos fixed.Int26_6
	for _, r := range s {
		if isControl(r) {
			continue
		}
		r = f.resolve(r)
		out = append(out, Placed{Rune: r, X: pos.Round()})
		pos += f.advanceOf(r)
	}
	return out
}

// Fit returns s when it fits in maxWidth pixels, and otherwise the longest start of it that, followed by an
// ellipsis, does. It returns "" when not even the ellipsis fits.
func (f *Face) Fit(s string, maxWidth int) string {
	if f.Width(s) <= maxWidth {
		return s
	}
	return f.cut(s, maxWidth)
}

// cut is the longest start of s that fits with an ellipsis after it, whether or not s itself would fit.
func (f *Face) cut(s string, maxWidth int) string {
	ell := f.Width(Ellipsis)
	if ell > maxWidth {
		return ""
	}
	end := 0
	var pos fixed.Int26_6
	for i, r := range s {
		if !isControl(r) {
			pos += f.advanceOf(f.resolve(r))
		}
		if pos.Round()+ell > maxWidth {
			break
		}
		end = i + utf8.RuneLen(r)
	}
	return strings.TrimRight(s[:end], " ") + Ellipsis
}

// Wrap breaks s into lines no wider than maxWidth at spaces; a word wider than the line is cut where it
// stops fitting. With maxLines above zero, text that does not fit ends its last line with an ellipsis.
func (f *Face) Wrap(s string, maxWidth, maxLines int) []string {
	var lines []string
	for _, paragraph := range strings.Split(s, "\n") {
		lines = append(lines, f.wrapParagraph(paragraph, maxWidth)...)
	}
	if maxLines > 0 && len(lines) > maxLines {
		rest := strings.Join(lines[maxLines-1:], " ")
		lines = append(lines[:maxLines-1], f.cut(rest, maxWidth))
	}
	return lines
}

func (f *Face) wrapParagraph(p string, maxWidth int) []string {
	words := strings.Fields(p)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	line := ""
	flush := func() {
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
	}
	for _, word := range words {
		for maxWidth > 0 && f.Width(word) > maxWidth { // a word wider than the line
			flush()
			head := f.longestPrefix(word, maxWidth)
			lines = append(lines, head)
			word = word[len(head):]
		}
		switch {
		case line == "":
			line = word
		case f.Width(line+" "+word) <= maxWidth:
			line += " " + word
		default:
			flush()
			line = word
		}
	}
	flush()
	return lines
}

// longestPrefix is the longest start of s that fits in maxWidth, and at least one character so that
// wrapping always makes progress.
func (f *Face) longestPrefix(s string, maxWidth int) string {
	end := 0
	var pos fixed.Int26_6
	for i, r := range s {
		pos += f.advanceOf(f.resolve(r))
		if pos.Round() > maxWidth && end > 0 {
			break
		}
		end = i + utf8.RuneLen(r)
	}
	return s[:end]
}
