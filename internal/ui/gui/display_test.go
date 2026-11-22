package gui

import (
	"slices"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// displayRuns are the text runs of the frame drawn in the display face, in drawing order.
func displayRuns(h *harness) []string {
	var out []string
	for _, op := range h.render().Ops {
		if op.Kind == draw.KindText && op.Display {
			out = append(out, op.Text)
		}
	}
	return out
}

// The display face (Handjet) draws the short labels of DESIGN.md Type, in all four languages: the wordmark, the Home tile
// labels and the Settings section headers. Nothing else is in it: a sentence, a row or a game title stays in Noto Sans.
func TestTheDisplayFaceDrawsTheShortLabelsInEveryLanguage(t *testing.T) {
	for _, loc := range []string{"en_us", "pt_br", "es_es", "ru_ru"} {
		h := started(t, guitest.New(), 1280, 720, withLocale(loc))
		runs := displayRuns(h)
		if !slices.Contains(runs, brand) {
			t.Errorf("%s Home: the wordmark is not in the display face: %q", loc, runs)
		}
		for _, id := range []string{tileSearch, tilePlatforms, tileRandom, tileQueue, tileHistory, tileSettings} {
			if name := h.m.tileName(id); !slices.Contains(runs, name) {
				t.Errorf("%s Home: the tile %q is not drawn whole in the display face: %q", loc, name, runs)
			}
		}
		if got := len(runs); got < 7 {
			t.Errorf("%s Home: only %d display runs", loc, got)
		}

		h.press(input.Menu)
		h.press(input.Down)
		h.press(input.Down)
		h.press(input.Down)
		h.press(input.Confirm) // Settings
		for _, run := range displayRuns(h) {
			if run == "" || len([]rune(run)) > 40 {
				t.Errorf("%s Settings: %q is not a short label", loc, run)
			}
		}
		if len(displayRuns(h)) < 3 {
			t.Errorf("%s Settings: the title and the section headers are in the display face: %q", loc, displayRuns(h))
		}
	}
}

// A label the display face cannot draw whole is drawn in the body face at the title size, never half in each.
func TestALabelWithAMissingGlyphFallsBackToTheBodyFace(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	p := h.m.painter(nil)
	if d := p.display("Settings"); !d.disp || d.dsize() != h.m.Metrics().Display {
		t.Fatalf("a Latin label is in the display face at the display size: %+v", d.dsize())
	}
	if d := p.display("Settings", "設定"); d.disp || d.dsize() != h.m.Metrics().Title {
		t.Fatalf("a label with a missing glyph falls back to the body face at the title size: disp=%v size=%d", d.disp, d.dsize())
	}
}
