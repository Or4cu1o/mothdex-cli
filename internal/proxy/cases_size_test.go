package proxy_test

// T-27 (downloader.md 3.2): Content-Length is the size authority. Cases 31 (strict length), 32b, 32c,
// 32d, 35 and 36 of T-16, as changed by T-27.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

// stubDoer answers every request with a fixed response, for headers a real HTTP server would not send.
type stubDoer struct {
	status int
	header http.Header
	cl     int64
	body   func() io.Reader
	err    error
	hits   atomic.Int32
	read   atomic.Int64
}

func (d *stubDoer) Do(*http.Request) (*http.Response, error) {
	d.hits.Add(1)
	if d.err != nil {
		return nil, d.err
	}
	var rd io.Reader = strings.NewReader("")
	if d.body != nil {
		rd = d.body()
	}
	return &http.Response{StatusCode: d.status, Header: d.header, ContentLength: d.cl, Body: io.NopCloser(&countReader{rd, &d.read})}, nil
}

type countReader struct {
	r io.Reader
	n *atomic.Int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

type zeroes struct{}

func (zeroes) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func (r *rig) useDoer(cfg proxy.Config, d proxy.HTTPDoer) {
	r.rebuild(cfg, func(o *proxy.Options) { o.HTTP = d })
}

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return ctx, cancel
}

// Case 31 (T-27): a missing, chunked, zero or malformed Content-Length is size_mismatch, not
// retried, with no body byte read.
func TestCase31StrictContentLength(t *testing.T) {
	body := fakeproxy.Pattern(1000)
	t.Run("missing or chunked", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		var read atomic.Int64
		r.rebuild(proxy.Config{}, func(o *proxy.Options) { o.HTTP = countingClient(r.srv.Client(), &read) })
		r.srv.Script("/f/n", fakeproxy.NoContentLength(body))
		wantFailed(t, r.download(r.job("/f/n", "n.zip", 1000)), proxy.CodeSizeMismatch)
		if n := len(r.srv.Requests()); n != 1 || read.Load() != 0 {
			t.Fatalf("%d requests, %d body bytes read: must fail once and read nothing", n, read.Load())
		}
		noFinalFile(t, r)
	})
	t.Run("zero", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		r.srv.Script("/f/z", fakeproxy.LengthMismatch(nil, 0))
		wantFailed(t, r.download(r.job("/f/z", "z.zip", 1000)), proxy.CodeSizeMismatch)
		if n := len(r.srv.Requests()); n != 1 {
			t.Fatalf("%d requests", n)
		}
	})
	for name, raw := range map[string]string{"plus sign": "+5", "leading blank": " 5", "decimal": "5.0", "list": "5, 5", "hex": "0x5", "leading zero": "05"} {
		t.Run("malformed "+name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			d := &stubDoer{status: 200, header: http.Header{"Content-Length": {raw}}, cl: 5, body: func() io.Reader { return strings.NewReader("12345") }}
			r.useDoer(proxy.Config{}, d)
			wantFailed(t, r.download(r.job("/f/m", "m.zip", 5)), proxy.CodeSizeMismatch)
			if d.hits.Load() != 1 || d.read.Load() != 0 {
				t.Fatalf("%d requests, %d bytes read", d.hits.Load(), d.read.Load())
			}
		})
	}
	t.Run("rejected by the transport", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		d := &stubDoer{err: errors.New(`net/http: bad Content-Length "+5"`)}
		r.useDoer(proxy.Config{}, d)
		wantFailed(t, r.download(r.job("/f/m", "m.zip", 5)), proxy.CodeSizeMismatch)
		if d.hits.Load() != 1 {
			t.Fatalf("%d requests: a malformed length is not retried", d.hits.Load())
		}
	})
	t.Run("one byte beyond Content-Length aborts at cl+1", func(t *testing.T) {
		r := newRig(t, proxy.Config{})
		d := &stubDoer{status: 200, header: http.Header{"Content-Length": {"1000"}}, cl: 1000,
			body: func() io.Reader { return io.LimitReader(zeroes{}, 10<<20) }}
		r.useDoer(proxy.Config{}, d)
		wantFailed(t, r.download(r.job("/f/o", "o.zip", 1000)), proxy.CodeSizeMismatch)
		if got := d.read.Load(); got > 1<<20 {
			t.Fatalf("read %d bytes of a 1000-byte file: it must abort at byte cl+1", got)
		}
		stagingEmpty(t, r)
		noFinalFile(t, r)
	})
}

