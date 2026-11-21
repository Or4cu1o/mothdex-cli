package input

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func at(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }

func press(b Button) Event   { return Event{Kind: ButtonPress, Button: b} }
func release(b Button) Event { return Event{Kind: ButtonRelease, Button: b} }
func axis(a Axis, v int) Event {
	return Event{Kind: AxisMotion, Axis: a, Value: v}
}
func key(k KeyCode) Event       { return Event{Kind: KeyPress, Key: k} }
func letter(r rune) Event       { return Event{Kind: KeyPress, Key: KeyRune, Rune: r} }
func acts(a ...Action) []Action { return a }

// Buttons are read by position, and the swap changes A and B only (docs/ux/gui.md section 2.1).
func TestButtonsByPositionAndTheConfirmSwap(t *testing.T) {
	tests := []struct {
		btn         Button
		confirmEast bool
		want        Action
	}{
		{South, false, Confirm}, {East, false, Back},
		{South, true, Back}, {East, true, Confirm},
		{West, false, Download}, {West, true, Download},
		{North, false, Info}, {North, true, Info},
		{LeftShoulder, false, PageUp}, {RightShoulder, true, PageDown},
		{Start, false, Menu}, {Start, true, Menu},
		{DPadUp, false, Up}, {DPadDown, false, Down}, {DPadLeft, true, Left}, {DPadRight, true, Right},
	}
	for _, tc := range tests {
		m := NewMapper(Config{ConfirmEast: tc.confirmEast})
		if got := m.Handle(press(tc.btn), t0); !reflect.DeepEqual(got, acts(tc.want)) {
			t.Errorf("button %d (confirmEast=%v) gave %v, want %v", tc.btn, tc.confirmEast, got, tc.want)
		}
	}
}

func TestChangingTheConfigurationAppliesAtOnce(t *testing.T) {
	m := NewMapper(Config{})
	m.SetConfig(Config{ConfirmEast: true})
	if got := m.Handle(press(East), t0); !reflect.DeepEqual(got, acts(Confirm)) {
		t.Fatalf("got %v", got)
	}
}

// Holding a direction repeats it: first after 400 ms, then every 80 ms, then every 40 ms after 2 s.
func TestHeldDirectionRepeatTiming(t *testing.T) {
	m := NewMapper(Config{})
	if got := m.Handle(press(DPadDown), t0); !reflect.DeepEqual(got, acts(Down)) {
		t.Fatalf("first press gave %v", got)
	}
	steps := []struct {
		ms   int
		want int // actions due at this time
	}{
		{399, 0}, {400, 1}, {479, 0}, {480, 1}, {560, 1}, {1000, 4}, // a late frame catches up, bounded
	}
	for _, s := range steps {
		if got := m.Tick(at(s.ms)); len(got) != s.want {
			t.Fatalf("at %d ms %d repeats, want %d", s.ms, len(got), s.want)
		}
	}
}

func TestRepeatGoesFasterAfterTwoSeconds(t *testing.T) {
	m := NewMapper(Config{})
	m.Handle(press(DPadDown), t0)
	count := func(from, to, step int) int {
		n := 0
		for ms := from; ms <= to; ms += step {
			n += len(m.Tick(at(ms)))
		}
		return n
	}
	count(0, 2000, 20)
	if early := count(2020, 2400, 20); early < 9 { // 380 ms at 40 ms apart is nine or ten repeats
		t.Fatalf("after 2 s only %d repeats in 380 ms, want about 9 at 40 ms", early)
	}
}

func TestReleasingStopsTheRepeatAndAnotherButtonDoesNot(t *testing.T) {
	m := NewMapper(Config{})
	m.Handle(press(DPadDown), t0)
	m.Handle(press(South), at(100))
	m.Handle(release(South), at(150))
	if got := m.Tick(at(450)); len(got) != 1 {
		t.Fatalf("releasing A must not stop the held direction: %v", got)
	}
	m.Handle(release(DPadDown), at(520))
	if got := m.Tick(at(2000)); len(got) != 0 {
		t.Fatalf("a released direction repeated: %v", got)
	}
	if _, ok := m.Deadline(); ok {
		t.Fatal("no deadline once nothing is held")
	}
}

