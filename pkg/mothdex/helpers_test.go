package mothdex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureDir = "testdata/api-v1"

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeSleeper records every wait and advances the fake clock instead of sleeping.
type fakeSleeper struct {
	clock *fakeClock
	mu    sync.Mutex
	waits []time.Duration
}

func (s *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.waits = append(s.waits, d)
	s.mu.Unlock()
	s.clock.Advance(d)
	return nil
}

func (s *fakeSleeper) Waits() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

// withJitter injects the jitter function (tests only).
func withJitter(f func(max time.Duration) time.Duration) Option {
	return func(c *config) { c.jitter = f }
}

func maxJitter(max time.Duration) time.Duration { return max }

// harness bundles a client with its fakes and a request counter.
type harness struct {
	client  *Client
	clock   *fakeClock
	sleeper *fakeSleeper
	hits    *atomic.Int32
}

// newHarness starts an httptest server with handler and builds a client for it.
// Rate limiting is wide open unless opts override it.
func newHarness(t *testing.T, handler http.Handler, opts ...Option) harness {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	clock := newFakeClock()
	sleeper := &fakeSleeper{clock: clock}
	all := append([]Option{
		WithBaseURL(srv.URL + "/v1"),
		WithClock(clock), WithSleeper(sleeper),
		WithRateLimit(10, 100), withJitter(maxJitter),
	}, opts...)
	c, err := New(all...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return harness{client: c, clock: clock, sleeper: sleeper, hits: &hits}
}

// fixtureRow is the manifest row used by the fixture server.
type fixtureRow struct {
	File   string `json:"file"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
}

func loadManifest(t *testing.T) map[string]fixtureRow {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m struct {
		Fixtures []fixtureRow `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	rows := make(map[string]fixtureRow, len(m.Fixtures))
	for _, r := range m.Fixtures {
		rows[strings.TrimSuffix(r.File, ".json")] = r
	}
	return rows
}

func fixtureBody(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtureDir, name+".json")) //nolint:gosec // G304: test-controlled fixture name
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// serveFixture answers with a recorded body and status, checking method and path.
// seen, when not nil, receives the request body.
func serveFixture(t *testing.T, name string, seen *[]byte) http.Handler {
	t.Helper()
	row := loadManifest(t)[name]
	body := fixtureBody(t, name)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != row.Method || r.URL.Path != "/v1"+row.Path {
			t.Errorf("%s: got %s %s, want %s /v1%s", name, r.Method, r.URL.Path, row.Method, row.Path)
		}
		if seen != nil {
			*seen, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(row.Status)
		_, _ = w.Write(body)
	})
}

// staticHandler always answers with status, optional headers and body.
func staticHandler(status int, headers map[string]string, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
