package proxy_test

// T-16 catalog cases for the network and URL-policy groups (26, 27, 29 to 33). Each test names
// the case it covers; internal/testutil/fakeproxy/catalog.go maps case to test.

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakefs"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/realfs"
)

// rebuild replaces the downloader of a rig, for tests that need another HTTP client, ids or
// file systems. edit may change any option before New.
func (r *rig) rebuild(cfg proxy.Config, edit func(*proxy.Options)) {
	r.t.Helper()
	if cfg.Platforms == nil {
		cfg.Platforms = map[string]bool{"snes": true, "ps1": true}
	}
	o := proxy.Options{
		HTTP: r.srv.Client(), Clock: r.clock, Sleeper: r.clock, Rand: zeroRand{},
		Staging: r.fs, Dest: r.fs, Finalizer: fakefs.Finalizer{},
		Confine: func(dir string) (string, error) { return dir, r.fs.MkdirAll(dir, 0o700) },
		History: r.history, Journal: r.journal, Config: cfg, DestRoot: r.fs.Dir(),
		OnEvent: func(e proxy.Event) { r.evMu.Lock(); r.events = append(r.events, e); r.evMu.Unlock() },
	}
	if edit != nil {
		edit(&o)
	}
	dl, err := proxy.New(o)
	if err != nil {
		r.t.Fatal(err)
	}
	r.dl = dl
}

// wantFailed asserts a Failed result with the given code.
func wantFailed(t *testing.T, res proxy.Result, code proxy.Code) {
	t.Helper()
	if res.State != proxy.StateFailed || res.Err == nil || res.Err.Code != code {
		t.Fatalf("result state %s err %v, want failed with %s", res.State, res.Err, code)
	}
}

// noFinalFile asserts that nothing was finalized: the destination tree holds no file at all.
func noFinalFile(t *testing.T, r *rig) {
	t.Helper()
	for name, kind := range realfs.Snapshot(t, r.fs.Dir()) {
		if len(name) > 5 && name[:5] == "roms/" && kind != "dir" {
			t.Fatalf("a file was finalized: %s (%s)", name, kind)
		}
	}
}

// Case 26: URL policy table; no connection for a rejected URL, and a normalised host passes.
func TestCase26URLPolicyThroughTheDownloader(t *testing.T) {
	rejected := map[string]string{ //nolint:gosec // G101: the userinfo URL is a rejected input, not a credential
		"http":        "http://proxy.mothdex.net/f/a",
		"port":        "https://proxy.mothdex.net:8443/f/a",
		"userinfo":    "https://user:pw@proxy.mothdex.net/f/a",
		"evil host":   "https://evil.example/f/a",
		"ipv4":        "https://127.0.0.1/f/a",
		"ipv6":        "https://[::1]/f/a",
		"suffix host": "https://proxy.mothdex.net.evil.example/f/a",
		"prefix host": "https://xproxy.mothdex.net/f/a",
		"idn":         "https://prøxy.mothdex.net/f/a",
		"empty":       "",
	}
	for name, url := range rejected {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			r.srv.Script(fakeproxy.AnyPath, fakeproxy.OK(fakeproxy.Pattern(10)))
			job := r.job("/f/a", "a.zip", 10)
			job.Link.URL = url
			wantFailed(t, r.download(job), proxy.CodeUntrustedURL)
			if r.srv.Connections() != 0 {
				t.Fatalf("%d connections for a rejected URL", r.srv.Connections())
			}
		})
	}
	for name, url := range map[string]string{
		"trailing dot": "https://proxy.mothdex.net./f/a",
		"upper case":   "https://PROXY.MOTHDEX.NET/f/a",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			r.srv.Script("/f/a", fakeproxy.OK(fakeproxy.Pattern(10)))
			job := r.job("/f/a", "a.zip", 10)
			job.Link.URL = url
			if res := r.download(job); res.State != proxy.StateDone {
				t.Fatalf("state %s err %v", res.State, res.Err)
			}
		})
	}
}

// Case 27: a redirect, even to an allowed host, is untrusted and is not retried.
func TestCase27RedirectsAreNeverFollowedNorRetried(t *testing.T) {
	for name, loc := range map[string]string{
		"to an allowed host": "https://proxy.mothdex.net/f/other",
		"to an evil host":    "https://evil.example/f/x",
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			r.srv.Script("/f/a", fakeproxy.Redirect(302, loc))
			r.srv.Script("/f/other", fakeproxy.OK(fakeproxy.Pattern(10)))
			res := r.download(r.job("/f/a", "a.zip", 10))
			wantFailed(t, res, proxy.CodeUntrustedURL)
			if n := len(r.srv.Requests()); n != 1 {
				t.Fatalf("%d requests, want exactly the first (no follow, no retry)", n)
			}
		})
	}
}

