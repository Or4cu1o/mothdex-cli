package gui

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

func (h *harness) mustMatch(pattern string) []string {
	h.t.Helper()
	re := regexp.MustCompile(pattern)
	for _, text := range h.texts() {
		if found := re.FindStringSubmatch(text); found != nil {
			return found
		}
	}
	h.t.Errorf("no text run matches %q in %q", pattern, h.texts())
	return nil
}

func (h *harness) mustNotMatch(pattern string) {
	h.t.Helper()
	re := regexp.MustCompile(pattern)
	if i := slices.IndexFunc(h.texts(), re.MatchString); i >= 0 {
		h.t.Errorf("unexpected text run %q (matches %q)", h.texts()[i], pattern)
	}
}

const testLogPath = "~/.local/state/mothdex/logs/latest.log"

// support wires the log and the export the composition root gives the model.
type supportFake struct {
	log     bytes.Buffer
	exports int
	err     error
}

func (f *supportFake) options(o *Options) {
	o.Log = slog.New(slog.NewTextHandler(&f.log, nil))
	o.LogPath = func() string { return testLogPath }
	o.ExportLogs = func(context.Context) (string, error) {
		f.exports++
		return "~/logs/bundles/mothdex-logs-20261126-030000.zip", f.err
	}
}

// What the screen shows is what finds the line in the log (logs.md section 5): the same ref, and one line only.
func TestAnErrorInPlaceShowsTheRefThatIsInTheLogAndTheLog(t *testing.T) {
	var f supportFake
	svc := zeldaService(1)
	svc.PlatformErr = &app.Error{Code: app.CodeNetwork, Message: "down"}
	h := started(t, svc, 1280, 720, f.options)
	h.press(input.Confirm)
	h.press(input.Up)
	h.press(input.Up)
	h.press(input.Confirm)
	found := h.mustMatch(`^code: network · ref: ([0-9ABCDEFGHJKMNPQRSTVWXYZ]{6})$`)
	h.mustHave("log: " + testLogPath)
	if found == nil {
		return
	}
	h.advance(0) // drawing again records nothing new
	if n := strings.Count(f.log.String(), "ref="+found[1]+" code=network cmd=platforms"); n != 1 || strings.Count(f.log.String(), "level=ERROR") != 1 {
		t.Errorf("the log must hold one line with the ref %s:\n%s", found[1], f.log.String())
	}
}

func TestEachFailedDownloadGetsItsOwnRefShownInDetails(t *testing.T) {
	var f supportFake
	h := started(t, zeldaService(1), 1280, 720, f.options)
	for _, id := range []app.JobID{"1", "2"} {
		h.send(JobMsg{Job: app.JobSnapshot{ID: id, Title: "Game " + string(id), State: app.StateFailed, Err: &app.Error{Code: app.CodeNetwork, Message: "down"}}})
	}
	refs := regexp.MustCompile(`ref=(\w{6}) code=network cmd=download`).FindAllStringSubmatch(f.log.String(), -1)
	if len(refs) != 2 || refs[0][1] == refs[1][1] {
		t.Fatalf("two failures need two refs: %v\n%s", refs, f.log.String())
	}
	h.press(input.Queue)
	h.press(input.Info)
	shown := strings.Join(h.texts(), " ")
	if !strings.Contains(shown, refs[0][1]) && !strings.Contains(shown, refs[1][1]) {
		t.Errorf("Details shows no ref of %v: %q", refs, shown)
	}
}

// Settings › About › Export logs, by pad only: a confirmation whose safe button is Cancel, then where the zip is.
func TestExportLogsFromAboutAsksFirstAndSaysWhereTheZipIs(t *testing.T) {
	var f supportFake
	h := openSettingsFromHome(t, settingsFixture(), 1280, 720)
	f.options(&h.m.o)
	openAbout(t, h)
	h.mustHave("Export logs", "Log file: "+testLogPath)
	h.press(input.Confirm)
	h.mustHave("Export the logs?", "Cancel")
	h.advance(dialogIgnore)
	h.press(input.Confirm) // the focus starts on Cancel
	if f.exports != 0 || h.m.overlay != overlayNone {
		t.Fatalf("Cancel exported %d times, overlay %d", f.exports, h.m.overlay)
	}
	h.press(input.Confirm)
	h.advance(dialogIgnore)
	h.press(input.Right)
	h.press(input.Confirm)
	if f.exports != 1 {
		t.Fatalf("%d exports", f.exports)
	}
	h.mustHave("Logs saved", "OK")
	h.mustMatch(`mothdex-logs-20261126-030000\.zip`)
}

