package proxy_test

// T-16 catalog cases for verification (41 to 43) and the Sanitize invariants (12).

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/zipfix"
)

func verifyRig(t *testing.T) *rig { return newRig(t, proxy.Config{}) }

// Case 41: hostile archives are refused as integrity failures, with bounded work.
func TestCase41ZipBombs(t *testing.T) {
	tests := map[string][]byte{
		"member longer than declared":       zipfix.DeclaredSizeLie(1<<20, 1000),
		"overlapping members over the cap":  zipfix.Overlap(50, 40<<20),
		"more members than the cap":         zipfix.ManyEmpty(70000),
		"declared sizes over the byte cap":  zipfix.Overlap(65535, 1<<20),
		"damaged header after the magic":    append([]byte("PK\x03\x04"), fakeproxy.Pattern(200)...),
		"truncated archive after the magic": zipfix.Valid(map[string][]byte{"a": fakeproxy.Pattern(5000)})[:300],
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			r := verifyRig(t)
			r.srv.Script("/f/z", fakeproxy.OK(body))
			wantFailed(t, r.download(r.job("/f/z", "z.zip", len(body))), proxy.CodeIntegrity)
			// Bounded work: two attempts (one integrity retry) read the file a few times, never
			// the gigabytes the archive declares.
			if got, ceiling := r.fs.BytesRead(), 4*int64(len(body))+64<<10; got > ceiling {
				t.Fatalf("verification read %d bytes of a %d-byte file (ceiling %d)", got, len(body), ceiling)
			}
			stagingEmpty(t, r)
			noFinalFile(t, r)
			if got := r.history.recs[len(r.history.recs)-1].CRC; got != "failed" {
				t.Fatalf("history crc = %q, want failed", got)
			}
		})
	}
}

// countReads is the number of Read calls the fake file system has seen.
func countReads(r *rig) int {
	n := 0
	for _, c := range r.fs.Calls() {
		if c.Op == fakefs.OpRead {
			n++
		}
	}
	return n
}

// Case 42: a cancel in the middle of the CRC pass stops the reading at once and returns
// Canceled, not an integrity failure.
func TestCase42CancelDuringZipCRC(t *testing.T) {
	members := map[string][]byte{}
	for _, n := range []string{"a.bin", "b.bin", "c.bin", "d.bin"} {
		members[n] = fakeproxy.Pattern(2 << 20)
	}
	body := zipfix.Valid(members)

	full := verifyRig(t) // calibration: how many reads a complete verification takes
	full.srv.Script("/f/z", fakeproxy.OK(body))
	if res := full.download(full.job("/f/z", "z.zip", len(body))); res.State != proxy.StateDone {
		t.Fatalf("calibration run: %s %v", res.State, res.Err)
	}
	total := countReads(full)
	if total < 20 {
		t.Fatalf("only %d reads: the archive is too small to cancel in the middle", total)
	}

	r := verifyRig(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	seen, stopAt := 0, total/2
	var tick func()
	tick = func() {
		if seen++; seen == stopAt {
			cancel()
			return
		}
		r.fs.Before(fakefs.OpRead, "*", tick)
	}
	r.fs.Before(fakefs.OpRead, "*", tick)
	r.srv.Script("/f/z", fakeproxy.OK(body))

	res := r.dl.Download(ctx, r.job("/f/z", "z.zip", len(body)))

	if res.State != proxy.StateCanceled || res.Err == nil || res.Err.Code != proxy.CodeCanceled {
		t.Fatalf("state %s err %v", res.State, res.Err)
	}
	if after := countReads(r); after > stopAt+2 {
		t.Fatalf("%d reads happened after the cancel at read %d (a complete run takes %d)", after-stopAt, stopAt, total)
	}
	stagingEmpty(t, r)
	noFinalFile(t, r)
}

// Case 43: the zip check keys on the magic bytes, not on the name, and says what it did.
func TestCase43MagicByteDetection(t *testing.T) {
	tests := []struct {
		name  string
		body  []byte
		state proxy.State
		crc   string
	}{
		{"end record only (PK 05 06)", zipfix.EmptyArchive(), proxy.StateDone, "skipped"},
		{"random bytes named .zip", fakeproxy.Pattern(4000), proxy.StateDone, "skipped"},
		{"valid archive", zipfix.Valid(map[string][]byte{"a.bin": fakeproxy.Pattern(4000)}), proxy.StateDone, "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := verifyRig(t)
			r.srv.Script("/f/z", fakeproxy.OK(tt.body))
			res := r.download(r.job("/f/z", "z.zip", len(tt.body)))
			if res.State != tt.state {
				t.Fatalf("state %s err %v", res.State, res.Err)
			}
			if got := r.history.recs[0].CRC; got != tt.crc {
				t.Fatalf("history crc = %q, want %q", got, tt.crc)
			}
		})
	}
	t.Run("verify off skips every check", func(t *testing.T) {
		r := newRig(t, proxy.Config{SkipVerify: true})
		body := zipfix.DeclaredSizeLie(1<<20, 1000)
		r.srv.Script("/f/z", fakeproxy.OK(body))
		if res := r.download(r.job("/f/z", "z.zip", len(body))); res.State != proxy.StateDone {
			t.Fatalf("state %s err %v", res.State, res.Err)
		}
		if got := r.history.recs[0].CRC; got != "skipped" {
			t.Fatalf("history crc = %q, want skipped", got)
		}
	})
}

