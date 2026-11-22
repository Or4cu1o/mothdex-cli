//go:build gui

package sdl

import "testing"

// On Windows a process started hidden can have its first ShowWindow answered with the start-up show state (RetroBat runs
// the .bat with a hidden window style, ports.md PRT-G12): the window must be asked again, and raised each time.
func TestEnsureShownAsksAgainWhenTheFirstShowIsSwallowed(t *testing.T) {
	tests := []struct {
		name           string
		swallowedFirst int // how many shows leave the window hidden
		wantCalls      int
		wantShown      bool
	}{
		{"the normal case: one call", 0, 1, true},
		{"the first show is swallowed", 1, 2, true},
		{"it never shows: gives up after two", 5, maxShowCalls, false},
	}
	for _, tc := range tests {
		shows, raises := 0, 0
		shown := false
		calls, ok := ensureShown(windowSteps{
			show:  func() { shows++; shown = shows > tc.swallowedFirst },
			raise: func() { raises++ },
			shown: func() bool { return shown },
		})
		if calls != tc.wantCalls || ok != tc.wantShown || raises != calls {
			t.Errorf("%s: %d calls (%d raises), shown %v; want %d calls, shown %v, one raise per call", tc.name, calls, raises, ok, tc.wantCalls, tc.wantShown)
		}
	}
}

// A window that is not hidden is shown (and so visible to SDL) when the backend has opened it; a hidden one stays hidden. The
// dummy video driver keeps the flag like a real one.
func TestTheBackendShowsItsWindowUnlessAskedToHideIt(t *testing.T) {
	shown, _ := headless(t, 640, 480) // headless opens a Hidden window
	if shown.lib.WindowFlags(shown.win)&WindowShown != 0 {
		t.Error("a hidden window must stay hidden")
	}
	visible := openVisible(t, 640, 480)
	if visible.lib.WindowFlags(visible.win)&WindowShown == 0 {
		t.Error("the window the backend opened is not shown")
	}
}
