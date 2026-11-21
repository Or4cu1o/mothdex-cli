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