// Case 29 (downloader side): a link from a poisoned cache is validated like one from the
// network, field by field. The cache layer itself is not part of this package.
func TestCase29HostileLinkFieldsGetTheSameRejections(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.srv.Script(fakeproxy.AnyPath, fakeproxy.OK(fakeproxy.Pattern(10)))

	evilURL := r.job("/f/a", "a.zip", 10)
	evilURL.Link.URL = "https://evil.example/f/a"
	wantFailed(t, r.download(evilURL), proxy.CodeUntrustedURL)

	exe := r.job("/f/a", "../../.config/autostart/run.desktop", 10)
	wantFailed(t, r.download(exe), proxy.CodeUnsafeName)

	badPlatform := r.job("/f/a", "a.zip", 10)
	badPlatform.Platform = "../../system"
	wantFailed(t, r.download(badPlatform), proxy.CodePathRejected)

	if r.srv.Connections() != 0 {
		t.Fatalf("%d connections for hostile links", r.srv.Connections())
	}
	noFinalFile(t, r)
}

// Case 30: a body with Content-Encoding: gzip is refused although the request said identity.
func TestCase30GzipDespiteIdentityIsAnIntegrityFailure(t *testing.T) {
	r := newRig(t, proxy.Config{})
	body := fakeproxy.Pattern(5000)
	r.srv.Script("/f/g", fakeproxy.GzipDespiteIdentity(body))
	res := r.download(r.job("/f/g", "g.zip", len(body)))
	wantFailed(t, res, proxy.CodeIntegrity)
	noFinalFile(t, r)
	reqs := r.srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the server")
	}
	if got := reqs[0].Header.Get("Accept-Encoding"); got != "identity" {
		t.Fatalf("Accept-Encoding = %q, want identity", got)
	}
}

// Case 31: length and size rules, and a server-chosen name that is never used.
func TestCase31LengthMismatchOversizeAndDisposition(t *testing.T) {
	t.Run("Content-Length differs from the link size", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		body := fakeproxy.Pattern(1000)
		r.srv.Script("/f/l", fakeproxy.LengthMismatch(body, 2000))
		wantFailed(t, r.download(r.job("/f/l", "l.zip", len(body))), proxy.CodeSizeMismatch)
		if n := len(r.srv.Requests()); n != 1 {
			t.Fatalf("%d requests, a size mismatch must not be retried", n)
		}
	})
	t.Run("no Content-Length and one byte too many", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		r.srv.Script("/f/o", fakeproxy.Oversized(1001))
		wantFailed(t, r.download(r.job("/f/o", "o.zip", 1000)), proxy.CodeSizeMismatch)
		if entries, _ := os.ReadDir(r.fs.Path("downloads")); len(entries) != 0 {
			t.Fatalf("staging not removed: %v", entries)
		}
		noFinalFile(t, r)
	})
	t.Run("Content-Disposition is ignored", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		body := fakeproxy.Pattern(1000)
		r.srv.Script("/f/d", fakeproxy.MaliciousDisposition(body, `attachment; filename="../../etc/cron.d/x"`))
		res := r.download(r.job("/f/d", "d.zip", len(body)))
		if res.State != proxy.StateDone || res.Name != "d.zip" {
			t.Fatalf("state %s name %q err %v", res.State, res.Name, res.Err)
		}
		if _, err := os.Lstat(r.fs.Path("roms/snes/d.zip")); err != nil {
			t.Fatal(err)
		}
	})
}

// Case 32: hostile sizes fail in Preflight, before any connection.
func TestCase32HostileSizesFailBeforeAnyConnection(t *testing.T) {
	r := newRig(t, proxy.Config{MaxFileSize: 1 << 20})
	r.srv.Script(fakeproxy.AnyPath, fakeproxy.OK(fakeproxy.Pattern(10)))
	for _, size := range []int64{0, -1, math.MinInt64, math.MaxInt64, 1<<20 + 1} {
		job := r.job("/f/a", "a.zip", 10)
		job.Link.Size = size
		res := r.download(job)
		if res.State != proxy.StateFailed || res.Err == nil {
			t.Fatalf("size %d: state %s", size, res.State)
		}
	}
	if r.srv.Connections() != 0 {
		t.Fatalf("%d connections for hostile sizes", r.srv.Connections())
	}
	noFinalFile(t, r)
}

// Case 33: a certificate that does not chain to the trust store is never accepted.
func TestCase33SelfSignedCertificateIsNeverFinalized(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.rebuild(proxy.Config{}, func(o *proxy.Options) { o.HTTP = r.srv.UntrustedClient() })
	body := fakeproxy.Pattern(1000)
	r.srv.Script("/f/c", fakeproxy.OK(body))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	res := r.dl.Download(ctx, r.job("/f/c", "c.zip", len(body)))
	if res.State == proxy.StateDone {
		t.Fatal("a download over an untrusted certificate must not succeed")
	}
	var pe *proxy.Error
	if !errors.As(res.Err, &pe) || pe.Code != proxy.CodeNetwork {
		t.Fatalf("err %v, want a network failure", res.Err)
	}
	if len(r.srv.Requests()) != 0 {
		t.Fatal("the handler must never be reached when the handshake fails")
	}
	noFinalFile(t, r)
}
