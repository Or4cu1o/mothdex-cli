package cover

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeresolver"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }

const publicIP = "93.184.216.34"

// rig serves handler over TLS; every dial that passes the guard lands on that server and is
// counted, so a test can assert that a blocked address was never connected to.
type rig struct {
	f     *Fetcher
	store cache.Store
	dials atomic.Int32
	res   *fakeresolver.Resolver
}

func newRig(t *testing.T, h http.Handler, res *fakeresolver.Resolver) *rig {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache"), cache.Config{Enabled: true, TTL: cache.TTL{Covers: time.Hour}}, clock{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	r := &rig{store: store, res: res}
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	tlsCfg.RootCAs = pool
	f, err := newWith(Options{Resolver: res, Cache: store}, hooks{tls: tlsCfg, dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		r.dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}})
	if err != nil {
		t.Fatal(err)
	}
	r.f = f
	return r
}

func publicHost() *fakeresolver.Resolver {
	return fakeresolver.New(map[string][]string{"example.com": {publicIP}})
}

func (r *rig) stored(url string) bool {
	rc, e, err := r.store.GetBlob(cache.NSCovers, cache.KeyCover(url))
	if rc != nil {
		_ = rc.Close()
	}
	return err == nil && e.Hit
}

func serve(ct string, body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	})
}

// pngHeader is a PNG of the declared size with an IHDR only: DecodeConfig reads no pixels.
func pngHeader(w, h uint32) []byte {
	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, struct {
		W, H            uint32
		Depth, Color    uint8
		Comp, Flt, Intl uint8
	}{w, h, 8, 2, 0, 0, 0})
	chunk := append([]byte("IHDR"), ihdr.Bytes()...)
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&out, binary.BigEndian, uint32(13))
	out.Write(chunk)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return out.Bytes()
}

func webpVP8L(w, h uint32) []byte {
	b := make([]byte, 30)
	copy(b, "RIFF")
	copy(b[8:], "WEBPVP8L")
	binary.LittleEndian.PutUint32(b[16:], 5)
	b[20] = 0x2f
	binary.LittleEndian.PutUint32(b[21:], (w-1)|(h-1)<<14)
	return b
}

// webpVP8X is a VP8X canvas followed by a VP8L frame chunk.
func webpVP8X(cw, ch, fw, fh uint32) []byte {
	b := make([]byte, 12+18+8+5)
	copy(b, "RIFF")
	copy(b[8:], "WEBPVP8X")
	binary.LittleEndian.PutUint32(b[16:], 10)
	for i, v := range []uint32{cw - 1, ch - 1} {
		b[24+3*i], b[25+3*i], b[26+3*i] = byte(v&0xff), byte(v>>8&0xff), byte(v>>16&0xff)
	}
	copy(b[30:], "VP8L")
	binary.LittleEndian.PutUint32(b[34:], 5)
	b[38] = 0x2f
	binary.LittleEndian.PutUint32(b[39:], (fw-1)|(fh-1)<<14)
	return b
}

const coverURL = "https://example.com/c.png"

// Case 47: the SSRF guard.
func TestCase47CoverSSRFIsNotFetched(t *testing.T) {
	ok := serve("image/png", pngHeader(64, 64))
	tests := []struct {
		name string
		url  string
		res  *fakeresolver.Resolver
	}{
		{"http scheme", "http://example.com/c.png", publicHost()},
		{"private IP literal", "https://192.168.0.1/c.png", publicHost()},
		{"loopback literal", "https://127.0.0.1/c.png", publicHost()},
		{"IPv4-mapped loopback", "https://[::ffff:127.0.0.1]/c.png", publicHost()},
		{"link-local metadata", "https://169.254.169.254/c.png", publicHost()},
		{"userinfo", "https://user@example.com/c.png", publicHost()},
		{"DNS to loopback", coverURL, fakeresolver.New(map[string][]string{"example.com": {"127.0.0.1"}})},
		{"DNS to CGNAT", coverURL, fakeresolver.New(map[string][]string{"example.com": {"100.64.0.1"}})},
		{"one private answer among public ones", coverURL, fakeresolver.New(map[string][]string{"example.com": {publicIP, "10.0.0.5"}})},
		{"DNS to IPv6 ULA", coverURL, fakeresolver.New(map[string][]string{"example.com": {"fd00::1"}})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, ok, tt.res)
			if r.f.Fetch(context.Background(), tt.url) || r.stored(tt.url) {
				t.Fatal("fetched or stored")
			}
			if r.dials.Load() != 0 {
				t.Fatalf("%d connections to a blocked target", r.dials.Load())
			}
		})
	}
}

