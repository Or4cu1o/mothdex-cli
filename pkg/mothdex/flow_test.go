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

func TestCrossHostRedirectIsRefused(t *testing.T) {
	h := newHarness(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://localhost:1/v1/info", http.StatusFound)
	}), WithMaxRetries(0))
	if _, err := h.client.Info(context.Background()); !IsNetwork(err) {
		t.Fatalf("want KindNetwork for a refused redirect, got %v", err)
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
