package mothdex

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func asError(err error, target **Error) bool { return errors.As(err, target) }

func TestRetryAfterIsHonoredOnceThenReported(t *testing.T) {
	h := newHarness(t, staticHandler(429, map[string]string{"Retry-After": "7"}, "slow down"))
	var e *Error
	_, err := h.client.Info(context.Background())
	if !asError(err, &e) || e.Kind != KindRateLimited || e.HTTPStatus != 429 || e.RetryAfter != 7*time.Second {
		t.Fatalf("got %#v", err)
	}
	if h.hits.Load() != 2 {
		t.Fatalf("want 1 automatic retry (2 requests), got %d", h.hits.Load())
	}
	if w := h.sleeper.Waits(); !slices.Equal(w, []time.Duration{7 * time.Second}) {
		t.Fatalf("waits %v", w)
	}
}

func TestRateLimitedWithoutRetryAfterWaitsTwoSeconds(t *testing.T) {
	// First answer 429, then the recorded 200: a transient breach recovers.
	var n atomic.Int32
	h2 := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(429)
			return
		}
		serveFixture(t, "info", nil).ServeHTTP(w, r)
	}))
	got, err := h2.client.Info(context.Background())
	if err != nil || got.TotalEntries <= 0 {
		t.Fatalf("Info = %+v, %v", got, err)
	}
	if w := h2.sleeper.Waits(); !slices.Equal(w, []time.Duration{2 * time.Second}) {
		t.Fatalf("waits %v", w)
	}
}

func TestBreachCircuitOpensOnThirdAndFailsFast(t *testing.T) {
	var hookUntil []time.Time
	h := newHarness(t, staticHandler(429, nil, ""), WithBreachHook(func(u time.Time) { hookUntil = append(hookUntil, u) }))
	ctx := context.Background()

	// Call 1: 429 (hit 1), retry after 2 s, 429 (hit 2): reported, circuit still closed.
	if _, err := h.client.Info(ctx); !IsRateLimited(err) {
		t.Fatalf("call 1: %v", err)
	}
	// Call 2: 429 (hit 3) opens the circuit and is not retried.
	opened := h.clock.Now()
	var e *Error
	if _, err := h.client.Info(ctx); !asError(err, &e) || e.RetryAfter != breachCooldown {
		t.Fatalf("call 2: %#v", err)
	}
	if len(hookUntil) != 1 || !hookUntil[0].Equal(opened.Add(breachCooldown)) {
		t.Fatalf("hook calls %v", hookUntil)
	}
	before := h.hits.Load()
	if before != 3 {
		t.Fatalf("hits before fail-fast: %d", before)
	}
	// Call 3: fails fast, no network, remaining time reported.
	h.clock.Advance(time.Minute)
	if _, err := h.client.Info(ctx); !asError(err, &e) || e.Kind != KindRateLimited || e.RetryAfter != breachCooldown-time.Minute {
		t.Fatalf("call 3: %#v", err)
	}
	if h.hits.Load() != before {
		t.Fatalf("fail-fast touched the network: %d -> %d", before, h.hits.Load())
	}
	// After the cooldown the network is used again.
	h.clock.Advance(breachCooldown)
	_, _ = h.client.Info(ctx)
	if h.hits.Load() != before+2 { // 429 plus its single automatic retry
		t.Fatalf("expected the network to be used again after cooldown, hits=%d", h.hits.Load())
	}
}

func TestSeededCooldownFailsFastWithoutNetwork(t *testing.T) {
	clock := newFakeClock()
	until := clock.Now().Add(5 * time.Minute)
	h := newHarness(t, serveFixture(t, "info", nil), WithClock(clock), WithCooldown(until))
	var e *Error
	if _, err := h.client.Info(context.Background()); !asError(err, &e) || e.Kind != KindRateLimited || e.RetryAfter != 5*time.Minute {
		t.Fatalf("got %#v", err)
	}
	if h.hits.Load() != 0 {
		t.Fatalf("network used during seeded cooldown")
	}
}

