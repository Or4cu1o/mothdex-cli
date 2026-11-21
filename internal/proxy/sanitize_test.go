package proxy

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeTable(t *testing.T) {
	fb := WithFallback("zelda", "zip", "abcdefghijklmnopqrstuvwxyz")
	tests := []struct {
		name string
		raw  string
		opts []NameOption
		want string
	}{
		{"unix traversal", "../../etc/passwd", nil, "passwd"},
		{"windows traversal", `..\..\Windows\x.zip`, nil, "x.zip"},
		{"absolute", "/abs/x.zip", nil, "x.zip"},
		{"drive", `C:\x.zip`, nil, "x.zip"},
		{"unc", `\\srv\share\x.zip`, nil, "x.zip"},
		{"subdirs", "dir/sub/x.zip", nil, "x.zip"},
		{"CON", "CON", nil, "_CON"},
		{"con.zip", "con.zip", nil, "_con.zip"},
		{"NUL.tar.gz", "NUL.tar.gz", nil, "_NUL.tar.gz"},
		{"COM1.bin", "COM1.bin", nil, "_COM1.bin"},
		{"COM superscript", "COM¹.zip", nil, "_COM¹.zip"},
		{"LPT9", "LPT9", nil, "_LPT9"},
		{"CONIN$", "CONIN$", nil, "_CONIN$"},
		{"aux space", "aux .zip", nil, "_aux .zip"},
		{"trailing dot", "game.zip.", nil, "game.zip"},
		{"trailing space", "game.zip ", nil, "game.zip"},
		{"trailing dots and spaces", "game.zip. . .", nil, "game.zip"},
		{"ads", "game.zip:evil", nil, "game.zip_evil"},
		{"data stream", "x::$DATA", nil, "x_$DATA"},
		{"forbidden", `a<b>c|d?e*f"g.zip`, nil, "a_b_c_d_e_f_g.zip"},
		{"control", "a\x00b\x1b[31mc\x7fd\ne.zip", nil, "ab[31mcde.zip"},
		{"bidi", "game\u202Ehs.zip", nil, "gamehs.zip"},
		{"zero width", "ga\u200Bme.zip", nil, "game.zip"},
		{"fullwidth slash", "ｇａｍｅ／ｘ.zip", nil, "ｇａｍｅ_ｘ.zip"},
		{"fullwidth colon", "a：b.zip", nil, "a_b.zip"},
		{"NFD", "Poke\u0301mon.zip", nil, "Pokémon.zip"},
		{"NFC", "Pokémon.zip", nil, "Pokémon.zip"},
		{"leading dot", ".bashrc", nil, "bashrc"},
		{"leading dash", "-rf.zip", nil, "rf.zip"},
		{"leading space", " x.zip", nil, "x.zip"},
		{"empty with slug", "", []NameOption{fb}, "zelda.zip"},
		{"dot", ".", []NameOption{fb}, "zelda.zip"},
		{"dotdot", "..", []NameOption{fb}, "zelda.zip"},
		{"dots", "...", []NameOption{fb}, "zelda.zip"},
		{"spaces", "   ", []NameOption{fb}, "zelda.zip"},
		{"lone bidi", "\u202E", []NameOption{fb}, "zelda.zip"},
		{"malicious slug", "", []NameOption{WithFallback("../x", "zip/../../x", "jid")}, "x"},
		{"empty slug", "", []NameOption{WithFallback("", "zip", "jobid26")}, "download-jobid26"},
		{"deny sh", "x.sh", nil, ""},
		{"deny SH", "x.SH", nil, ""},
		{"deny double", "x.zip.sh", nil, ""},
		{"deny desktop", "x.desktop", nil, ""},
		{"deny AppImage", "x.AppImage", nil, ""},
		{"deny exe", "x.exe", nil, ""},
		{"deny bat", "x.bat", nil, ""},
		{"deny lnk", "x.lnk", nil, ""},
		{"deny ps1", "x.ps1", nil, ""},
		{"deny via fallback", "", []NameOption{WithFallback("run", "sh", "j")}, ""},
		{"allow bin", "x.bin", nil, "x.bin"},
		{"allow cue", "x.cue", nil, "x.cue"},
		{"allow iso", "x.iso", nil, "x.iso"},
		{"allow chd", "x.chd", nil, "x.chd"},
		{"allow zip", "x.zip", nil, "x.zip"},
		{"allow 7z", "x.7z", nil, "x.7z"},
		{"allow-executable", "x.sh", []NameOption{AllowExecutable()}, "x.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Sanitize(tt.raw, tt.opts...)
			if tt.want == "" {
				if !errors.Is(err, ErrUnsafeName) {
					t.Fatalf("got %q, %v, want ErrUnsafeName", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestSanitizeWithoutFallbackIsAnError(t *testing.T) {
	if _, err := Sanitize("..."); !errors.Is(err, ErrEmptyName) {
		t.Fatalf("want ErrEmptyName, got %v", err)
	}
}

func TestSanitizeLength(t *testing.T) {
	for name, stem := range map[string]string{
		"ascii":  strings.Repeat("a", 300),
		"3 byte": strings.Repeat("あ", 100),
		"4 byte": strings.Repeat("😀", 75),
	} {
		got, err := Sanitize(stem + ".zip")
		if err != nil || len(got) > maxNameBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, ".zip") {
			t.Errorf("%s: %d bytes, %q, %v", name, len(got), got, err)
		}
	}
}

// checkInvariants is shared by the property test and the fuzz target.
func checkInvariants(t *testing.T, raw string) {
	t.Helper()
	got, err := Sanitize(raw, WithFallback("slug", "zip", "jobid"), AllowExecutable())
	if err != nil {
		t.Fatalf("%q: %v", raw, err)
	}
	if again, err := Sanitize(got, WithFallback("slug", "zip", "jobid"), AllowExecutable()); err != nil || again != got {
		t.Fatalf("not idempotent: %q -> %q -> %q (%v)", raw, got, again, err)
	}
}

func TestSanitizeIsIdempotentOnHardInputs(t *testing.T) {
	for _, raw := range []string{"con", "CON.", " con ", "-con", "..con..", "COM¹ .x", strings.Repeat("con ", 80),
		strings.Repeat("a", 199) + "é.zip", strings.Repeat("a", 190) + ".CON", "a\u0301\u200B\u0301", "_", "__ __"} {
		checkInvariants(t, raw)
	}
}

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{"../x", "CON", "a\x00b", "game.zip.", "\u202E", "ｘ／ｙ", "COM¹", strings.Repeat("é", 150)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) { checkInvariants(t, raw) })
}