// Case 32b: Content-Length above download.max_file_size, with a small link.size, fails before the body.
func TestCase32bContentLengthAboveTheCap(t *testing.T) {
	cfg := proxy.Config{MaxFileSize: 1 << 20}
	r := newRig(t, cfg)
	var read atomic.Int64
	r.rebuild(cfg, func(o *proxy.Options) { o.HTTP = countingClient(r.srv.Client(), &read) })
	r.srv.Script("/f/c", fakeproxy.OK(fakeproxy.Pattern(2<<20)))
	wantFailed(t, r.download(r.job("/f/c", "c.zip", 1000)), proxy.CodeSizeMismatch)
	if read.Load() != 0 {
		t.Fatalf("%d body bytes read before the cap check", read.Load())
	}
	noFinalFile(t, r)
}

// Case 32c: attempt 1 is cut short with Content-Length N, attempt 2 answers N+1: the file changed
// upstream, size_mismatch, no third attempt.
func TestCase32cContentLengthIsPinnedAcrossRetries(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.srv.Script("/f/p", fakeproxy.Truncate(fakeproxy.Pattern(1000), 400), fakeproxy.OK(fakeproxy.Pattern(1001)))
	res := r.download(r.job("/f/p", "p.zip", 1000))
	wantFailed(t, res, proxy.CodeSizeMismatch)
	if n := len(r.srv.Requests()); n != 2 {
		t.Fatalf("%d requests, want 2", n)
	}
	if res.ContentLength != 1000 {
		t.Fatalf("the pinned length is %d", res.ContentLength)
	}
	noFinalFile(t, r)
}

// Case 32d: confirmation at the real size, before the first body byte.
func TestCase32dConfirmationAtTheRealSize(t *testing.T) {
	cfg := proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}
	body := fakeproxy.Pattern(20_000)
	run := func(t *testing.T, consent int64, served []byte) (proxy.Result, *rig, *atomic.Int64) {
		t.Helper()
		r := newRig(t, cfg)
		var read atomic.Int64
		r.rebuild(cfg, func(o *proxy.Options) { o.HTTP = countingClient(r.srv.Client(), &read) })
		r.srv.Script("/f/b", fakeproxy.OK(served))
		job := r.job("/f/b", "b.zip", 1000) // the catalog says 1000 bytes, below the threshold
		job.SizeConsent = consent
		return r.download(job), r, &read
	}
	t.Run("default rule: confirmation_required, no body byte", func(t *testing.T) {
		res, r, read := run(t, 0, body)
		wantFailed(t, res, proxy.CodeConfirmationRequired)
		if read.Load() != 0 || res.ContentLength != 20_000 || len(r.srv.Requests()) != 1 {
			t.Fatalf("read %d, cl %d, %d requests", read.Load(), res.ContentLength, len(r.srv.Requests()))
		}
		r.evMu.Lock()
		last := r.events[len(r.events)-1]
		r.evMu.Unlock()
		if last.Expected != 20_000 {
			t.Fatalf("the terminal event must carry the real size, got %d", last.Expected)
		}
		noFinalFile(t, r)
	})
	t.Run("the exact consent downloads", func(t *testing.T) {
		if res, _, _ := run(t, 20_000, body); res.State != proxy.StateDone {
			t.Fatalf("%+v err %v", res, res.Err)
		}
	})
	t.Run("a consent for another size asks again", func(t *testing.T) {
		res, _, read := run(t, 20_000, fakeproxy.Pattern(30_000))
		wantFailed(t, res, proxy.CodeConfirmationRequired)
		if read.Load() != 0 {
			t.Fatalf("%d body bytes read", read.Load())
		}
	})
	t.Run("ConsentAny skips it", func(t *testing.T) {
		if res, _, _ := run(t, proxy.ConsentAny, body); res.State != proxy.StateDone {
			t.Fatalf("%+v err %v", res, res.Err)
		}
	})
	t.Run("confirm_downloads off, or a catalog size already above the threshold, need nothing", func(t *testing.T) {
		r := newRig(t, proxy.Config{ConfirmBytes: 10_000})
		r.srv.Script("/f/b", fakeproxy.OK(body))
		if res := r.download(r.job("/f/b", "b.zip", 1000)); res.State != proxy.StateDone {
			t.Fatalf("off: %+v", res)
		}
		r = newRig(t, cfg)
		r.srv.Script("/f/b", fakeproxy.OK(body))
		if res := r.download(r.job("/f/b", "b.zip", 15_000)); res.State != proxy.StateDone {
			t.Fatalf("the catalog already crossed the threshold, the batch prompt covered it: %+v", res)
		}
	})
}

