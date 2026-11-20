package zipfix_test

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/zipfix"
)

func open(t *testing.T, b []byte) *zip.Reader {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	return r
}

func readAll(f *zip.File) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	return io.Copy(io.Discard, rc)
}

func TestValidReadsClean(t *testing.T) {
	r := open(t, zipfix.Valid(map[string][]byte{"a.bin": []byte("hello")}))
	if n, err := readAll(r.File[0]); err != nil || n != 5 {
		t.Fatalf("read = %d, %v", n, err)
	}
}

func TestValidIsStoredAndDeterministic(t *testing.T) {
	in := map[string][]byte{"b.bin": []byte("22"), "a.bin": []byte("1"), "c.bin": []byte("333")}
	first := zipfix.Valid(in)
	for range 5 {
		if !bytes.Equal(first, zipfix.Valid(in)) {
			t.Fatal("Valid is not deterministic")
		}
	}
	for i, f := range open(t, first).File {
		if f.Method != zip.Store || f.Name != string('a'+rune(i))+".bin" {
			t.Fatalf("member %d = %q method %d", i, f.Name, f.Method)
		}
	}
}

func TestOverlapRejectsFieldOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Overlap accepted more members than the zip header can count")
		}
	}()
	zipfix.Overlap(70000, 1<<20)
}

func TestManyEmptyExceedsMemberCap(t *testing.T) {
	if got := len(open(t, zipfix.ManyEmpty(70000)).File); got != 70000 {
		t.Fatalf("members = %d, want 70000", got)
	}
}

func TestDeclaredSizeLieFailsOnRead(t *testing.T) {
	r := open(t, zipfix.DeclaredSizeLie(1<<20, 1000))
	if _, err := readAll(r.File[0]); err == nil {
		t.Fatal("reading a member larger than declared must fail")
	}
}

func TestBombInflatesFarBeyondItsSize(t *testing.T) {
	const inflated = 64 << 20
	b := zipfix.Bomb(inflated)
	if len(b) > inflated/100 {
		t.Fatalf("archive is %d bytes, not a bomb", len(b))
	}
	if n, err := readAll(open(t, b).File[0]); err != nil || n != inflated {
		t.Fatalf("read = %d, %v; want %d", n, err, inflated)
	}
}

func TestOverlapDeclaresManyMembersFromOneBody(t *testing.T) {
	const members, inner = 50, 4 << 20
	b := zipfix.Overlap(members, inner)
	r := open(t, b)
	if len(r.File) != members {
		t.Fatalf("members = %d, want %d", len(r.File), members)
	}
	var total int64
	for _, f := range r.File {
		n, err := readAll(f)
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		total += n
	}
	if total != members*inner || len(b) > inner/10 {
		t.Fatalf("total %d (want %d), archive %d bytes", total, int64(members)*inner, len(b))
	}
}

func TestEmptyArchiveHasEndRecordMagic(t *testing.T) {
	b := zipfix.EmptyArchive()
	if !bytes.HasPrefix(b, []byte("PK\x05\x06")) {
		t.Fatalf("prefix = %q", b[:4])
	}
}
