//go:build gui

package sdl

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// The GUID is written the way SDL_JoystickGetGUIDString does and gamecontrollerdb.txt lines start: 32 lower-case hex digits.
func TestJoystickGUIDIsTheHexFormOfItsBytes(t *testing.T) {
	g := JoystickGUID{Lo: 0x0000045e00000003, Hi: 0x000005140000028e} // the bytes 03 00 00 00 5e 04 00 00 8e 02 00 00 14 05 00 00
	if got, want := g.String(), "030000005e0400008e02000014050000"; got != want {
		t.Fatalf("%s, want %s", got, want)
	}
}

// The list of joysticks goes to the loop in the order SDL numbered them, mapped and not, and only the latest list that
// the loop has not read yet stays queued.
func TestReportPadsQueuesOnlyTheLatestListAndKeepsOtherEvents(t *testing.T) {
	b := &Backend{joys: map[int32]padRecord{}}
	b.pending = []input.Incoming{{Kind: input.IncomingRedraw}}
	b.joys[7] = padRecord{name: "Unknown Pad", guid: "0300aa"}
	b.reportPads()
	b.joys[3] = padRecord{name: "Xbox", guid: "0300bb", typ: input.TypeXbox360, mapped: true}
	b.pending = append(b.pending, input.Incoming{Kind: input.IncomingQuit})
	b.reportPads()

	if len(b.pending) != 3 || b.pending[0].Kind != input.IncomingRedraw || b.pending[1].Kind != input.IncomingQuit || b.pending[2].Kind != input.IncomingPadList {
		t.Fatalf("pending %+v", b.pending)
	}
	pads := b.pending[2].Pads
	if len(pads) != 2 || pads[0].Name != "Xbox" || !pads[0].Mapped || pads[0].Type != input.TypeXbox360 || pads[1].Name != "Unknown Pad" || pads[1].Mapped {
		t.Fatalf("pads %+v", pads)
	}
	b.joys = map[int32]padRecord{}
	b.reportPads()
	if last := b.pending[len(b.pending)-1]; last.Kind != input.IncomingPadList || len(last.Pads) != 0 {
		t.Fatalf("an empty list is still reported, so the GUI knows no pad is there: %+v", last)
	}
}

// An unplugged joystick without a mapping is forgotten; a mapped one is left to its controller event.
func TestRemoveJoystickForgetsOnlyTheUnmappedOnes(t *testing.T) {
	b := &Backend{joys: map[int32]padRecord{1: {name: "Mapped", mapped: true}, 2: {name: "Unmapped"}}}
	b.removeJoystick(1)
	if _, ok := b.joys[1]; !ok {
		t.Fatal("a mapped pad is removed by its controller event")
	}
	b.removeJoystick(2)
	if _, ok := b.joys[2]; ok || len(b.pending) != 1 {
		t.Fatalf("joys %v pending %+v", b.joys, b.pending)
	}
}