func TestAFailedExportShowsItsOwnRef(t *testing.T) {
	f := supportFake{err: errors.New("disk is full")}
	h := openSettingsFromHome(t, settingsFixture(), 1280, 720)
	f.options(&h.m.o)
	openAbout(t, h)
	h.press(input.Confirm)
	h.advance(dialogIgnore)
	h.press(input.Right)
	h.press(input.Confirm)
	h.mustHave("The logs were not saved")
	found := h.mustMatch(`^internal · (\w{6}) · `)
	if found != nil && !strings.Contains(f.log.String(), "ref="+found[1]) {
		t.Errorf("the ref %s is not in the log:\n%s", found[1], f.log.String())
	}
}

// The crash screen offers exactly two things, by pad, and calls no service: whatever broke is left alone.
func TestTheCrashScreenExportsTheLogsAndQuitsWithoutTheService(t *testing.T) {
	var f supportFake
	h := newHarness(t, zeldaService(1), 640, 480, f.options)
	o := h.m.o
	settings, err := o.Service.ConfigList()
	if err != nil {
		t.Fatal(err)
	}
	o.Service = nil // a call would panic
	h.m = NewCrash(o, settings, "7F3K2Q", h.now)
	h.mustHave("Something went wrong", "code: internal · ref: 7F3K2Q", "Export logs", "Quit")
	h.mustMatch(`^log: `)
	h.send(JobMsg{Job: app.JobSnapshot{ID: "1", State: app.StateDone}}) // nothing else moves it
	h.press(input.Confirm)                                              // the focus starts on Export logs
	h.advance(dialogIgnore)
	h.press(input.Right)
	h.press(input.Confirm)
	if f.exports != 1 {
		t.Fatalf("%d exports", f.exports)
	}
	h.mustHave("Logs saved")
	h.advance(dialogIgnore)
	h.press(input.Confirm) // OK
	if h.m.Done() {
		t.Fatal("OK closes the dialog, not the program")
	}
	h.press(input.Right)
	h.press(input.Confirm)
	if !h.m.Done() {
		t.Fatal("Quit ends the program")
	}
}

func TestTheCrashScreenLeavesOnSignalAndOnBack(t *testing.T) {
	for name, msg := range map[string]Msg{"SIGTERM": SignalMsg{}, "B": InputMsg{Action: input.Back}, "window close": QuitRequestMsg{}} {
		h := newHarness(t, zeldaService(1), 640, 480)
		h.m = NewCrash(h.m.o, nil, "7F3K2Q", h.now)
		h.send(msg)
		if !h.m.Done() {
			t.Errorf("%s does not leave the crash screen", name)
		}
	}
}

// Leaving while the bundle is being written would lose it without a word: the crash screen waits, but a signal from
// the frontend does not.
func TestTheCrashScreenDoesNotQuitInTheMiddleOfAnExport(t *testing.T) {
	var f supportFake
	h := newHarness(t, zeldaService(1), 640, 480, f.options)
	h.m = NewCrash(h.m.o, nil, "7F3K2Q", h.now)
	h.press(input.Confirm)
	h.advance(dialogIgnore)
	h.hold = true // the export has started and not ended
	h.press(input.Right)
	h.press(input.Confirm)
	h.press(input.Back)
	if h.m.Done() {
		t.Fatal("B left during the export")
	}
	h.hold = false
	h.run(h.held)
	h.advance(dialogIgnore)
	h.press(input.Confirm) // OK
	h.press(input.Back)
	if !h.m.Done() {
		t.Fatal("B leaves once the export ended")
	}
}

// With something else open, the toast still says what happened, not only a path.
func TestAnExportThatEndsUnderAnOverlayKeepsItsTitle(t *testing.T) {
	var f supportFake
	h := openSettingsFromHome(t, settingsFixture(), 1280, 720)
	f.options(&h.m.o)
	openAbout(t, h)
	h.press(input.Confirm)
	h.advance(dialogIgnore)
	h.hold = true
	h.press(input.Right)
	h.press(input.Confirm)
	h.press(input.Back) // leaves About
	h.press(input.Menu)
	h.hold = false
	h.run(h.held)
	h.mustMatch(`Logs saved: `)
}

func openAbout(t *testing.T, h *harness) {
	t.Helper()
	for i := 0; i < 20 && h.m.settings.focus != "about"; i++ {
		h.press(input.Down)
	}
	h.press(input.Confirm)
	if h.m.settings.ed == nil || h.m.settings.ed.row != "about" {
		t.Fatalf("About did not open: %+v", h.m.settings)
	}
}
