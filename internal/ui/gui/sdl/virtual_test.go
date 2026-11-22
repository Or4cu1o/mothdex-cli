//go:build gui

package sdl

import (
	"testing"
	"time"

	"github.com/ebitengine/purego"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
)

// A virtual joystick (SDL 2.0.14) goes through the real backend and the real SDL: its GUID is read with the real call,
// the joystick is found unmapped, attached and detached events give the list, a duplicate event or an unknown instance
// id changes nothing. This is what catches an ABI problem in the GUID call, which a hand-built map cannot (Crivo, T-80:
// a struct with an array field made purego panic as soon as any joystick was connected).
func TestVirtualJoystickIsSeenThroughTheRealBackend(t *testing.T) {
	be, _ := headless(t, 640, 480)
	handle, err := openLibrary()
	if err != nil {
		t.Fatal(err)
	}
	var (
		attach     func(typ, axes, buttons, hats int32) int32
		detach     func(index int32) int32
		guidString func(g JoystickGUID, buf *byte, size int32)
	)
	bound := func() (ok bool) {
		defer func() { ok = ok && recover() == nil }()
		ok = true
		purego.RegisterLibFunc(&attach, handle, "SDL_JoystickAttachVirtual")
		purego.RegisterLibFunc(&detach, handle, "SDL_JoystickDetachVirtual")
		return ok
	}()
	if !bound {
		t.Skip("this SDL2 has no virtual joysticks (before 2.0.14)")
	}
	if structsByValue() {
		purego.RegisterLibFunc(&guidString, handle, "SDL_JoystickGetGUIDString")
	}

	// waitPads reads events until the backend reports a list with n joysticks.
	waitPads := func(n int) []input.PadInfo {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			ev, ok := be.Wait(100 * time.Millisecond)
			if ok && ev.Kind == input.IncomingPadList && len(ev.Pads) == n {
				return ev.Pads
			}
		}
		t.Fatalf("no list of %d joysticks came back", n)
		return nil
	}
	waitPads(0) // the start-up list is reported even when empty

	idx := attach(0, 2, 4, 0) // SDL_JOYSTICK_TYPE_UNKNOWN: SDL has no game controller mapping for it
	if idx < 0 {
		t.Skipf("SDL could not attach a virtual joystick: %s", be.lib.a.getError())
	}
	pads := waitPads(1)
	if pads[0].Mapped {
		t.Fatalf("a joystick with no mapping reported as mapped: %+v", pads[0])
	}

	// The GUID call: no panic, and the hex form equals what SDL itself writes.
	guid, ok := be.lib.JoystickGUID(int(idx))
	if structsByValue() {
		if !ok || len(guid) != 32 {
			t.Fatalf("GUID %q, ok %v", guid, ok)
		}
		var buf [33]byte
		guidString(be.lib.a.joystickDeviceGUID(idx), &buf[0], int32(len(buf)))
		if want := string(buf[:32]); guid != want || pads[0].GUID != want {
			t.Fatalf("GUID %q, SDL writes %q, the list has %q", guid, want, pads[0].GUID)
		}
	} else if ok {
		t.Fatalf("the GUID is not available here, got %q", guid)
	}

	be.addUnmapped(int(idx)) // the same device announced twice: still one
	if len(be.joys) != 1 {
		t.Fatalf("joys %v", be.joys)
	}
	be.removeJoystick(9999) // an instance id nobody has: nothing changes
	if len(be.joys) != 1 {
		t.Fatalf("joys %v", be.joys)
	}

	detach(idx)
	waitPads(0)
}
