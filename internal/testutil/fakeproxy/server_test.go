package fakeproxy_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeclock"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

const target = "https://proxy.mothdex.net/file.zip"

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func setup(t *testing.T, steps ...fakeproxy.Step) (*fakeproxy.Server, *fakeclock.Clock) {
	t.Helper()
	clk := fakeclock.New(epoch)
	srv := fakeproxy.New(t, clk)
	srv.Script("/file.zip", steps...)
	return srv, clk
}

func get(t *testing.T, srv *fakeproxy.Server) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestBodyBehaviors(t *testing.T) {
	body := fakeproxy.Pattern(1000)
	tests := []struct {
		name    string
		step    fakeproxy.Step
		wantLen int
		wantErr error
	}{
		{"ok", fakeproxy.OK(body), 1000, nil},
		{"truncated mid-body", fakeproxy.Truncate(body, 400), 400, io.ErrUnexpectedEOF},
		{"truncated before any byte", fakeproxy.Truncate(body, 0), 0, io.ErrUnexpectedEOF},
		{"declared longer than sent", fakeproxy.LengthMismatch(body, 2000), 1000, io.ErrUnexpectedEOF},
		{"declared shorter than body", fakeproxy.LengthMismatch(body, 600), 600, nil},
		{"no content-length, oversized", fakeproxy.Oversized(5000), 5000, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := setup(t, tc.step)
			got, err := io.ReadAll(get(t, srv).Body) //nolint:bodyclose // closed by get via t.Cleanup
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("read error = %v, want %v", err, tc.wantErr)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("read %d bytes, want %d", len(got), tc.wantLen)
			}
		})
	}
}

func TestOversizedHasNoContentLength(t *testing.T) {
	srv, _ := setup(t, fakeproxy.Oversized(100))
	if cl := get(t, srv).ContentLength; cl != -1 { //nolint:bodyclose // closed by get via t.Cleanup
		t.Fatalf("ContentLength = %d, want -1", cl)
	}
}

func TestStatusBehaviors(t *testing.T) {
	tests := []struct {
		name       string
		step       fakeproxy.Step
		wantStatus int
		header     string
		wantHeader string
	}{
		{"404", fakeproxy.NotFound(), 404, "", ""},
		{"429 with Retry-After", fakeproxy.TooManyRequests("120"), 429, "Retry-After", "120"},
		{"503", fakeproxy.ServerError(503), 503, "", ""},
		{"302 redirect", fakeproxy.Redirect(302, "https://evil.test/x"), 302, "Location", "https://evil.test/x"},
		{"malicious disposition", fakeproxy.MaliciousDisposition(nil, `attachment; filename="../../etc/passwd"`), 200,
			"Content-Disposition", `attachment; filename="../../etc/passwd"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := setup(t, tc.step)
			resp := get(t, srv) //nolint:bodyclose // closed by get via t.Cleanup
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.header != "" && resp.Header.Get(tc.header) != tc.wantHeader {
				t.Fatalf("%s = %q, want %q", tc.header, resp.Header.Get(tc.header), tc.wantHeader)
			}
		})
	}
}

func TestTransientNotFoundThenOK(t *testing.T) {
	srv, _ := setup(t, fakeproxy.NotFound(), fakeproxy.NotFound(), fakeproxy.OK([]byte("data")))
	var statuses []int
	for range 4 { // the last step repeats once the script is exhausted
		statuses = append(statuses, get(t, srv).StatusCode) //nolint:bodyclose // closed by get via t.Cleanup or by the receiver
	}
	want := []int{404, 404, 200, 200}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("statuses = %v, want %v", statuses, want)
		}
	}
	if reqs := srv.Requests(); len(reqs) != 4 || reqs[3].Attempt != 4 {
		t.Fatalf("request log = %+v", reqs)
	}
}

func TestGzipDespiteIdentity(t *testing.T) {
	srv, _ := setup(t, fakeproxy.GzipDespiteIdentity([]byte("payload")))
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", resp.Header.Get("Content-Encoding"))
	}
	zr, err := gzip.NewReader(resp.Body) // transport must not have decoded it
	if err != nil {
		t.Fatalf("body is not raw gzip: %v", err)
	}
	if got, _ := io.ReadAll(zr); !bytes.Equal(got, []byte("payload")) {
		t.Fatalf("decoded body = %q", got)
	}
	if got := srv.Requests()[0].Header.Get("Accept-Encoding"); got != "identity" {
		t.Fatalf("server saw Accept-Encoding %q", got)
	}
}

func TestStallBeforeHeadersWaitsForClock(t *testing.T) {
	srv, clk := setup(t, fakeproxy.StallBeforeHeaders([]byte("x"), 90*time.Second))
	done := make(chan *http.Response, 1)
	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		resp, err := srv.Client().Do(req) //nolint:bodyclose // closed by get via t.Cleanup or by the receiver
		if err != nil {
			t.Error(err)
		}
		done <- resp
	}()
	clk.BlockUntil(t, 1)
	select {
	case <-done:
		t.Fatal("response arrived before the clock advanced")
	default:
	}
	clk.Advance(90 * time.Second)
	if resp := <-done; resp != nil {
		_ = resp.Body.Close()
	}
}

func TestStallBeforeFirstByteSendsHeadersThenWaits(t *testing.T) {
	srv, clk := setup(t, fakeproxy.StallBeforeFirstByte([]byte("abc"), 10*time.Minute))
	resp := get(t, srv) //nolint:bodyclose // closed by get via t.Cleanup; headers must arrive while the body is stalled
	read := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(resp.Body); read <- b }()
	clk.BlockUntil(t, 1)
	select {
	case <-read:
		t.Fatal("body arrived before the clock advanced")
	default:
	}
	clk.Advance(10 * time.Minute)
	if got := <-read; string(got) != "abc" {
		t.Fatalf("body = %q", got)
	}
}

func TestStallMidStreamPausesAtOffset(t *testing.T) {
	body := fakeproxy.Pattern(100)
	srv, clk := setup(t, fakeproxy.StallMidStream(body, 40, 5*time.Minute))
	resp := get(t, srv) //nolint:bodyclose // closed by get via t.Cleanup
	head := make([]byte, 40)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		t.Fatalf("first part: %v", err)
	}
	clk.BlockUntil(t, 1)
	rest := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(resp.Body); rest <- b }()
	clk.Advance(5 * time.Minute)
	if got := append(head, <-rest...); !bytes.Equal(got, body) {
		t.Fatal("reassembled body differs")
	}
}

func TestConnectionsCountsOnlyRealConnects(t *testing.T) {
	srv, _ := setup(t, fakeproxy.OK(nil))
	if n := srv.Connections(); n != 0 {
		t.Fatalf("Connections = %d before any request", n)
	}
	get(t, srv) //nolint:bodyclose // closed by get via t.Cleanup
	if n := srv.Connections(); n != 1 {
		t.Fatalf("Connections = %d after one request", n)
	}
}
