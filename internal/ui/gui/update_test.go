package gui

import (
	"context"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

type fakeUpdates struct {
	release   Release
	checks    int
	applies   int
	installed bool
	failure   error // returned by Apply after the confirmation
	notified  []string
}

func (f *fakeUpdates) MarkNotified(v string) error { f.notified = append(f.notified, v); return nil }

func (f *fakeUpdates) Check(context.Context) (Release, error) { f.checks++; return f.release, nil }

func (f *fakeUpdates) Apply(_ context.Context, progress func(UpdateProgress), confirm func(Staged) bool) error {
	f.applies++
	progress(UpdateProgress{Done: 1 << 20, Total: 2 << 20})
	if !confirm(Staged{From: "0.2.0", To: "0.3.0", Signature: "none", Notes: "### Added\n- a \x1b[31mthing"}) {
		return &app.Error{Code: app.CodeCanceled}
	}
	f.installed = f.failure == nil
	return f.failure
}

func newer() *fakeUpdates {
	return &fakeUpdates{release: Release{Version: "0.3.0", PageURL: "https://example.test/v0.3.0", Notify: true}}
}

// toUpdate opens Settings › About, whose first action is Update, and presses A.
func toUpdate(t *testing.T, f *fakeUpdates) *harness {
	t.Helper()
	h := openSettingsFromHome(t, settingsFixture(), 1280, 720)
	h.m.o.Updates, h.m.upd.checked = f, false
	h.send(TickMsg{}) // not on Home: no check from here
	if f.checks != 0 {
		t.Fatal("the check ran off Home")
	}
	h.m.upd.release = f.release
	openAbout(t, h)
	h.mustHave("New version: 0.3.0", "Update")
	h.press(input.Confirm)
	return h
}

// The check starts once Home is up, never on the Splash, and is told once.
func TestTheUpdateCheckRunsOnceAfterTheStartScreen(t *testing.T) {
	f := newer()
	h := newHarness(t, guitest.New(), 1280, 720, func(o *Options) { o.Updates = f })
	if f.checks != 0 || h.m.screen != screenSplash {
		t.Fatalf("checks %d on screen %d: the start screen must not wait for it", f.checks, h.m.screen)
	}
	h.run(h.pending)
	h.advance(0)
	if f.checks != 1 || f.applies != 0 {
		t.Fatalf("checks %d, applies %d: one check, and nothing installs by itself", f.checks, f.applies)
	}
	h.mustHave("Version 0.3.0 is available: Settings > About.")
	if len(f.notified) != 1 || f.notified[0] != "0.3.0" {
		t.Errorf("a notice that was shown is recorded once: %v", f.notified)
	}
	quiet := newer()
	quiet.release.Notify = false // already told today: the About item stays, the notice does not
	h = started(t, guitest.New(), 1280, 720, func(o *Options) { o.Updates = quiet })
	h.mustNotMatch(`is available`)
	if len(quiet.notified) != 0 || h.m.upd.release.Version != "0.3.0" {
		t.Errorf("notified %v, release %+v", quiet.notified, h.m.upd.release)
	}
}

// Nothing installs without a second yes on the pad, given after the download is verified; B and the default
// button change nothing.
func TestAnUpdateInstallsOnlyAfterTheConfirmationOnThePad(t *testing.T) {
	f := newer()
	h := toUpdate(t, f)
	h.mustHave("Update 0.2.0 to 0.3.0?", "Integrity checked by SHA-256 only", "- a [31mthing", "Cancel", "Update")
	h.advance(dialogIgnore)
	h.press(input.Confirm) // the focus starts on Cancel
	if f.installed || h.m.upd.run != nil {
		t.Fatalf("installed %v: Cancel must change nothing", f.installed)
	}
	h.mustHave("Update canceled. Nothing changed.")

	h.press(input.Confirm) // About is still open: Update again
	h.advance(dialogIgnore)
	h.press(input.Right)
	h.press(input.Confirm)
	if !f.installed {
		t.Fatal("the confirmed update did not install")
	}
	h.mustHave("Updated to 0.3.0", "Close and open Mothdex again.", "Quit")
	h.advance(dialogIgnore)
	h.press(input.Confirm) // the only button leaves
	h.advance(exitMinShow)
	if !h.m.Done() {
		t.Fatalf("the confirm button must quit: screen %d", h.m.screen)
	}
}

func TestAFailedUpdateShowsItsCodeAndRef(t *testing.T) {
	f := newer()
	f.failure = &app.Error{Code: "integrity", Message: "bad checksum"}
	h := toUpdate(t, f)
	h.advance(dialogIgnore)
	h.press(input.Right)
	h.press(input.Confirm)
	h.mustHave("Not updated")
	h.mustMatch(`^code: integrity · ref: \w{6}$`)
	h.mustNotMatch(`Close and open`)
}

// A failed update says what to do next: whether the old version is back, the command that installs again when it
// is not, and the updater's own hint when it has one.
func TestAFailedUpdateShowsHowToRecover(t *testing.T) {
	const command = "curl -fsSL https://example.test/install.sh | sh"
	for _, c := range []struct {
		name, code string
		args       map[string]any
		want, not  string
	}{
		{"not restored", "update_failed", map[string]any{"restored": false, "install_command": command}, "Install again from a terminal: " + command, "previous version is back"},
		{"restored", "update_failed", map[string]any{"restored": true, "install_command": command}, "The previous version is back.", command},
		{"signing root", "integrity", map[string]any{"hint_key": "update.verification_hint", "install_command": command}, "reinstall with the official command: " + command, "previous version is back"},
	} {
		f := newer()
		f.failure = &app.Error{Code: c.code, Message: c.name, Args: c.args}
		h := toUpdate(t, f)
		h.advance(dialogIgnore)
		h.press(input.Right)
		h.press(input.Confirm)
		shown := strings.Join(h.texts(), " ") // a wrapped line reads as one again
		if !strings.Contains(shown, c.want) || strings.Contains(shown, c.not) {
			t.Errorf("%s: want %q and not %q in %q", c.name, c.want, c.not, shown)
		}
	}
}

// Without an Updates (the check is off, or the installation is not managed) there is no check and no action.
func TestNoUpdatesNoCheckNoAction(t *testing.T) {
	h := openSettingsFromHome(t, settingsFixture(), 1280, 720)
	openAbout(t, h)
	if strings.Contains(strings.Join(h.texts(), " "), "Update") || len(h.m.infoButtons("about")) != 0 {
		t.Errorf("an update action without Updates: %q", h.texts())
	}
}

// Before the yes, B and SIGTERM stop the download; the goroutine of Apply ends.
func TestAnUnconfirmedUpdateStopsOnSignal(t *testing.T) {
	f := newer()
	h := toUpdate(t, f)
	run := h.m.upd.run
	h.hold = true
	h.send(SignalMsg{})
	if run == nil || h.m.screen != screenExit {
		t.Fatalf("screen %d", h.m.screen)
	}
	if msg, ok := (<-run.events).(updateDoneMsg); !ok || f.installed {
		t.Fatalf("Apply did not end unchanged: %#v", msg)
	}
}
