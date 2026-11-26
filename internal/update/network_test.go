package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
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
		serve(t, u, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, 65<<10)) })
		_, _, err := u.request(context.Background(), "/download/v2.0.0/checksums.txt", 64<<10, false)
		errorCode(t, err, "size_mismatch", "")
	})
}