func TestOnlyDirectionsAndPagesRepeatUnlessBackIsAsked(t *testing.T) {
	m := NewMapper(Config{})
	for _, b := range []Button{South, East, West, North, Start} {
		m.Handle(press(b), t0)
		if got := m.Tick(at(5000)); len(got) != 0 {
			t.Errorf("button %d repeated: %v", b, got)
		}
		m.Handle(release(b), at(5000))
	}
	m.Handle(press(LeftShoulder), at(6000))
	if got := m.Tick(at(6400)); !reflect.DeepEqual(got, acts(PageUp)) {
		t.Fatalf("L must repeat like the D-pad: %v", got)
	}
	m.Handle(release(LeftShoulder), at(6400))

	m.RepeatBack = true // the keyboard screen: holding B deletes
	m.Handle(press(East), at(7000))
	if got := m.Tick(at(7400)); !reflect.DeepEqual(got, acts(Back)) {
		t.Fatalf("held B with RepeatBack: %v", got)
	}
}

func TestReleaseCancelsAHeldRepeat(t *testing.T) {
	m := NewMapper(Config{})
	m.Handle(press(DPadUp), t0)
	m.Release() // a dialog opened under a held button
	if got := m.Tick(at(1000)); len(got) != 0 {
		t.Fatalf("repeated after Release: %v", got)
	}
}

