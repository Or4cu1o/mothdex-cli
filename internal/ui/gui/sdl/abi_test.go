//go:build gui

package sdl

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// The mirrored structs must match the C layout of the SDL2 headers on every GOARCH: none of them holds a
// pointer or a long, so the numbers are the same on all targets. The CI runs this test once per
// architecture (qemu for arm64 and armv7), which is what "per GOARCH" means in the contract.
func TestStructLayoutMatchesTheSDL2Headers(t *testing.T) {
	var r Rect
	var v Version
	var b ControllerButtonEvent
	var e Event
	var w WindowEvent
	var k KeyboardEvent
	var a ControllerAxisEvent
	var d ControllerDeviceEvent
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"sizeof(SDL_version)", unsafe.Sizeof(v), 3},
		{"sizeof(SDL_Rect)", unsafe.Sizeof(r), 16},
		{"SDL_Rect.x", unsafe.Offsetof(r.X), 0},
		{"SDL_Rect.y", unsafe.Offsetof(r.Y), 4},
		{"SDL_Rect.w", unsafe.Offsetof(r.W), 8},
		{"SDL_Rect.h", unsafe.Offsetof(r.H), 12},
		{"sizeof(SDL_Event)", unsafe.Sizeof(e), 56},
		{"sizeof(SDL_ControllerButtonEvent)", unsafe.Sizeof(b), 16},
		{"SDL_ControllerButtonEvent.type", unsafe.Offsetof(b.Type), 0},
		{"SDL_ControllerButtonEvent.timestamp", unsafe.Offsetof(b.Timestamp), 4},
		{"SDL_ControllerButtonEvent.which", unsafe.Offsetof(b.Which), 8},
		{"SDL_ControllerButtonEvent.button", unsafe.Offsetof(b.Button), 12},
		{"SDL_ControllerButtonEvent.state", unsafe.Offsetof(b.State), 13},
		{"sizeof(SDL_WindowEvent)", unsafe.Sizeof(w), 24},
		{"SDL_WindowEvent.windowID", unsafe.Offsetof(w.WindowID), 8},
		{"SDL_WindowEvent.event", unsafe.Offsetof(w.Event), 12},
		{"SDL_WindowEvent.data1", unsafe.Offsetof(w.Data1), 16},
		{"SDL_WindowEvent.data2", unsafe.Offsetof(w.Data2), 20},
		{"sizeof(SDL_KeyboardEvent)", unsafe.Sizeof(k), 32},
		{"SDL_KeyboardEvent.windowID", unsafe.Offsetof(k.WindowID), 8},
		{"SDL_KeyboardEvent.state", unsafe.Offsetof(k.State), 12},
		{"SDL_KeyboardEvent.repeat", unsafe.Offsetof(k.Repeat), 13},
		{"SDL_Keysym.scancode", unsafe.Offsetof(k.Scancode), 16},
		{"SDL_Keysym.sym", unsafe.Offsetof(k.Sym), 20},
		{"SDL_Keysym.mod", unsafe.Offsetof(k.Mod), 24},
		{"SDL_Keysym.unused", unsafe.Offsetof(k.Unused), 28},
		{"sizeof(SDL_ControllerAxisEvent)", unsafe.Sizeof(a), 20},
		{"SDL_ControllerAxisEvent.which", unsafe.Offsetof(a.Which), 8},
		{"SDL_ControllerAxisEvent.axis", unsafe.Offsetof(a.Axis), 12},
		{"SDL_ControllerAxisEvent.value", unsafe.Offsetof(a.Value), 16},
		{"sizeof(SDL_ControllerDeviceEvent)", unsafe.Sizeof(d), 12},
		{"SDL_ControllerDeviceEvent.which", unsafe.Offsetof(d.Which), 8},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s is %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestAnEventIsEightByteAligned(t *testing.T) {
	// SDL_Event holds 64-bit members, and ARM EABI needs 8 bytes of alignment for them: NewEvent must
	// return memory aligned that way (a heap object of this size is).
	for i := 0; i < 64; i++ {
		if p := uintptr(unsafe.Pointer(NewEvent())); p%8 != 0 { //nolint:gosec // only the address is looked at
			t.Fatalf("event %d at %#x is not 8-byte aligned", i, p)
		}
	}
}

