package text

import (
	"image"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/assets"
)

func testFace(t *testing.T, px int) *Face {
	t.Helper()
	font, err := Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	face, err := font.Face(px)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func TestParseRejectsGarbageAndFaceRejectsSizesOutOfRange(t *testing.T) {
	if _, err := Parse([]byte("not a font")); err == nil {
		t.Fatal("garbage parsed as a font")
	}
	font, err := Parse(assets.NotoSans)
	if err != nil {
		t.Fatal(err)
	}
	for _, px := range []int{-1, 0, 3, 201} {
		if _, err := font.Face(px); err == nil {
			t.Errorf("size %d accepted", px)
		}
	}
	a, _ := font.Face(16)
	if b, _ := font.Face(16); a != b {
		t.Error("the same size gave two faces")
	}
}

func TestMetricsGrowWithTheSize(t *testing.T) {
	small, big := testFace(t, 12), testFace(t, 24)
	if small.Ascent() <= 0 || small.Descent() <= 0 || small.LineHeight() < small.Ascent()+small.Descent()-2 {
		t.Fatalf("12 px metrics %d/%d/%d", small.Ascent(), small.Descent(), small.LineHeight())
	}
	if big.LineHeight() <= small.LineHeight() || big.Width("Mothdex") <= small.Width("Mothdex") {
		t.Fatal("a larger size must be taller and wider")
	}
}

func TestWidthIsAdditiveWithinARoundingPixelAndIgnoresControls(t *testing.T) {
	f := testFace(t, 16)
	if f.Width("") != 0 {
		t.Fatal("empty string has a width")
	}
	if f.Width("a\tb\nc\x00") != f.Width("abc") {
		t.Error("control characters must count as nothing")
	}
	whole, parts := f.Width("Download queue"), f.Width("Download")+f.Width(" ")+f.Width("queue")
	if d := whole - parts; d < -2 || d > 2 {
		t.Errorf("width %d against the sum of parts %d", whole, parts)
	}
	if f.Width("iiii") >= f.Width("MMMM") {
		t.Error("a proportional font: i is narrower than M")
	}
}

// A character the font lacks is measured and drawn as a question mark, so layouts do not depend on it.
func TestMissingGlyphsFallBackToAQuestionMark(t *testing.T) {
	f := testFace(t, 16)
	if f.Width("日本") != f.Width("??") {
		t.Errorf("CJK width %d, '??' width %d", f.Width("日本"), f.Width("??"))
	}
	placed := f.Layout("a日", nil)
	if len(placed) != 2 || placed[0].Rune != 'a' || placed[1].Rune != '?' {
		t.Fatalf("layout %+v", placed)
	}
	for _, s := range []string{"Привет", "Ñandú", "€ 5", "—"} {
		for _, p := range f.Layout(s, nil) {
			if p.Rune == '?' {
				t.Errorf("%q: a glyph the subset should have fell back", s)
			}
		}
	}
}

func TestLayoutOriginsIncreaseAndEndWithinWidth(t *testing.T) {
	f := testFace(t, 18)
	s := "Mothdex 1.0 — Привет"
	placed := f.Layout(s, nil)
	if len(placed) != len([]rune(s)) {
		t.Fatalf("%d placed for %d runes", len(placed), len([]rune(s)))
	}
	for i := 1; i < len(placed); i++ {
		if placed[i].X < placed[i-1].X {
			t.Fatalf("origins go backwards at %d: %+v", i, placed)
		}
	}
	if last := placed[len(placed)-1].X; last > f.Width(s) {
		t.Errorf("last origin %d past the width %d", last, f.Width(s))
	}
}

func TestFitKeepsWhatFitsAndCutsWithAnEllipsisThatFits(t *testing.T) {
	f := testFace(t, 16)
	s := "Super Mario World (USA) (Rev 1)"
	if got := f.Fit(s, f.Width(s)); got != s {
		t.Fatalf("a string that fits was changed: %q", got)
	}
	for _, maxW := range []int{40, 77, 120, f.Width(s) - 1} {
		got := f.Fit(s, maxW)
		if !strings.HasSuffix(got, Ellipsis) || f.Width(got) > maxW {
			t.Errorf("Fit(%d) = %q (%d px)", maxW, got, f.Width(got))
		}
		if !strings.HasPrefix(s, strings.TrimSuffix(got, Ellipsis)) {
			t.Errorf("Fit(%d) = %q is not the start of the text", maxW, got)
		}
	}
	if got := f.Fit(s, f.Width(Ellipsis)-1); got != "" {
		t.Errorf("not even the ellipsis fits, got %q", got)
	}
	if got := f.Fit("Привет, мир", 50); f.Width(got) > 50 || !strings.HasSuffix(got, Ellipsis) {
		t.Errorf("Cyrillic cut: %q", got)
	}
}

func TestFitNeverSplitsARune(t *testing.T) {
	f := testFace(t, 16)
	for w := 10; w < 200; w += 7 {
		got := f.Fit("Ñandú — Привет мир €", w)
		if strings.ToValidUTF8(got, "�") != got {
			t.Fatalf("width %d: invalid UTF-8 in %q", w, got)
		}
	}
}

func TestWrapBreaksAtSpacesAndCutsLongWords(t *testing.T) {
	f := testFace(t, 16)
	text := "Press the south button to confirm and the east button to go back"
	for _, w := range []int{120, 200, 400} {
		lines := f.Wrap(text, w, 0)
		if strings.Join(lines, " ") != text {
			t.Errorf("width %d lost or changed words: %q", w, lines)
		}
		for _, l := range lines {
			if f.Width(l) > w {
				t.Errorf("width %d: %q is %d px", w, l, f.Width(l))
			}
		}
	}
	long := strings.Repeat("W", 40)
	lines := f.Wrap(long, 100, 0)
	if len(lines) < 2 || strings.Join(lines, "") != long {
		t.Fatalf("a long word was not cut across lines: %q", lines)
	}
	for _, l := range lines {
		if f.Width(l) > 100 && len([]rune(l)) > 1 {
			t.Errorf("%q is wider than the line", l)
		}
	}
}

func TestWrapKeepsParagraphsAndEmptyLines(t *testing.T) {
	f := testFace(t, 16)
	got := f.Wrap("one\n\ntwo", 300, 0)
	if len(got) != 3 || got[0] != "one" || got[1] != "" || got[2] != "two" {
		t.Fatalf("%q", got)
	}
	if got := f.Wrap("", 300, 0); len(got) != 1 || got[0] != "" {
		t.Fatalf("empty text: %q", got)
	}
}

func TestWrapWithMaxLinesEndsTheLastLineWithAnEllipsis(t *testing.T) {
	f := testFace(t, 16)
	text := strings.Repeat("word ", 40)
	lines := f.Wrap(text, 160, 3)
	if len(lines) != 3 {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	if !strings.HasSuffix(lines[2], Ellipsis) || f.Width(lines[2]) > 160 {
		t.Errorf("last line %q (%d px)", lines[2], f.Width(lines[2]))
	}
	if strings.Contains(lines[0]+lines[1], Ellipsis) {
		t.Error("only the last line is cut")
	}
	if fit := f.Wrap("short text", 300, 3); len(fit) != 1 || fit[0] != "short text" {
		t.Errorf("text that fits was changed: %q", fit)
	}
}

func TestAtlasRasterizesGlyphsOnceWithoutOverlap(t *testing.T) {
	f := testFace(t, 20)
	a := NewAtlas(256, 256)
	var entries []Entry
	for _, r := range "AbgÑЖ€.,j" {
		e, ok := a.Glyph(f, r)
		if !ok || e.W == 0 || e.H == 0 {
			t.Fatalf("glyph %q: %+v ok=%v", r, e, ok)
		}
		if again, _ := a.Glyph(f, r); again != e {
			t.Fatalf("glyph %q moved on a second ask", r)
		}
		entries = append(entries, e)
	}
	rect := func(e Entry) image.Rectangle { return image.Rect(e.X, e.Y, e.X+e.W, e.Y+e.H) }
	for i, e := range entries {
		if !rect(e).In(image.Rect(0, 0, 256, 256)) {
			t.Errorf("entry %d outside the atlas: %+v", i, e)
		}
		for j := i + 1; j < len(entries); j++ {
			if rect(e).Inset(-padding).Overlaps(rect(entries[j])) {
				t.Errorf("entries %d and %d touch: %+v %+v", i, j, e, entries[j])
			}
		}
	}
	ink := 0
	for _, c := range a.Coverage() {
		if c > 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("no ink in the atlas")
	}
	if r, ok := a.TakeDirty(); !ok || r.Empty() {
		t.Fatal("rasterizing must mark a dirty rectangle")
	}
	if _, ok := a.TakeDirty(); ok {
		t.Fatal("the dirty rectangle must be taken once")
	}
}

func TestSpaceHasNoInkAndAGlyphLargerThanTheAtlasIsRefused(t *testing.T) {
	f := testFace(t, 20)
	a := NewAtlas(256, 256)
	if e, ok := a.Glyph(f, ' '); !ok || e.W != 0 || e.H != 0 {
		t.Fatalf("space: %+v ok=%v", e, ok)
	}
	if _, ok := NewAtlas(8, 8).Glyph(testFace(t, 48), 'W'); ok {
		t.Fatal("a glyph larger than the atlas was accepted")
	}
}

func TestAtlasStartsAgainWhenFullAndSaysSo(t *testing.T) {
	f := testFace(t, 24)
	a := NewAtlas(64, 64)
	gen := a.Generation()
	for r := 'A'; r <= 'z'; r++ {
		if _, ok := a.Glyph(f, r); !ok {
			t.Fatalf("glyph %q refused", r)
		}
	}
	if a.Generation() == gen {
		t.Fatal("a 64x64 atlas cannot hold 58 glyphs of 24 px: it must have started again")
	}
	e, ok := a.Glyph(f, 'Z')
	if !ok || e.W == 0 {
		t.Fatalf("a glyph after the restart: %+v ok=%v", e, ok)
	}
}

func TestAppendARGBIsWhiteWithTheCoverageAsAlpha(t *testing.T) {
	f := testFace(t, 20)
	a := NewAtlas(64, 64)
	e, _ := a.Glyph(f, 'I')
	rect := image.Rect(e.X, e.Y, e.X+e.W, e.Y+e.H)
	pix, pitch := a.AppendARGB(nil, rect)
	if pitch != e.W*4 || len(pix) != pitch*e.H {
		t.Fatalf("pitch %d, %d bytes for %dx%d", pitch, len(pix), e.W, e.H)
	}
	opaque := 0
	for i := 0; i < len(pix); i += 4 {
		if pix[i] != 0xff || pix[i+1] != 0xff || pix[i+2] != 0xff {
			t.Fatalf("pixel %d is not white: % x", i/4, pix[i:i+4])
		}
		if pix[i+3] > 0xc0 {
			opaque++
		}
	}
	if opaque == 0 {
		t.Fatal("the stem of an I has opaque pixels")
	}
	clipped, _ := a.AppendARGB(nil, image.Rect(-10, -10, 100, 100))
	if len(clipped) != 64*64*4 {
		t.Fatalf("a rectangle larger than the atlas must be clipped: %d bytes", len(clipped))
	}
}