func TestRedirectsAreRecheckedAndCapped(t *testing.T) {
	t.Run("redirect to a private literal", func(t *testing.T) {
		r := newRig(t, http.RedirectHandler("https://10.0.0.1/c.png", http.StatusFound), publicHost())
		if r.f.Fetch(context.Background(), coverURL) || r.dials.Load() != 1 {
			t.Fatalf("fetched, or dialed %d times (only the first hop may connect)", r.dials.Load())
		}
	})
	t.Run("redirect to a host that resolves privately", func(t *testing.T) {
		res := fakeresolver.New(map[string][]string{"example.com": {publicIP}, "evil.test": {"192.168.1.1"}})
		r := newRig(t, http.RedirectHandler("https://evil.test/c.png", http.StatusFound), res)
		if r.f.Fetch(context.Background(), coverURL) || r.dials.Load() != 1 {
			t.Fatalf("fetched, or dialed %d times", r.dials.Load())
		}
	})
	t.Run("redirect to http", func(t *testing.T) {
		r := newRig(t, http.RedirectHandler("http://example.com/c.png", http.StatusFound), publicHost())
		if r.f.Fetch(context.Background(), coverURL) || r.dials.Load() != 1 {
			t.Fatal("an http redirect must not be followed")
		}
	})
	t.Run("DNS rebinding: public first, private on the next lookup", func(t *testing.T) {
		res := fakeresolver.NewSequence(map[string][][]string{"example.com": {{publicIP}, {"127.0.0.1"}}})
		r := newRig(t, http.RedirectHandler("https://example.com/again", http.StatusFound), res)
		if r.f.Fetch(context.Background(), coverURL) || r.dials.Load() != 1 {
			t.Fatalf("dialed %d times", r.dials.Load())
		}
	})
	t.Run("four redirects", func(t *testing.T) {
		var hits atomic.Int32
		r := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			hits.Add(1)
			http.Redirect(w, req, "https://example.com/next", http.StatusFound)
		}), publicHost())
		if r.f.Fetch(context.Background(), coverURL) || hits.Load() != maxRedirects+1 {
			t.Fatalf("%d requests, want %d", hits.Load(), maxRedirects+1)
		}
	})
	t.Run("same-host redirect within the cap is followed", func(t *testing.T) {
		r := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/c.png" {
				http.Redirect(w, req, "https://example.com/final.png", http.StatusFound)
				return
			}
			serve("image/png", pngHeader(64, 64)).ServeHTTP(w, req)
		}), publicHost())
		if !r.f.Fetch(context.Background(), coverURL) || !r.stored(coverURL) {
			t.Fatal("a safe redirect must work")
		}
	})
}

