//go:build gui

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/sdl"
)

// --probe says where SDL2 was loaded from as one word after the version, never a path, and stays one short sanitized line
// (ports.md PRT-G9). Where the question does not apply (Windows) the line is the old "sdl X.Y.Z".
func TestProbePrintsTheVersionAndTheOriginWord(t *testing.T) {
	defer func(old func() (sdl.Found, error)) { checkSDL = old }(checkSDL)
	tests := []struct {
		name   string
		found  sdl.Found
		want   string
		maxLen int
	}{
		{"system", sdl.Found{Version: sdl.Version{Major: 2, Minor: 32, Patch: 10}, Origin: sdl.OriginSystem}, "sdl 2.32.10 system\n", 200},
		{"emuelec", sdl.Found{Version: sdl.Version{Major: 2, Minor: 30, Patch: 11}, Origin: sdl.OriginLibraryPath}, "sdl 2.30.11 ld_library_path\n", 200},
		{"windows", sdl.Found{Version: sdl.Version{Major: 2, Minor: 32, Patch: 10}}, "sdl 2.32.10\n", 200},
		{"an odd word is sanitized and cut", sdl.Found{Version: sdl.Version{Major: 2, Minor: 0, Patch: 4}, Origin: "a\nb\x1b" + strings.Repeat("x", 400)}, "", 201},
	}
	for _, tc := range tests {
		checkSDL = func() (sdl.Found, error) { return tc.found, nil }
		var out, errOut bytes.Buffer
		if code := run([]string{"mothdex-gui", "--probe"}, &out, &errOut); code != exitOK {
			t.Fatalf("%s: exit %d, stderr %q", tc.name, code, errOut.String())
		}
		got := out.String()
		if tc.want != "" && got != tc.want {
			t.Errorf("%s: stdout %q, want %q", tc.name, got, tc.want)
		}
		if len(got) > tc.maxLen || strings.Count(got, "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(got, "\n"), "\x1b\r\n") {
			t.Errorf("%s: the line is not one sanitized line of at most 200 bytes: %q", tc.name, got)
		}
	}
}

// The start-up diagnostics line carries the same word next to the version, and LANGUAGE as its first entry only, as a word
// (ports.md PRT-G9 and PRT-G10): a whole list, a path or an empty value is never copied.
func TestDiagnosticsCarryTheOriginWordAndTheFirstLanguage(t *testing.T) {
	lookup := func(env map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	}
	value := func(d diagnostics, key string) any {
		attrs := d.attrs()
		for i := 0; i+1 < len(attrs); i += 2 {
			if attrs[i] == key {
				return attrs[i+1]
			}
		}
		return nil
	}
	base := diagnostics{SDLVersion: "2.30.11", Lookup: lookup(nil), Pads: []input.ControllerType{}}

	for _, tc := range []struct{ origin, want string }{
		{sdl.OriginSystem, "system"}, {sdl.OriginLibraryPath, "ld_library_path"}, {sdl.OriginOther, "other"},
		{sdl.OriginUnknown, "unknown"}, {"", "n/a"}, {"/home/alice/lib", "not shown"},
	} {
		d := base
		d.SDLOrigin = tc.origin
		if got := value(d, "sdl_origin"); got != tc.want {
			t.Errorf("origin %q is logged as %v, want %q", tc.origin, got, tc.want)
		}
	}

	for _, tc := range []struct {
		name  string
		env   map[string]string
		want  string
		leaks []string
	}{
		{"a list logs its first entry", map[string]string{"LANGUAGE": "pt_BR:en"}, "pt_BR", []string{":en"}},
		{"one entry", map[string]string{"LANGUAGE": "ru"}, "ru", nil},
		{"absent", nil, "unset", nil},
		{"empty", map[string]string{"LANGUAGE": ""}, "unset", nil},
		{"an empty first entry", map[string]string{"LANGUAGE": ":en"}, "unset", nil},
		{"a path is not copied", map[string]string{"LANGUAGE": "/home/alice/loc:en"}, "not shown", []string{"alice", "/home"}},
	} {
		d := base
		d.Lookup = lookup(tc.env)
		got := value(d, "LANGUAGE")
		if got != tc.want {
			t.Errorf("%s: LANGUAGE is %v, want %q", tc.name, got, tc.want)
		}
		for _, leak := range tc.leaks {
			if strings.Contains(strings.Join(attrStrings(d.attrs()), " "), leak) {
				t.Errorf("%s: the line leaks %q", tc.name, leak)
			}
		}
	}
	// LANGUAGE is a word, not one of the variables named only as set or unset.
	for _, name := range setOnly {
		if name == "LANGUAGE" {
			t.Error("LANGUAGE is logged with its first entry, not as set or unset")
		}
	}
}

func attrStrings(attrs []any) []string {
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
