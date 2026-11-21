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
