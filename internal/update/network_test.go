package update

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type routeTransport struct {
	base     http.RoundTripper
	server   *url.URL
	requests *atomic.Int64
	t        *testing.T
}

func (r routeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.requests.Add(1)
	if !strings.HasPrefix(req.URL.String(), origin) {
		r.t.Errorf("unexpected origin: %s", req.URL)
	}
	if req.Header.Get("User-Agent") != "mothdex" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.URL.RawQuery != "" {
		r.t.Error("request disclosed private data")
	}
	cp := req.Clone(req.Context())
	u := *req.URL
	u.Scheme = r.server.Scheme
	u.Host = r.server.Host
	cp.URL = &u
	return r.base.RoundTrip(cp)
}
func serve(t *testing.T, u *Updater, h http.HandlerFunc) *atomic.Int64 {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	addr, _ := url.Parse(s.URL)
	n := new(atomic.Int64)
	u.o.Client = newClient(&http.Client{Transport: routeTransport{s.Client().Transport, addr, n, t}})
	return n
}
func TestDiscovery(t *testing.T) {
	for _, tc := range []struct{ name, channel, location, feed, want, code string }{
		{"stable", "stable", origin + "/tag/v2.0.0", "", "2.0.0", ""},
		{"wrong repo", "stable", "https://github.com/evil/repo/releases/tag/v2.0.0", "", "", "integrity"},
		{"http", "stable", "http://github.com/Or4cu1o/mothdex-cli/releases/tag/v2.0.0", "", "", "integrity"},
		{"beta", "beta", origin + "/tag/v2.0.0", `<feed><entry><link href="` + origin + `/tag/v2.1.0-beta.2"/></entry><entry><link href="` + origin + `/tag/v2.1.0-beta.10"/></entry><entry><link href="` + origin + `/tag/v1.0.0"/></entry></feed>`, "2.1.0-beta.10", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := fixture(t)
			u.o.Channel = tc.channel
			serve(t, u, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/latest") {
					w.Header().Set("Location", tc.location)
					w.WriteHeader(302)
				} else {
					_, _ = w.Write([]byte(tc.feed))
				}
			})
			r, err := u.Latest(context.Background())
			if tc.code != "" {
				errorCode(t, err, tc.code, "")
			} else if err != nil || r.Version != tc.want {
				t.Fatalf("got %+v, %v", r, err)
			}
		})
	}
}
func TestRedirectAndBounds(t *testing.T) {
	for _, tc := range []struct{ name, location string }{{"http", "http://github.com/x"}, {"host", "https://evil.example/x"}, {"userinfo", "https://x@github.com/x"}, {"port", "https://github.com:444/x"}} {
		t.Run(tc.name, func(t *testing.T) {
			u, _ := fixture(t)
			serve(t, u, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", tc.location)
				w.WriteHeader(302)
			})
			_, _, err := u.request(context.Background(), "/download/v2.0.0/checksums.txt", 64<<10, false)
			errorCode(t, err, "untrusted_redirect", "")
		})
	}
	t.Run("oversize", func(t *testing.T) {
		u, _ := fixture(t)
		serve(t, u, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "66560")
			_, _ = w.Write(make([]byte, 65<<10))
		})
		_, _, err := u.request(context.Background(), "/download/v2.0.0/checksums.txt", 64<<10, false)
		errorCode(t, err, "size_mismatch", "")
	})
}

type fakeIdleTimer struct{ resets int }

func (t *fakeIdleTimer) Reset(time.Duration) bool { t.resets++; return true }
func (*fakeIdleTimer) Stop() bool                 { return true }

type updateTransportFunc func(*http.Request) (*http.Response, error)

func (f updateTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type stalledUpdateBody struct{ ctx context.Context }

func (r stalledUpdateBody) Read([]byte) (int, error) { <-r.ctx.Done(); return 0, r.ctx.Err() }
func (stalledUpdateBody) Close() error               { return nil }

// Fire the idle timer deterministically: no clock wait and no external server.
func TestRequestIdleTimeoutAndReset(t *testing.T) {
	u, _ := fixture(t)
	callbacks := make(chan func(), 1)
	timer := &fakeIdleTimer{}
	u.startTimer = func(_ time.Duration, fire func()) idleTimer { callbacks <- fire; return timer }
	reading := make(chan struct{})
	u.o.Client = newClient(&http.Client{Transport: updateTransportFunc(func(r *http.Request) (*http.Response, error) {
		close(reading)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: stalledUpdateBody{r.Context()}, ContentLength: -1, Request: r}, nil
	})})
	finished := make(chan error, 1)
	go func() { _, _, err := u.request(context.Background(), "/fixture", 1024, false); finished <- err }()
	fire := <-callbacks
	<-reading
	fire()
	errorCode(t, <-finished, "network", "")
	r := idleReader{r: strings.NewReader("chunk"), timer: timer}
	if _, err := io.ReadAll(&r); err != nil || timer.resets != 1 {
		t.Fatalf("idle timer not reset: %d %v", timer.resets, err)
	}
}