func TestEventFieldsRoundTripThroughTheRawBytes(t *testing.T) {
	e := NewEvent()
	e.SetControllerButton(EventControllerButtonDown, -3, 9, 1)
	got := e.ControllerButton()
	if e.Type() != EventControllerButtonDown || got.Which != -3 || got.Button != 9 || got.State != 1 {
		t.Fatalf("type %#x, %+v", e.Type(), *got)
	}
	e.SetControllerButton(EventControllerButtonDown, 1, 2, 3) // a refill must not keep old bytes
	if got.Which != 1 || got.Button != 2 || got.State != 3 {
		t.Fatalf("after refill %+v", *got)
	}
}

func TestVersionComparison(t *testing.T) {
	v := func(a, b, c uint8) Version { return Version{a, b, c} }
	tests := []struct {
		have, need Version
		want       bool
	}{
		{v(2, 32, 4), v(2, 0, 18), true},
		{v(2, 0, 18), v(2, 0, 18), true},
		{v(2, 0, 17), v(2, 0, 18), false},
		{v(2, 0, 20), v(2, 0, 18), true},
		{v(2, 30, 0), v(2, 0, 18), true},
		{v(1, 99, 99), v(2, 0, 0), false},
		{v(3, 0, 0), v(2, 32, 9), true},
	}
	for _, tc := range tests {
		if got := tc.have.AtLeast(tc.need); got != tc.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", tc.have, tc.need, got, tc.want)
		}
	}
	if (Version{2, 0, 18}).String() != "2.0.18" {
		t.Fatal("String")
	}
}

// Every function field of api needs an entry in the table, or it stays nil and the first call panics.
func TestEveryFunctionOfTheBindingIsInTheTable(t *testing.T) {
	var a api
	fields := reflect.TypeOf(a).NumField()
	table := a.table()
	if len(table) != fields {
		t.Fatalf("api has %d functions and the table lists %d", fields, len(table))
	}
	seen := map[string]bool{}
	for _, b := range table {
		if !strings.HasPrefix(b.name, "SDL_") || seen[b.name] {
			t.Errorf("bad or repeated name %q", b.name)
		}
		seen[b.name] = true
		if reflect.TypeOf(b.fn).Kind() != reflect.Pointer || reflect.TypeOf(b.fn).Elem().Kind() != reflect.Func {
			t.Errorf("%s: fn must be a pointer to a Go function", b.name)
		}
	}
	if len(Functions()) != len(table) {
		t.Fatal("Functions() and the table disagree")
	}
}

func TestTheMinimumVersionIsWithinTheTarget(t *testing.T) {
	if !TargetVersion.AtLeast(MinimumVersion()) {
		t.Fatalf("the functions need SDL %s, newer than the target %s", MinimumVersion(), TargetVersion)
	}
}