func TestUpstreamErrorsAreRetriedWithBackoffThenReported(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"http 500":            {500, "boom"},
		"http 503":            {503, ""},
		"db error with 200":   {200, `{"info":{"error":"Database operation failed"},"data":{}}`},
		"unexpected with 200": {200, `{"info":{"error":"An unexpected error occurred"},"data":{}}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, staticHandler(tc.status, nil, tc.body))
			if _, err := h.client.Info(context.Background()); !IsUpstream(err) {
				t.Fatalf("want upstream, got %v", err)
			}
			if h.hits.Load() != 3 {
				t.Fatalf("want 1 + 2 retries, got %d requests", h.hits.Load())
			}
			if w := h.sleeper.Waits(); !slices.Equal(w, []time.Duration{500 * time.Millisecond, time.Second}) {
				t.Fatalf("waits %v", w)
			}
		})
	}
}

func TestMaxRetriesOption(t *testing.T) {
	h := newHarness(t, staticHandler(500, nil, ""), WithMaxRetries(0))
	if _, err := h.client.Info(context.Background()); !IsUpstream(err) || h.hits.Load() != 1 {
		t.Fatalf("err=%v hits=%d", err, h.hits.Load())
	}
}

func TestNonRetriedStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   Kind
	}{{400, KindInvalidRequest}, {404, KindIncompatible}}
	for _, tc := range cases {
		h := newHarness(t, staticHandler(tc.status, nil, `{"info":{},"data":{}}`))
		var e *Error
		if _, err := h.client.Info(context.Background()); !asError(err, &e) || e.Kind != tc.want || h.hits.Load() != 1 {
			t.Errorf("status %d: got %v hits=%d", tc.status, err, h.hits.Load())
		}
	}
}

// transportFlaky fails n times with a network error, then answers with the info fixture.
func transportFlaky(t *testing.T, failures int) (*http.Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	body := string(fixtureBody(t, "info"))
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("attempt has no deadline")
		}
		if int(calls.Add(1)) <= failures {
			return nil, errors.New("connection reset by peer")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})
	return &http.Client{Transport: rt}, &calls
}

func TestNetworkErrorsRetryThenSucceed(t *testing.T) {
	hc, calls := transportFlaky(t, 2)
	h := newHarness(t, serveFixture(t, "info", nil), WithHTTPClient(hc))
	got, err := h.client.Info(context.Background())
	if err != nil || got.TotalEntries <= 0 || calls.Load() != 3 {
		t.Fatalf("Info = %+v, %v (calls %d)", got, err, calls.Load())
	}
}

func TestNetworkErrorsExhaustBudget(t *testing.T) {
	hc, calls := transportFlaky(t, 99)
	h := newHarness(t, serveFixture(t, "info", nil), WithHTTPClient(hc))
	if _, err := h.client.Info(context.Background()); !IsNetwork(err) || calls.Load() != 3 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestPerAttemptTimeoutIsNetworkNotCanceled(t *testing.T) {
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	h := newHarness(t, serveFixture(t, "info", nil), WithHTTPClient(&http.Client{Transport: rt}), WithMaxRetries(0))
	if _, err := h.client.Info(context.Background()); !IsNetwork(err) {
		t.Fatalf("want KindNetwork for an attempt timeout, got %v", err)
	}
}

func TestContextCanceledBeforeCall(t *testing.T) {
	h := newHarness(t, serveFixture(t, "info", nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.client.Info(ctx)
	if !IsCanceled(err) || !errors.Is(err, context.Canceled) || h.hits.Load() != 0 {
		t.Fatalf("err=%v hits=%d", err, h.hits.Load())
	}
}

func TestContextCanceledMidRequestIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		cancel()
		return nil, r.Context().Err()
	})
	h := newHarness(t, serveFixture(t, "info", nil), WithHTTPClient(&http.Client{Transport: rt}))
	if _, err := h.client.Info(ctx); !IsCanceled(err) || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestDeadlineExceededIsCanceledKind(t *testing.T) {
	h := newHarness(t, serveFixture(t, "info", nil))
	ctx, cancel := context.WithDeadline(context.Background(), h.clock.Now().Add(-time.Hour))
	defer cancel()
	if _, err := h.client.Info(ctx); !IsCanceled(err) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestTokenBucketThrottlesTheSixthCall(t *testing.T) {
	h := newHarness(t, serveFixture(t, "info", nil), WithRateLimit(5, 5))
	for i := 0; i < 6; i++ {
		if _, err := h.client.Info(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	w := h.sleeper.Waits()
	if len(w) != 1 || w[0] < 190*time.Millisecond || w[0] > 210*time.Millisecond {
		t.Fatalf("want one wait of about 200ms (5 req/s), got %v", w)
	}
}

func TestRateLimitOptionIsClamped(t *testing.T) {
	cases := map[string]struct {
		rps  float64
		want float64
	}{"low": {0.1, 1}, "high": {500, 10}, "inside": {3, 3}}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := defaultConfig()
			WithRateLimit(tc.rps, 5)(&cfg)
			if cfg.rate != tc.want {
				t.Fatalf("rate %v, want %v", cfg.rate, tc.want)
			}
		})
	}
}

func TestBaseURLValidation(t *testing.T) {
	cases := map[string]bool{
		"https://api.mothdex.net/v1":  true,
		"http://127.0.0.1:8080/v1":    true,
		"http://localhost:9/v1":       false,
		"http://[::1]:9/v1":           true,
		"http://api.mothdex.net/v1":   false,
		"ftp://127.0.0.1/v1":          false,
		"https://user:pw@example.com": false,
		"https://example.com/v1?x=1":  false,
		"":                            false,
	}
	for raw, ok := range cases {
		_, err := New(WithBaseURL(raw))
		if (err == nil) != ok {
			t.Errorf("base URL %q: err=%v, want ok=%v", raw, err, ok)
		}
	}
}

// A refused redirect is KindUntrustedRedirect (api-client 5.1): never retried, never the 429
// circuit, never KindNetwork, and the message names the target host only.
func TestRefusedRedirectIsUntrustedRedirect(t *testing.T) {
	tests := []struct {
		name     string
		location func(base string) string // base is http://127.0.0.1:port
		hits     int32
	}{
		{"other host", func(string) string { return "http://localhost:1/secret/v1/info" }, 1},
		{"scheme change", func(b string) string { return "https://" + strings.TrimPrefix(b, "http://") + "/secret/v1/info" }, 1},
		{"other port", func(string) string { return "http://127.0.0.1:1/secret/v1/info" }, 1},
		{"fourth hop", nil, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base string
			h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.location == nil { // same origin, endless chain: the fourth redirect is refused
					http.Redirect(w, r, r.URL.Path+"x", http.StatusFound) //nolint:gosec // test server redirecting to its own path
					return
				}
				http.Redirect(w, r, tt.location(base), http.StatusFound)
			}), WithMaxRetries(3))
			base = strings.TrimSuffix(h.client.base.String(), "/v1")

			for call := 0; call < 2; call++ { // the second call proves no circuit opened
				_, err := h.client.Info(context.Background())
				var e *Error
				if !errors.As(err, &e) || e.Kind != KindUntrustedRedirect {
					t.Fatalf("call %d: want KindUntrustedRedirect, got %v", call, err)
				}
				if strings.Contains(e.Message, "secret") || e.RetryAfter != 0 {
					t.Fatalf("message %q must name the host only, and no cooldown", e.Message)
				}
			}
			if got := h.hits.Load(); got != 2*tt.hits {
				t.Fatalf("%d requests for 2 calls, want %d: a refused redirect is not retried", got, 2*tt.hits)
			}
		})
	}
}

func TestSameOriginRedirectIsFollowed(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/info" {
			http.Redirect(w, r, "/v1/info2", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"error":"stop"}}`))
	}), WithMaxRetries(0))
	_, err := h.client.Info(context.Background())
	if IsUntrustedRedirect(err) || IsNetwork(err) {
		t.Fatalf("a same-origin redirect must be followed, got %v", err)
	}
}