// Case 48: content checks.
func TestCase48CoverContentChecks(t *testing.T) {
	big := append(pngHeader(64, 64), make([]byte, MaxBytes)...)
	tests := []struct {
		name string
		ct   string
		body []byte
		want bool
	}{
		{"png", "image/png", pngHeader(64, 64), true},
		{"png with charset parameter", "image/png; charset=binary", pngHeader(64, 64), true},
		{"webp", "image/webp", webpVP8L(300, 200), true},
		{"html served as png", "image/png", []byte("<html><script>alert(1)</script></html>"), false},
		{"svg as image/svg+xml", "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), false},
		{"svg bytes labelled png", "image/png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), false},
		{"png bytes labelled jpeg", "image/jpeg", pngHeader(64, 64), false},
		{"png bytes labelled html", "text/html", pngHeader(64, 64), false},
		{"no content type", "", pngHeader(64, 64), false},
		{"png 60000x60000", "image/png", pngHeader(60000, 60000), false},
		{"png 4097 wide", "image/png", pngHeader(4097, 10), false},
		{"png exactly 4096", "image/png", pngHeader(4096, 4096), true},
		{"webp VP8X ok", "image/webp", webpVP8X(300, 200, 300, 200), true},
		{"webp VP8X small canvas hides a big frame", "image/webp", webpVP8X(100, 100, 16383, 16383), false},
		{"webp VP8X canvas over the cap", "image/webp", webpVP8X(5000, 100, 100, 100), false},
		{"webp VP8X without a frame chunk", "image/webp", webpVP8X(10, 10, 10, 10)[:30], false},
		{"webp 60000x60000", "image/webp", webpVP8L(16384, 16384), false},
		{"truncated png", "image/png", pngHeader(64, 64)[:12], false},
		{"over 5 MiB", "image/png", big, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, serve(tt.ct, tt.body), publicHost())
			if got := r.f.Fetch(context.Background(), coverURL); got != tt.want || r.stored(coverURL) != tt.want {
				t.Fatalf("Fetch %v, stored %v, want %v", got, r.stored(coverURL), tt.want)
			}
		})
	}
	t.Run("non-200", func(t *testing.T) {
		r := newRig(t, http.NotFoundHandler(), publicHost())
		if r.f.Fetch(context.Background(), coverURL) {
			t.Fatal("a 404 body must not be stored")
		}
	})
}

func TestFetchSendsTheGenericUserAgent(t *testing.T) {
	var got string
	r := newRig(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		got = req.Header.Get("User-Agent")
		serve("image/png", pngHeader(8, 8)).ServeHTTP(w, req)
	}), publicHost())
	if !r.f.Fetch(context.Background(), coverURL) || got != mothdex.DefaultUserAgent("") {
		t.Fatalf("User-Agent %q", got)
	}
}

func TestBlockedIPTable(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.0.1", "169.254.1.1", "100.64.0.1", "0.0.0.0", "224.0.0.1",
		"255.255.255.255", "198.18.0.1", "::1", "::", "fe80::1", "fc00::1", "ff02::1", "::ffff:10.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "0.1.2.3", "fec0::1", "::7f00:1"} {
		if !blockedIP(netip.MustParseAddr(s)) {
			t.Errorf("%s must be blocked", s)
		}
	}
	for _, s := range []string{publicIP, "8.8.8.8", "2606:4700::1111"} {
		if blockedIP(netip.MustParseAddr(s)) {
			t.Errorf("%s must be allowed", s)
		}
	}
}

// The dialer Control hook is the last line: it refuses a blocked address even when the
// resolver step is bypassed. The target is a live listener on loopback, so only the hook can
// be what refuses the connection.
func TestControlHookRefusesBlockedAddresses(t *testing.T) {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	c, err := defaultDialer().DialContext(context.Background(), "tcp", l.Addr().String())
	if err == nil {
		_ = c.Close()
	}
	if !errors.Is(err, errBlocked) {
		t.Fatalf("dial to loopback: %v, want errBlocked", err)
	}
	if err := control("tcp", publicIP+":443", nil); err != nil {
		t.Fatalf("a public address must pass: %v", err)
	}
}

// Production wiring: no proxy (it would skip the guard), TLS 1.2 floor, and the default dialer
// carries the Control hook.
func TestProductionTransportHasNoProxyAndTLSFloor(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	f, err := New(Options{Resolver: publicHost(), Cache: openStore(t)})
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := f.client.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("the cover transport must not use a proxy")
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("TLS policy %+v", tr.TLSClientConfig)
	}
	if defaultDialer().Control == nil {
		t.Fatal("the default dialer needs the Control hook")
	}
}

func openStore(t *testing.T) cache.Store {
	t.Helper()
	s, err := cache.Open(filepath.Join(t.TempDir(), "cache"), cache.Config{Enabled: true}, clock{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
