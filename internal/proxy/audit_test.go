package proxy

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/zipfix"
)

// Sentinela m4: the member cap is enforced from the end record, before archive/zip would load
// a huge central directory into memory.
func TestZipDirectoryLimitsAreReadFromTheEndRecord(t *testing.T) {
	ok := zipfix.Valid(map[string][]byte{"a.bin": []byte("x")})
	if err := zipDirectoryWithinLimits(bytes.NewReader(ok), int64(len(ok))); err != nil {
		t.Fatalf("a small archive passes: %v", err)
	}
	many := zipfix.ManyEmpty(70_000)
	err := zipDirectoryWithinLimits(bytes.NewReader(many), int64(len(many)))
	if err == nil || err.kind != kindFatal || err.err.Code != CodeIntegrity {
		t.Fatalf("70 000 members must be refused as a fatal integrity failure, got %+v", err)
	}
	if zipDirectoryWithinLimits(bytes.NewReader([]byte("not a zip at all")), 16) != nil {
		t.Fatal("no end record: left to zip.NewReader")
	}
}

// Sentinela m5: the properties of the real download client.
func TestNewHTTPClientPolicy(t *testing.T) {
	c := NewHTTPClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", c.Transport)
	}
	if !tr.DisableCompression || c.Jar != nil || c.Timeout != 0 {
		t.Fatal("no compression, no cookie jar, no overall timeout")
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion < tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS 1.2 or later, certificates always verified")
	}
	if err := c.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirects must come back to the downloader, got %v", err)
	}
}

// Sentinela m9: a memory-backed directory is detected (Linux): /dev/shm is asserted when the
// mount table says it is tmpfs, and the test skips otherwise.
func TestIsMemoryBacked(t *testing.T) {
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil || !strings.Contains(string(mounts), " /dev/shm tmpfs ") {
		t.Skip("no tmpfs /dev/shm to assert on (not Linux, or not mounted)")
	}
	if !IsMemoryBacked("/dev/shm") {
		t.Fatal("/dev/shm is tmpfs and must be reported as memory-backed")
	}
	if IsMemoryBacked("/definitely/not/there") {
		t.Fatal("an unreadable path is not reported as memory-backed")
	}
}

// Sentinela m4 probe: the plain end record claims fewer members than the directory holds (the
// count is 16 bits wide) and the zip64 locator is gone. archive/zip would still load every
// member, so the directory is walked instead of trusting the declared count.
func TestLyingEndRecordIsCaughtByWalkingTheDirectory(t *testing.T) {
	z := zipfix.ManyEmpty(70_000)
	at := bytes.LastIndex(z, []byte{0x50, 0x4b, 0x05, 0x06})
	loc := bytes.LastIndex(z[:at], []byte{0x50, 0x4b, 0x06, 0x07})
	if at < 0 || loc < 0 {
		t.Fatal("the fixture has no zip64 records")
	}
	copy(z[loc:], []byte{0, 0, 0, 0})                       // the locator is gone
	binary.LittleEndian.PutUint16(z[at+8:], 70_000%65_536)  // entries on this disk
	binary.LittleEndian.PutUint16(z[at+10:], 70_000%65_536) // total entries

	// The central directory ends where the zip64 end record started: recompute its size.
	rec := bytes.LastIndex(z[:loc], []byte{0x50, 0x4b, 0x06, 0x06})
	cdStart := int(binary.LittleEndian.Uint64(z[rec+48:]))        //nolint:gosec // a few MB
	binary.LittleEndian.PutUint32(z[at+12:], uint32(rec-cdStart)) //nolint:gosec // a few MB

	err := zipDirectoryWithinLimits(bytes.NewReader(z), int64(len(z)))
	if err == nil || err.err.Code != CodeIntegrity {
		t.Fatalf("a directory of 70 000 members behind a count of 4 464 must be refused, got %+v", err)
	}
}