func TestDefaultHTTPClientTLSPolicy(t *testing.T) {
	tr, ok := newDefaultHTTPClient().Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		t.Fatal("default client must carry an explicit TLS config")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("TLS policy: %+v", tr.TLSClientConfig)
	}
}

func TestBaseURLErrorsDoNotEchoCredentials(t *testing.T) {
	for _, raw := range []string{"https://TOKEN@exa mple/v1", "https://user:pw@host/v1", "https://TOKEN@%zz/v1", "https://user:S3cret", "https://h:80S3cret/v1"} {
		err := ValidateBaseURL(raw)
		if err == nil {
			t.Fatalf("%q accepted", raw)
		}
		if strings.Contains(err.Error(), "TOKEN") || strings.Contains(err.Error(), "pw") || strings.Contains(err.Error(), "S3cret") {
			t.Errorf("credential echoed: %v", err)
		}
	}
}

func TestRedirectWithoutLocationIsUntrustedRedirect(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFound) // a 3xx with no Location header
	}), WithMaxRetries(2))
	if _, err := h.client.Info(context.Background()); !IsUntrustedRedirect(err) || h.hits.Load() != 1 {
		t.Fatalf("err %v after %d requests, want KindUntrustedRedirect once", err, h.hits.Load())
	}
}

// Crivo minor: the refused host comes from a server-controlled Location, so control and format
// characters (bidi override) never reach the message.
func TestRefusedRedirectMessageDropsFormatCharacters(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "http://ex%E2%80%AEample.com/x")
		w.WriteHeader(http.StatusFound)
	}))
	_, err := h.client.Info(context.Background())
	if !IsUntrustedRedirect(err) || strings.ContainsRune(err.Error(), rune(0x202E)) {
		t.Fatalf("err %q must be untrusted_redirect without U+202E", err)
	}
}

