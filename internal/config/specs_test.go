package config

import (
	"strconv"
	"testing"
)

// The metadata a UI reads must agree with what the parser accepts: every enum value parses, and
// the bounds of a ranged key are the ones the parser enforces.
func TestSpecsAgreeWithTheParsers(t *testing.T) {
	specs := Specs()
	if len(specs) != len(schema) {
		t.Fatalf("%d specs for %d keys", len(specs), len(schema))
	}
	for i, sp := range specs {
		s := schema[i]
		if sp.Key != s.key() || sp.Env != envName(sp.Key) || sp.Kind == "" {
			t.Errorf("%s: %+v", s.key(), sp)
		}
		switch sp.Kind {
		case KindEnum:
			if len(sp.Enum) == 0 {
				t.Errorf("%s: an enum without values", sp.Key)
			}
			for _, v := range sp.Enum {
				if _, err := s.parse(v); err != nil {
					t.Errorf("%s: enum value %q is refused: %v", sp.Key, v, err)
				}
			}
			if _, err := s.parse("no-such-value"); err == nil {
				t.Errorf("%s: accepts a value outside its enum", sp.Key)
			}
		case KindInt:
			lo, _ := strconv.Atoi(sp.Min)
			hi, _ := strconv.Atoi(sp.Max)
			for v, ok := range map[int]bool{lo: true, hi: true, lo - 1: false, hi + 1: false} {
				if _, err := s.parse(v); (err == nil) != ok {
					t.Errorf("%s: %d accepted=%v, want %v (range %s..%s)", sp.Key, v, err == nil, ok, sp.Min, sp.Max)
				}
			}
		case KindDuration:
			for _, v := range []string{sp.Min, sp.Max} {
				if _, err := s.parse(v); err != nil {
					t.Errorf("%s: bound %q is refused: %v", sp.Key, v, err)
				}
			}
		}
	}
	// A key the loader ignores in the environment advertises no variable (profile.folders.<id>).
	f, _ := lookup("profile.folders.snes")
	if sp := f.spec(); sp.Env != "" || !f.noEnv() {
		t.Errorf("folder key advertises %q", sp.Env)
	}
	specs[0].Key = "changed"
	if Specs()[0].Key == "changed" {
		t.Fatal("Specs must return a copy")
	}
}

// gui.safe_area is auto or 0..10, written as a number or as text in the file.
func TestGUISafeArea(t *testing.T) {
	s, _ := lookup("gui.safe_area")
	for in, want := range map[any]string{"auto": "auto", 5: "5", "10": "10", 0: "0"} {
		if got, err := s.parse(in); err != nil || got != want {
			t.Errorf("%v: %v %v", in, got, err)
		}
	}
	for _, bad := range []any{11, -1, "11", "wide", 2.5, true} {
		if _, err := s.parse(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