// Select acts on release, and only when no other button was pressed while it was held, so the hotkey
// combo of a frontend does not also open the Queue (section 2.1).
func TestSelectActsOnReleaseAndNotInACombo(t *testing.T) {
	tests := []struct {
		name   string
		events []Event
		want   []Action
	}{
		{"alone", []Event{press(Select), release(Select)}, acts(Queue)},
		{"nothing on press", []Event{press(Select)}, nil},
		{"hotkey combo with Start", []Event{press(Select), press(Start), release(Start), release(Select)}, acts(Menu)},
		{"combo with a direction", []Event{press(Select), press(DPadUp), release(DPadUp), release(Select)}, acts(Up)},
		{"a button released before Select was pressed is no combo", []Event{press(South), release(South), press(Select), release(Select)}, acts(Confirm, Queue)},
		{"a second Select press starts clean", []Event{press(Select), press(Start), release(Select), press(Select), release(Select)}, acts(Menu, Queue)},
		{"a stray release does nothing", []Event{release(Select)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMapper(Config{})
			var got []Action
			for i, e := range tc.events {
				got = append(got, m.Handle(e, at(i*10))...)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// The stick: the dominant axis only, a direction past 50 % of the range, released below 35 % (hysteresis),
// and no diagonal movement.
func TestStickHysteresisAndDominantAxis(t *testing.T) {
	m := NewMapper(Config{})
	steps := []struct {
		ev   Event
		want []Action
	}{
		{axis(LeftX, 10000), nil},         // 30 %: not yet
		{axis(LeftX, 16383), nil},         // just under 50 %
		{axis(LeftX, 16384), acts(Right)}, // past 50 %: a step right
		{axis(LeftX, 20000), nil},         // still the same direction: no second step
		{axis(LeftX, 14000), nil},         // between 35 % and 50 %: still held (hysteresis)
		{axis(LeftX, 11000), nil},         // below 35 %: released, no action
		{axis(LeftX, 14000), nil},         // not past 50 % again
		{axis(LeftX, 30000), acts(Right)}, // a new push
		{axis(LeftY, 25000), nil},         // Y smaller than X: X stays dominant, no diagonal
		{axis(LeftX, 5000), acts(Down)},   // X let go, Y now dominant and past 50 %: switches
		{axis(LeftY, -30000), acts(Up)},   // the other way on the same axis
		{axis(LeftY, 0), nil},
	}
	for i, s := range steps {
		if got := m.Handle(s.ev, at(i*10)); !reflect.DeepEqual(got, s.want) {
			t.Fatalf("step %d (%+v): got %v, want %v", i, s.ev, got, s.want)
		}
	}
}

func TestAHeldStickRepeatsLikeTheDPadAndStopsWhenReleased(t *testing.T) {
	m := NewMapper(Config{})
	m.Handle(axis(LeftY, 30000), t0)
	if got := m.Tick(at(400)); !reflect.DeepEqual(got, acts(Down)) {
		t.Fatalf("held stick: %v", got)
	}
	m.Handle(axis(LeftY, 0), at(450))
	if got := m.Tick(at(900)); len(got) != 0 {
		t.Fatalf("a released stick repeated: %v", got)
	}
}

func TestKeyboardFallback(t *testing.T) {
	tests := []struct {
		name string
		ev   Event
		text bool
		want []Action
	}{
		{"arrow up", key(KeyUp), false, acts(Up)},
		{"arrow down", key(KeyDown), false, acts(Down)},
		{"arrow left", key(KeyLeft), false, acts(Left)},
		{"arrow right", key(KeyRight), false, acts(Right)},
		{"Enter is A", key(KeyEnter), false, acts(Confirm)},
		{"Esc is B", key(KeyEscape), false, acts(Back)},
		{"Backspace is B outside the keyboard screen", key(KeyBackspace), false, acts(Back)},
		{"Backspace deletes on the keyboard screen", key(KeyBackspace), true, acts(Delete)},
		{"PgUp is L", key(KeyPageUp), false, acts(PageUp)},
		{"PgDn is R", key(KeyPageDown), false, acts(PageDown)},
		{"Tab is Start", key(KeyTab), false, acts(Menu)},
		{"x is X", letter('x'), false, acts(Download)},
		{"y is Y", letter('y'), false, acts(Info)},
		{"q is Select", letter('q'), false, acts(Queue)},
		{"capital X too", letter('X'), false, acts(Download)},
		{"another letter does nothing", letter('z'), false, nil},
		{"letters are text on the keyboard screen", letter('x'), true, nil},
		{"q too", letter('q'), true, nil},
		{"Enter still confirms there", key(KeyEnter), true, acts(Confirm)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMapper(Config{})
			m.TextMode = tc.text
			if got := m.Handle(tc.ev, t0); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKeyRepeatUsesThePadTimingAndStopsOnKeyUp(t *testing.T) {
	m := NewMapper(Config{})
	m.Handle(key(KeyDown), t0)
	if got := m.Tick(at(400)); !reflect.DeepEqual(got, acts(Down)) {
		t.Fatalf("held arrow: %v", got)
	}
	m.Handle(Event{Kind: KeyRelease, Key: KeyDown}, at(450))
	if got := m.Tick(at(1000)); len(got) != 0 {
		t.Fatalf("repeated after key up: %v", got)
	}
	m.TextMode = true
	m.Handle(key(KeyBackspace), at(2000))
	if got := m.Tick(at(2400)); !reflect.DeepEqual(got, acts(Delete)) {
		t.Fatalf("a held Backspace must keep deleting: %v", got)
	}
}

func TestPositionLabelsByControllerType(t *testing.T) {
	type labels struct{ a, b, x, y, l, r, start, sel string }
	read := func(typ ControllerType) labels {
		return labels{
			PositionLabel(South, typ), PositionLabel(East, typ), PositionLabel(West, typ), PositionLabel(North, typ),
			PositionLabel(LeftShoulder, typ), PositionLabel(RightShoulder, typ), PositionLabel(Start, typ), PositionLabel(Select, typ),
		}
	}
	tests := []struct {
		name string
		typ  ControllerType
		want labels
	}{
		{"unknown pads use Xbox letters", TypeUnknown, labels{"A", "B", "X", "Y", "L", "R", "Start", "Select"}},
		{"Xbox 360", TypeXbox360, labels{"A", "B", "X", "Y", "LB", "RB", "Menu", "View"}},
		{"Xbox One", TypeXboxOne, labels{"A", "B", "X", "Y", "LB", "RB", "Menu", "View"}},
		{"PS4 uses words", TypePS4, labels{"Cross", "Circle", "Square", "Triangle", "L1", "R1", "Start", "Select"}},
		{"PS5", TypePS5, labels{"Cross", "Circle", "Square", "Triangle", "L1", "R1", "Start", "Select"}},
		{"Switch Pro: south is B", TypeSwitchPro, labels{"B", "A", "Y", "X", "L", "R", "Start", "Select"}},
		{"a Joy-Con pair", TypeJoyConPair, labels{"B", "A", "Y", "X", "L", "R", "Start", "Select"}},
		{"virtual", TypeVirtual, labels{"A", "B", "X", "Y", "L", "R", "Start", "Select"}},
		{"a type SDL adds later", ControllerType(99), labels{"A", "B", "X", "Y", "L", "R", "Start", "Select"}},
	}
	for _, tc := range tests {
		if got := read(tc.typ); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// With a Nintendo pad the labels are B A Y X at south, east, west and north, and with
// gui.confirm_button east the roles swap in the hints too (section 11, conformance).
func TestHintsFollowTheRoleTheSwapAndThePad(t *testing.T) {
	tests := []struct {
		role        Action
		typ         ControllerType
		confirmEast bool
		want        string
	}{
		{Confirm, TypeXbox360, false, "A"},
		{Back, TypeXbox360, false, "B"},
		{Confirm, TypeXbox360, true, "B"}, // confirm moved east
		{Back, TypeXbox360, true, "A"},
		{Confirm, TypeSwitchPro, false, "B"}, // south on a Nintendo pad
		{Back, TypeSwitchPro, false, "A"},
		{Confirm, TypeSwitchPro, true, "A"},
		{Download, TypeSwitchPro, false, "Y"},
		{Info, TypeSwitchPro, false, "X"},
		{Confirm, TypePS4, false, "Cross"},
		{Back, TypePS4, true, "Cross"},
		{PageUp, TypePS5, false, "L1"},
		{PageDown, TypeXboxOne, false, "RB"},
		{Menu, TypeXboxOne, false, "Menu"},
		{Queue, TypeXboxOne, false, "View"},
		{Up, TypeXbox360, false, ""}, // a direction has no button label
	}
	for _, tc := range tests {
		if got := Hint(tc.role, tc.typ, tc.confirmEast); got != tc.want {
			t.Errorf("Hint(%d, type %d, east=%v) = %q, want %q", tc.role, tc.typ, tc.confirmEast, got, tc.want)
		}
	}
}

func TestMappingLinesKeepsOnlyUsableLinesForThePlatform(t *testing.T) {
	const guid = "03000000deadbeef0000000000000000"
	db := strings.Join([]string{
		"# Windows",
		"",
		guid + ",Pad A,a:b0,b:b1,platform:Windows,",
		"03000000deadbeef0000000000000001,Pad B,a:b0,b:b1,platform:Linux,",
		"03000000deadbeef0000000000000002,Pad C,a:b0,b:b1,",              // no platform: valid everywhere
		"03000000deadbeef0000000000000003,Pad D,a:b0,platform:Mac OS X,", // another system
		"not a mapping",
		"short,Name,a:b0,platform:Linux,", // GUID too short
		"03000000deadbeef00000000000000zz,Pad E,a:b0,platform:Linux,", // not hex
		"03000000deadbeef0000000000000004,OnlyName",                   // no mapping
		"  03000000DEADBEEF0000000000000005,Pad F,a:b0,platform:Linux,  ",
	}, "\n")
	keep, skipped := MappingLines(db, "Linux")
	wantKeep := []string{
		"03000000deadbeef0000000000000001,Pad B,a:b0,b:b1,platform:Linux,",
		"03000000deadbeef0000000000000002,Pad C,a:b0,b:b1,",
		"03000000DEADBEEF0000000000000005,Pad F,a:b0,platform:Linux,",
	}
	if !reflect.DeepEqual(keep, wantKeep) {
		t.Errorf("keep %q\nwant %q", keep, wantKeep)
	}
	if len(skipped) != 4 {
		t.Errorf("skipped %d malformed lines, want 4: %q", len(skipped), skipped)
	}
	keepWin, _ := MappingLines(db, "Windows")
	if len(keepWin) != 2 || !strings.Contains(keepWin[0], "Pad A") {
		t.Errorf("windows keep %q", keepWin)
	}
	if SDLPlatform("linux") != "Linux" || SDLPlatform("windows") != "Windows" || SDLPlatform("darwin") != "Mac OS X" {
		t.Error("platform names")
	}
}

func TestLoadMappingsReadsTheEmbeddedDatabaseThenTheUsersFile(t *testing.T) {
	const embedded = "03000000deadbeef0000000000000001,Pad B,a:b0,platform:Linux,\nbroken line\n03000000deadbeef0000000000000002,Win only,a:b0,platform:Windows,\n"
	dir := t.TempDir()
	user := filepath.Join(dir, "gamecontrollerdb.txt")
	if err := os.WriteFile(user, []byte("# mine\n03000000deadbeef0000000000000001,Pad B fixed,a:b1,platform:Linux,\nnot a mapping\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	warn := func(s string) { warnings = append(warnings, s) }

	got := LoadMappings(embedded, user, "Linux", warn)
	want := []string{
		"03000000deadbeef0000000000000001,Pad B,a:b0,platform:Linux,",
		"03000000deadbeef0000000000000001,Pad B fixed,a:b1,platform:Linux,", // later wins in SDL
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines %q, want %q", got, want)
	}
	if len(warnings) != 2 || !strings.HasPrefix(warnings[0], "controller database: skipped a malformed line") || !strings.HasPrefix(warnings[1], "gamecontrollerdb.txt: skipped a malformed line") {
		t.Fatalf("warnings %q: one for the broken embedded line, then one for the user's", warnings)
	}

	// No user file and a missing one keep the embedded lines; an unreadable one (a directory) also warns.
	for path, wantWarnings := range map[string]int{"": 1, filepath.Join(dir, "missing.txt"): 1, dir: 2} {
		warnings = nil
		if got := LoadMappings(embedded, path, "Linux", warn); len(got) != 1 {
			t.Errorf("%q: %q", path, got)
		}
		if len(warnings) != wantWarnings {
			t.Errorf("%q: %d warnings %q, want %d", path, len(warnings), warnings, wantWarnings)
		}
	}
}
