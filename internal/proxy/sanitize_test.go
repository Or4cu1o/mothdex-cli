package proxy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// sanitizeVectors is the shared fixture: mothdex-mcp copies testdata/sanitize-vectors.json and
// checks it for divergence, so the file is a contract and the error codes in it are stable.
type sanitizeVectors struct {
	Version int `json:"version"`
	Vectors []struct {
		Name            string `json:"name"`
		Input           string `json:"input"`
		AllowExecutable bool   `json:"allow_executable"`
		Fallback        *struct {
			Slug   string `json:"slug"`
			Format string `json:"format"`
			JobID  string `json:"job_id"`
		} `json:"fallback"`
		Output *string `json:"output"`
		Error  *string `json:"error"`
	} `json:"vectors"`
}

func TestSanitizeVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "sanitize-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc sanitizeVectors
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version != 1 || len(doc.Vectors) == 0 {
		t.Fatalf("fixture: version %d, %d vectors, %v", doc.Version, len(doc.Vectors), err)
	}
	codes := map[string]error{"unsafe_name": ErrUnsafeName, "empty_name": ErrEmptyName}
	for _, v := range doc.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			var opts []NameOption
			if v.Fallback != nil {
				opts = append(opts, WithFallback(v.Fallback.Slug, v.Fallback.Format, v.Fallback.JobID))
			}
			if v.AllowExecutable {
				opts = append(opts, AllowExecutable())
			}
			got, err := Sanitize(v.Input, opts...)
			if (v.Output == nil) == (v.Error == nil) {
				t.Fatal("exactly one of output and error must be set")
			}
			if v.Error != nil {
				want, ok := codes[*v.Error]
				if !ok || !errors.Is(err, want) || got != "" {
					t.Fatalf("got %q, %v, want error %q", got, err, *v.Error)
				}
				return
			}
			if err != nil || got != *v.Output {
				t.Fatalf("got %q, %v, want %q", got, err, *v.Output)
			}
		})
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
