package gui

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// Start menu > Buttons shows the buttons of the screen underneath with the labels printed on the pad in hand, and the
// words of that screen's hint bar (docs/ux/gui.md section 5.11).
func TestButtonsOverlayListsTheButtonsOfTheScreenWithThePadsLabels(t *testing.T) {
	svc := guitest.New()
	h := started(t, svc, 1280, 720)
	h.send(ControllerMsg{Type: input.TypeSwitchPro})
	h.press(input.Menu)
	for i := 0; i < 4; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm) // Buttons
	if h.m.overlay != overlayButtons {
		t.Fatalf("overlay %d", h.m.overlay)
	}
	h.mustHave("Buttons", "Open", "Quit", "Queue", "Move", "Menu")
	rows := h.m.buttonRows()
	if rows[0].labels[0] != "B" || rows[0].word != "Open" || rows[1].labels[0] != "A" { // a Nintendo pad prints B at the south position
		t.Fatalf("rows %+v", rows)
	}
	h.press(input.Back)
	if h.m.overlay != overlayNone {
		t.Fatal("B closes it")
	}

	// A screen with more buttons lists them: Queue has Details, and no Select row (it is the Queue).
	h.press(input.Queue)
	h.press(input.Menu)
	for i := 0; i < 4; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm)
	rows = h.m.buttonRows()
	words := map[string]bool{}
	for _, r := range rows {
		words[r.word] = true
	}
	if !words["Actions"] || !words["Details"] || words["Queue"] {
		t.Fatalf("rows %+v", rows)
	}
}
