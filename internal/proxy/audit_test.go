package proxy

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/zipfix"
)

// Sentinela m4: the member cap is enforced from the end record, before archive/zip would load
// a huge central directory into memory.
func TestZipDirectoryLimitsAreReadFromTheEndRecord(t *testing.T) {
	ok := zipfix.Valid(map[string][]byte{"a.bin": []byte("x")})
	if err := zipDirectoryWithinLimits(bytesAt(ok), int64(len(ok))); err != nil {
		t.Fatalf("a small archive passes: %v", err)
	}
	many := zipfix.ManyEmpty(70_000)
	err := zipDirectoryWithinLimits(bytesAt(many), int64(len(many)))
	if err == nil || err.kind != kindFatal || err.err.Code != CodeIntegrity {
		t.Fatalf("70 000 members must be refused as a fatal integrity failure, got %+v", err)
	}
	if zipDirectoryWithinLimits(bytesAt([]byte("not a zip at all")), 16) != nil {
		t.Fatal("no end record: left to zip.NewReader")
	}
}

type bytesAt []byte

func (b bytesAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, errors.New("EOF")
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, errors.New("EOF")
	}
	return n, nil
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

// Sentinela m9: a memory-backed temp directory is detected (Linux only).
func TestIsMemoryBacked(t *testing.T) {
	if _, err := os.Stat("/dev/shm"); err != nil {
		t.Skip("no /dev/shm")
	}
	if IsMemoryBacked("/dev/shm") == false && IsMemoryBacked(t.TempDir()) == false {
		t.Skip("not Linux, or /dev/shm is not tmpfs here")
	}
	if IsMemoryBacked("/definitely/not/there") {
		t.Fatal("an unreadable path is not reported as memory-backed")
	}
}