// Case 35 (T-27): the claim made for link.size is swapped for Content-Length before the body.
func TestCase35ClaimFollowsContentLength(t *testing.T) {
	t.Run("a larger Content-Length fails disk_full before the first body byte", func(t *testing.T) {
		r := newRig(t, proxy.Config{Concurrency: 2})
		r.fs.SetVolume(".", 1000*mib, 1)
		r.srv.Script("/f/a", fakeproxy.StallBeforeHeaders(fakeproxy.Pattern(1000), time.Hour))
		r.srv.Script("/f/b", fakeproxy.LengthMismatch(fakeproxy.Pattern(1000), 600*mib))
		ctx, cancel := contextWithCancel(t)
		first := make(chan proxy.Result, 1)
		go func() { first <- r.dl.Download(ctx, r.job("/f/a", "a.zip", 1000)) }() // holds about 256 MiB
		r.clock.BlockUntil(t, 1)

		res := r.download(r.job("/f/b", "b.zip", 1000)) // 1000 bytes says the catalog, 600 MiB says the proxy
		wantFailed(t, res, proxy.CodeDiskFull)
		if res.Bytes != 0 {
			t.Fatalf("%d body bytes written before the space check", res.Bytes)
		}
		cancel()
		<-first
	})
	t.Run("a smaller Content-Length shrinks the claim", func(t *testing.T) {
		r := newRig(t, proxy.Config{Concurrency: 2})
		transferring := make(chan struct{}, 1)
		r.rebuild(proxy.Config{Concurrency: 2}, func(o *proxy.Options) {
			prev := o.OnEvent
			o.OnEvent = func(e proxy.Event) {
				prev(e)
				if e.State == proxy.StateTransferring {
					select {
					case transferring <- struct{}{}:
					default:
					}
				}
			}
		})
		r.fs.SetVolume(".", 700*mib, 1)
		// A is claimed as 400 MiB by the catalog (about 656 MiB with the margin) and is really 1000 bytes.
		r.srv.Script("/f/a", fakeproxy.StallMidStream(fakeproxy.Pattern(1000), 500, time.Hour))
		r.srv.Script("/f/b", fakeproxy.OK(fakeproxy.Pattern(1000)))
		ctx, cancel := contextWithCancel(t)
		first := make(chan proxy.Result, 1)
		go func() { first <- r.dl.Download(ctx, r.job("/f/a", "a.zip", int(400*mib))) }()
		<-transferring // A passed its headers (the claim shrank) and is streaming; it stalls mid-body

		// B needs about 406 MiB by the catalog: it only fits because A's claim shrank.
		if res := r.download(r.job("/f/b", "b.zip", int(150*mib))); res.State != proxy.StateDone {
			t.Fatalf("B: %+v err %v", res, res.Err)
		}
		cancel()
		<-first
	})
}

// Case 36 (T-27): the same file-system split reached only through Content-Length.
func TestCase36SplitReachedThroughContentLength(t *testing.T) {
	r := newRig(t, proxy.Config{})
	r.fs.SetVolume("downloads", 100<<30, 1)
	r.fs.SetVolume("roms", 300*mib, 2) // fits the 1000-byte estimate (about 256 MiB), not 100 MiB plus margin
	r.srv.Script("/f/a", fakeproxy.LengthMismatch(fakeproxy.Pattern(1000), 100*mib))
	res := r.download(r.job("/f/a", "a.zip", 1000))
	wantFailed(t, res, proxy.CodeDiskFull)
	if res.Bytes != 0 {
		t.Fatalf("%d body bytes written", res.Bytes)
	}
}

// Sentinela S2: netFailure recognises a malformed length by the wording of Go's transport error. This
// pins that wording against the Go version of go.mod: if a Go release changes it, this fails instead of
// the downloader silently retrying malformed lengths as network errors.
func TestGoTransportWordingForAMalformedContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: +5\r\n\r\n12345")
		_ = buf.Flush()
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL) //nolint:gosec,noctx // a local test server
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Go accepted a Content-Length of +5")
	}
	if !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("the transport error no longer names Content-Length: %v", err)
	}
}
