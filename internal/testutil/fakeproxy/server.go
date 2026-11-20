package fakeproxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeclock"
)

// AnyPath is the script key used for paths that have no script of their own.
const AnyPath = "*"

// Request is one request the server saw.
type Request struct {
	Method  string
	Path    string
	Header  http.Header
	Attempt int // 1-based count of requests to this path
}

// Server is a scripted TLS proxy double.
type Server struct {
	*httptest.Server
	t     testing.TB
	clock *fakeclock.Clock
	done  chan struct{}

	closeOnce sync.Once

	mu         sync.Mutex
	scripts    map[string][]Step
	hits       map[string]int
	log        []Request
	conns      int
	transports []*http.Transport
}

// New starts the server and closes it with the test.
func New(t testing.TB, clock *fakeclock.Clock) *Server {
	t.Helper()
	s := &Server{
		t: t, clock: clock, done: make(chan struct{}),
		scripts: map[string][]Step{}, hits: map[string]int{},
	}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(s.handle))
	s.Config.ConnState = func(_ net.Conn, st http.ConnState) {
		if st == http.StateNew {
			s.mu.Lock()
			s.conns++
			s.mu.Unlock()
		}
	}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

// Script sets the steps for path. Each request consumes the next step; the
// last step repeats once the list is exhausted. Use AnyPath for a fallback.
func (s *Server) Script(path string, steps ...Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scripts[path] = steps
	s.hits[path] = 0
}

// Requests returns a copy of every request seen so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.log...)
}

// Connections returns how many TCP connections were accepted, so tests can
// assert that a rejected URL never reached the network.
func (s *Server) Connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

// Transport returns a transport that trusts the server certificate, sends
// every connection to the fake whatever the URL host, never uses a proxy and
// never negotiates compression (as the downloader contract requires).
func (s *Server) Transport() *http.Transport {
	pool := x509.NewCertPool()
	pool.AddCert(s.Certificate())
	return s.transport(pool)
}

// UntrustedTransport is Transport without the server certificate in its root
// pool, so every TLS handshake fails verification (T-16 case 33).
func (s *Server) UntrustedTransport() *http.Transport {
	return s.transport(x509.NewCertPool())
}

func (s *Server) transport(pool *x509.CertPool) *http.Transport {
	addr := s.Listener.Addr().String()
	tr := &http.Transport{
		Proxy:              nil,
		DisableCompression: true,
		TLSClientConfig:    &tls.Config{RootCAs: pool, ServerName: "example.com", MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	s.mu.Lock()
	s.transports = append(s.transports, tr)
	s.mu.Unlock()
	return tr
}

// Client wraps Transport and returns 3xx responses instead of following them.
func (s *Server) Client() *http.Client { return s.client(s.Transport()) }

// UntrustedClient wraps UntrustedTransport the same way.
func (s *Server) UntrustedClient() *http.Client { return s.client(s.UntrustedTransport()) }

func (s *Server) client(tr http.RoundTripper) *http.Client {
	return &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Close releases stalled handlers, closes idle client connections and shuts
// the server down. It is safe to call more than once and from the test itself;
// it is also registered with t.Cleanup.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.done) // release stalled handlers first: Server.Close waits for them
		s.Server.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, tr := range s.transports {
			tr.CloseIdleConnections()
		}
	})
}

func (s *Server) next(r *http.Request) (Step, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.URL.Path
	steps, ok := s.scripts[key]
	if !ok {
		steps, ok = s.scripts[AnyPath]
	}
	s.hits[key]++
	s.log = append(s.log, Request{
		Method: r.Method, Path: key, Header: r.Header.Clone(), Attempt: s.hits[key],
	})
	if !ok || len(steps) == 0 {
		return Step{}, false
	}
	idx := s.hits[key] - 1
	if idx >= len(steps) {
		idx = len(steps) - 1
	}
	return steps[idx], true
}

// wait blocks for d of fake time; false means the request or server ended.
func (s *Server) wait(r *http.Request, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer, stop := s.clock.NewTimer(d)
	defer stop() // an abandoned wait must not stay pending on the clock
	select {
	case <-timer:
		return true
	case <-r.Context().Done():
	case <-s.done:
	}
	return false
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	step, ok := s.next(r)
	if !ok {
		s.t.Errorf("fakeproxy: no script for %s", r.URL.Path)
		http.Error(w, "unscripted", http.StatusInternalServerError)
		return
	}
	if !s.wait(r, step.StallBeforeHeaders) {
		return
	}
	body := step.Body
	if step.Gzip {
		body = gzipBytes(body)
		w.Header().Set("Content-Encoding", "gzip")
	}
	for k, v := range step.Header {
		w.Header()[k] = v
	}
	switch {
	case step.DeclaredLength == nil:
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	case *step.DeclaredLength >= 0:
		w.Header().Set("Content-Length", strconv.FormatInt(*step.DeclaredLength, 10))
	}
	status := step.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	flush(w)
	if !s.wait(r, step.StallBeforeFirstByte) {
		return
	}
	s.stream(w, r, step, body)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request, step Step, body []byte) {
	if step.StallFor > 0 && step.StallAt < len(body) {
		_, _ = w.Write(body[:step.StallAt])
		flush(w)
		if !s.wait(r, step.StallFor) {
			return
		}
		body = body[step.StallAt:]
	}
	if step.Truncate && step.TruncateAt < len(body) {
		_, _ = w.Write(body[:step.TruncateAt])
		flush(w)
		panic(http.ErrAbortHandler) // drop the connection mid-body
	}
	_, _ = w.Write(body)
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func gzipBytes(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Bytes()
}