// T-77: an upstream code at HTTP 200 takes the same retry paths as the equivalent status.
func TestRateLimitCodeAt200UsesTheExistingRetry(t *testing.T) {
	var n atomic.Int32
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "3")
			_, _ = w.Write([]byte(`{"info":{"error":"RATE_LIMIT_ERROR","message":"slow down"},"data":{}}`))
			return
		}
		serveFixture(t, "info", nil).ServeHTTP(w, r)
	}))
	if got, err := h.client.Info(context.Background()); err != nil || got.TotalEntries <= 0 {
		t.Fatalf("Info = %+v, %v", got, err)
	}
	if w := h.sleeper.Waits(); !slices.Equal(w, []time.Duration{3 * time.Second}) {
		t.Fatalf("waits %v", w)
	}
}

func TestUpstreamCodeAt200IsRetriedAndANotFoundIsNot(t *testing.T) {
	body := func(code string) http.Handler {
		return staticHandler(200, nil, `{"info":{"error":"`+code+`","message":"m"},"data":{}}`)
	}
	h := newHarness(t, body("DB_ERROR"))
	var e *Error
	if _, err := h.client.Info(context.Background()); !asError(err, &e) || e.Kind != KindUpstream || e.Code != "DB_ERROR" {
		t.Fatalf("got %v", err)
	}
	if h.hits.Load() < 2 {
		t.Fatalf("DB_ERROR was not retried: %d hits", h.hits.Load())
	}
	h = newHarness(t, body("ENTRY_NOT_FOUND"))
	if _, err := h.client.Info(context.Background()); !IsNotFound(err) || h.hits.Load() != 1 {
		t.Fatalf("not found: %v, hits %d", err, h.hits.Load())
	}
}

func TestRateLimitCodeAt200CountsTowardTheBreachCircuit(t *testing.T) {
	var hookUntil []time.Time
	h := newHarness(t, staticHandler(200, nil, `{"info":{"error":"RATE_LIMIT_ERROR","message":"slow"},"data":{}}`),
		WithBreachHook(func(u time.Time) { hookUntil = append(hookUntil, u) }))
	ctx := context.Background()
	// Call 1: hit 1, one automatic retry, hit 2: reported, circuit still closed.
	if _, err := h.client.Info(ctx); !IsRateLimited(err) || len(hookUntil) != 0 {
		t.Fatalf("call 1: %v, hooks %d", err, len(hookUntil))
	}
	// Call 2: hit 3 opens the circuit and is not retried.
	var e *Error
	if _, err := h.client.Info(ctx); !asError(err, &e) || e.Kind != KindRateLimited || len(hookUntil) != 1 || e.RetryAfter <= 0 {
		t.Fatalf("call 2: %v, hooks %d", err, len(hookUntil))
	}
}