// sanitizeInvariants are the guarantees of downloader.md 5.9 for any input.
func sanitizeInvariants(t *testing.T, raw string) {
	t.Helper()
	opts := []proxy.NameOption{proxy.WithFallback("slug", "zip", "jobid")}
	got, err := proxy.Sanitize(raw, opts...)
	if errors.Is(err, proxy.ErrUnsafeName) {
		return // the deny-list refusal is a valid outcome: no name is produced at all
	}
	if err != nil {
		t.Fatalf("%q: %v", raw, err)
	}
	fail := func(why string) { t.Fatalf("%q -> %q: %s", raw, got, why) }
	switch {
	case got == "":
		fail("empty")
	case len(got) > 200:
		fail("longer than 200 bytes")
	case !utf8.ValidString(got):
		fail("not valid UTF-8")
	case got == "." || got == "..":
		fail("dot name")
	case strings.ContainsAny(got, `/\:<>"|?*`):
		fail("separator or forbidden character")
	case strings.HasPrefix(got, ".") || strings.HasPrefix(got, "-") || strings.HasPrefix(got, " "):
		fail("leading dot, dash or space")
	case strings.HasSuffix(got, ".") || strings.HasSuffix(got, " "):
		fail("trailing dot or space")
	}
	for _, c := range got {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			fail("control or format character")
		}
	}
	stem := strings.ToUpper(strings.SplitN(got, ".", 2)[0])
	if reservedDevice[strings.TrimRight(stem, " ")] {
		fail("Windows reserved device name")
	}
	if again, err := proxy.Sanitize(got, opts...); err != nil || again != got {
		fail("not idempotent")
	}
}

// reservedDevice is every Windows device name of downloader.md 5.9, digits 1 to 9 and the
// superscript digits included.
var reservedDevice = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true}
	for _, d := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
		m["COM"+d], m["LPT"+d] = true, true
	}
	return m
}()

// Case 12 (property): random strings built from hostile pieces keep every invariant.
func TestCase12SanitizeInvariantsOnRandomInput(t *testing.T) {
	pieces := []string{"..", "/", `\`, ":", "CON", "nul", ".", " ", "\x00", "\x1b[2J", "\u202e", "\u200b", "\uff58", "\uff0f", "é", "é",
		"😀", "あ", "COM1", "lpt9", "COM²", "CONIN$", "conout$", "aux.", ".sh", "-", "<", "|", "$DATA", strings.Repeat("a", 120)}
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // a fixed seed: the test must be reproducible
	for range 20000 {
		var b strings.Builder
		for range 1 + rng.IntN(8) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		sanitizeInvariants(t, b.String())
	}
}

// Case 12 (fuzz): the same invariants under go test -fuzz.
func FuzzSanitizeInvariants(f *testing.F) {
	for _, s := range []string{"../x", "CON", "a\x00b", "game.zip.", "\u202e", "\uff58\uff0fｙ", "COM¹", strings.Repeat("é", 150), "..\\..\\x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) { sanitizeInvariants(t, raw) })
}
