package fakeproxy_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

// Regression (Crivo, T-18 Minor 1): a canceled request must release its timer,
// or the next attempt's BlockUntil returns on the stale one.
func TestCanceledStallReleasesItsTimer(t *testing.T) {
	srv, clk := setup(t, fakeproxy.StallBeforeFirstByte([]byte("abc"), time.Hour))
	ctx, cancel := context.WithCancel(t.Context())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	clk.BlockUntil(t, 1)
	cancel()
	_ = resp.Body.Close()
	clk.BlockUntilIdle(t)
}

// Regression (Crivo, T-18 Minor 2): Close called by the test must not hang on
// a stalled handler.
func TestCloseReleasesStalledHandler(t *testing.T) {
	srv, clk := setup(t, fakeproxy.StallBeforeHeaders([]byte("x"), time.Hour))
	errc := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		resp, err := srv.Client().Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		errc <- err
	}()
	clk.BlockUntil(t, 1)
	closed := make(chan struct{})
	go func() { srv.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second): // real-time guard, fires only on failure
		t.Fatal("Close hung on a stalled handler")
	}
	t.Logf("stalled request ended: %v", <-errc)
	srv.Close() // second call is a no-op
}

// Case 33 needs a client that does not trust the certificate.
func TestUntrustedClientFailsVerification(t *testing.T) {
	srv, _ := setup(t, fakeproxy.OK([]byte("x")))
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	resp, err := srv.UntrustedClient().Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("request succeeded without trusting the certificate")
	}
	var verr *tls.CertificateVerificationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want a certificate verification error", err)
	}
}

// A step whose argument has no effect would let a test pass without
// exercising anything, so the constructors refuse it.
func TestNoOpStepsPanic(t *testing.T) {
	body := fakeproxy.Pattern(10)
	tests := map[string]func(){
		"truncate at length":  func() { fakeproxy.Truncate(body, 10) },
		"truncate negative":   func() { fakeproxy.Truncate(body, -1) },
		"stall at length":     func() { fakeproxy.StallMidStream(body, 10, time.Second) },
		"stall zero duration": func() { fakeproxy.StallMidStream(body, 5, 0) },
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("constructor accepted a no-op argument")
				}
			}()
			fn()
		})
	}
}
