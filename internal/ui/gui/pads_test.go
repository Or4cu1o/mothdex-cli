package gui

import (
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/guitest"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

var (
	xbox    = input.PadInfo{Name: "Xbox 360 Controller", GUID: "030000005e0400008e02000014050000", Type: input.TypeXbox360, Mapped: true}
	unknown = input.PadInfo{Name: "Mystery Gamepad", GUID: "03000000aaaabbbb0000000000000000"}
)

// docs/ux/gui.md 2.3: with no pad a warn toast says so once and the top bar keeps a badge while it lasts; a pad that
// connects takes the badge away, and unplugging the last one brings the toast and the badge back.
func TestNoControllerGivesAToastOnceAndABadgeWhileItLasts(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.mustNotHave("No controller") // the backend has not reported yet
	h.send(PadsMsg{})
	h.mustHave("Connect a controller. On a keyboard: arrows, Enter, Esc.", "No controller")
	h.advance(7 * time.Second)
	h.mustHave("No controller")
	h.mustNotHave("Connect a controller. On a keyboard: arrows, Enter, Esc.")
	h.send(PadsMsg{}) // the same state again: no second toast
	h.mustNotHave("Connect a controller. On a keyboard: arrows, Enter, Esc.")

	h.send(PadsMsg{Pads: []input.PadInfo{xbox}})
	h.mustNotHave("No controller")
	h.send(PadsMsg{}) // the last pad is unplugged
	h.mustHave("Connect a controller. On a keyboard: arrows, Enter, Esc.", "No controller")
}

// A joystick SDL has no mapping for is announced once per device per session, and points to About.
func TestUnmappedJoystickIsAnnouncedOncePerDevice(t *testing.T) {
	h := started(t, guitest.New(), 1280, 720)
	h.send(PadsMsg{Pads: []input.PadInfo{xbox, unknown}})
	h.mustHave("Controller not recognized: Mystery Gamepad. See Settings › About.")
	h.mustNotHave("No controller") // a pad that works is there
	h.advance(7 * time.Second)
	h.send(PadsMsg{Pads: []input.PadInfo{xbox}}) // unplugged
	h.send(PadsMsg{Pads: []input.PadInfo{xbox, unknown}})
	h.mustNotHave("Controller not recognized: Mystery Gamepad. See Settings › About.") // not on every reconnect
	other := unknown
	other.Name, other.GUID = "Another Pad", "03000000ccccdddd0000000000000000"
	h.send(PadsMsg{Pads: []input.PadInfo{xbox, other}})
	h.mustHave("Controller not recognized: Another Pad. See Settings › About.")
}

// Toasts show one at a time: a notice that comes while another is on screen waits for it, and a long device name is
// the only thing cut, never the sentence.
func TestPadNoticesWaitForTheToastOnScreenAndCutOnlyTheName(t *testing.T) {
	long := input.PadInfo{Name: strings.Repeat("Very Long Controller Name ", 12), GUID: "03000000"}
	h := started(t, guitest.New(), 640, 480)
	h.send(PadsMsg{Pads: []input.PadInfo{long}}) // no pad that works, and an unknown joystick: two notices
	if !strings.Contains(strings.Join(h.texts(), " "), "Connect a controller. On a keyboard: arrows, Enter, Esc.") { // wrapped at 640
		t.Fatalf("the first notice is on screen: %v", h.texts())
	}
	h.mustNotHave("See Settings › About.")
	h.advance(7 * time.Second)
	got := strings.Join(h.texts(), " ")
	if !strings.Contains(got, "…") || !strings.Contains(got, ". See Settings › About.") || strings.Contains(got, "Connect a controller") {
		t.Fatalf("the second notice follows the first, with only the name cut:\n%s", got)
	}
}

// A report that arrives while the Splash is showing waits for Home.
func TestPadNoticeWaitsForHomeAfterTheSplash(t *testing.T) {
	h := newHarness(t, guitest.New(), 1280, 720)
	h.send(PadsMsg{})
	h.mustNotHave("Connect a controller. On a keyboard: arrows, Enter, Esc.")
	h.run(h.pending)
	h.pending = nil
	h.mustHave("Connect a controller. On a keyboard: arrows, Enter, Esc.")
}

// Settings › About lists every joystick with its GUID, says where to add a mapping for one SDL does not know, and says
// when the GUID cannot be had on this platform.
func TestAboutListsTheControllersAndWhereToAddAMapping(t *testing.T) {
	svc := settingsFixture()
	h := started(t, svc, 1280, 720, func(o *Options) { o.MappingsPath = "/userdata/system/mothdex/data/gamecontrollerdb.txt" })
	openAbout := func() {
		h.press(input.Menu)
		for i := 0; i < 3; i++ {
			h.press(input.Down)
		}
		h.press(input.Confirm)
		for i := 0; i < 12; i++ {
			h.press(input.Down)
		}
		h.press(input.Confirm)
	}
	openAbout()
	h.mustHave("Controller: None connected")
	h.press(input.Back)
	noGUID := unknown
	noGUID.GUID = ""
	h.send(PadsMsg{Pads: []input.PadInfo{xbox, noGUID}})
	h.press(input.Confirm)
	h.mustHave("Controller: Xbox 360 Controller", "GUID: 030000005e0400008e02000014050000",
		"Controller: Mystery Gamepad", "GUID: Unavailable on this system")
	joined := strings.Join(h.texts(), "")
	if !strings.Contains(strings.ReplaceAll(joined, " ", ""), "Notrecognized.Addamappingto/userdata/system/mothdex/data/gamecontrollerdb.txt.") {
		t.Fatalf("the whole path, wrapped but not shortened:\n%v", h.texts())
	}
}