// SDL reads or writes pitch*(h-1)+w*bytes bytes through the pointer it is given, so a shorter slice would
// corrupt or over-read the Go heap. checkBuffer refuses it before SDL is called (PR-69 review M1).
func TestCheckBufferRefusesWhatSDLWouldOverrun(t *testing.T) {
	tests := []struct {
		name          string
		w, h, pitch   int32
		bpp           int32
		have          int
		wantErrSubstr string // empty: accepted
	}{
		{"exact size", 64, 48, 256, 4, 64 * 48 * 4, ""},
		{"the last row needs no padding", 10, 3, 64, 4, 64*2 + 10*4, ""},
		{"one byte short", 64, 48, 256, 4, 64*48*4 - 1, "too small"},
		{"short by a row", 64, 48, 256, 4, 64 * 47 * 4, "too small"},
		{"a one-byte buffer for a block", 64, 48, 256, 4, 1, "too small"},
		{"an empty buffer", 4, 4, 16, 4, 0, "too small"},
		{"pitch smaller than a row", 64, 48, 255, 4, 1 << 20, "pitch"},
		{"zero width", 0, 48, 256, 4, 1 << 20, "empty"},
		{"zero height", 64, 0, 256, 4, 1 << 20, "empty"},
		{"negative width", -1, 48, 256, 4, 1 << 20, "empty"},
		{"unknown pixel size", 64, 48, 256, 0, 1 << 20, "pixel size"},
		// int32 inputs whose product overflows 32 bits: the check must still say "too small"
		{"huge block, small buffer", 1 << 30, 1 << 30, 1 << 30, 4, 1 << 20, "pitch"},
		{"huge height, small buffer", 4, 1 << 30, 16, 4, 1 << 20, "too small"},
		{"max pitch and height", 4, 1<<31 - 1, 1<<31 - 1, 4, 1 << 20, "too small"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkBuffer(tc.w, tc.h, tc.pitch, tc.bpp, tc.have)
			switch {
			case tc.wantErrSubstr == "" && err != nil:
				t.Fatalf("refused a good buffer: %v", err)
			case tc.wantErrSubstr != "" && err == nil:
				t.Fatalf("accepted a buffer SDL would overrun (%d bytes for %dx%d pitch %d)", tc.have, tc.w, tc.h, tc.pitch)
			case tc.wantErrSubstr != "" && !strings.Contains(err.Error(), tc.wantErrSubstr):
				t.Fatalf("error %q does not mention %q", err, tc.wantErrSubstr)
			}
		})
	}
}

func TestBytesPerPixelOfTheFormatsTheBindingUses(t *testing.T) {
	tests := map[string]struct {
		format uint32
		want   int32
	}{
		"ARGB8888, 4 bytes": {PixelFormatARGB8888, 4},
		"ABGR8888, 4 bytes": {0x16762004, 4},
		"RGB565, 2 bytes":   {0x15151002, 2},
		"no format":         {0, 0},
		"a FourCC format (YV12) has no pixel size": {0x32315659, 0},
	}
	for name, tc := range tests {
		if got := bytesPerPixel(tc.format); got != tc.want {
			t.Errorf("%s: bytesPerPixel(%#x) = %d, want %d", name, tc.format, got, tc.want)
		}
	}
}

// bind registers SDL_GetVersion first and reads the version before the rest, so it must lead the table.
func TestTheTableStartsWithGetVersion(t *testing.T) {
	var a api
	if got := a.table()[0].name; got != "SDL_GetVersion" {
		t.Fatalf("the first function is %s, bind needs SDL_GetVersion", got)
	}
}

// An optional function (newer than the minimum) must not raise the minimum version, and only optional
// functions may be newer than it.
func TestOnlyOptionalFunctionsAreNewerThanTheMinimum(t *testing.T) {
	var a api
	min := MinimumVersion()
	optionalSeen := false
	for _, b := range a.table() {
		if b.since.AtLeast(Version{2, 0, 1}) && !b.optional {
			t.Errorf("%s needs SDL %s and is not optional, which would raise the minimum", b.name, b.since)
		}
		optionalSeen = optionalSeen || b.optional
		if !b.optional && !min.AtLeast(b.since) {
			t.Errorf("minimum %s is older than the required %s (%s)", min, b.name, b.since)
		}
	}
	if !optionalSeen {
		t.Error("SDL_GameControllerGetType (2.0.12) must be optional")
	}
}

func TestNewEventViewsRoundTrip(t *testing.T) {
	e := NewEvent()
	*e.Keyboard() = KeyboardEvent{Type: EventKeyDown, Repeat: 1, Sym: KeycodeEscape}
	if e.Type() != EventKeyDown || e.Keyboard().Sym != KeycodeEscape || e.Keyboard().Repeat != 1 {
		t.Fatalf("%+v", *e.Keyboard())
	}
	*e.ControllerAxis() = ControllerAxisEvent{Type: EventControllerAxisMotion, Which: 3, Axis: ControllerAxisLeftY, Value: -32768}
	if got := e.ControllerAxis(); got.Value != -32768 || got.Which != 3 || got.Axis != ControllerAxisLeftY {
		t.Fatalf("%+v", *got)
	}
	e.SetType(0x8001)
	if e.Type() != 0x8001 || e.Keyboard().Sym != 0 {
		t.Fatal("SetType must clear the rest of the event")
	}
}
