package proxy_test

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

// The size-authority tests of cases_size_test.go leave the edges of two rules unpinned: found by hand
// mutation of the T-28 code (comparison operators changed one by one).

// Downloader 3.2 step 6: the confirmation asks only when the real size is ABOVE the threshold.
func TestConfirmationBoundaryIsInclusive(t *testing.T) {
	cfg := proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}
	run := func(t *testing.T, size int) proxy.Result {
		t.Helper()
		r := newRig(t, cfg)
		r.srv.Script("/f/b", fakeproxy.OK(fakeproxy.Pattern(size)))
		return r.download(r.job("/f/b", "b.zip", 1000)) // the catalog says 1000, below the threshold
	}
	if res := run(t, 10_000); res.State != proxy.StateDone {
		t.Fatalf("a real size of exactly the threshold needs no confirmation: %+v err %v", res, res.Err)
	}
	wantFailed(t, run(t, 10_001), proxy.CodeConfirmationRequired)
}

// Downloader 3.2 step 7: a warning only beyond 1.5x or below 0.5x of the catalog size, never at the edge.
func TestSizeDriftEdges(t *testing.T) {
	for _, c := range []struct {
		name      string
		link, cl  int
		wantDrift bool
	}{
		{"exactly 1.5x is not drift", 10, 15, false},
		{"just above 1.5x is drift", 10, 16, true},
		{"exactly 0.5x is not drift", 10, 5, false},
		{"just below 0.5x is drift", 10, 4, true},
		{"equal sizes are not drift", 10, 10, false},
		{"odd catalog size: 2 is below 2.5", 5, 2, true},
		{"odd catalog size: 3 is above 2.5", 5, 3, false},
		{"odd catalog size: 3 is below 3.5", 7, 3, true},
		{"odd catalog size: 8 is above 7.5", 5, 8, true},
		{"odd catalog size: 7 is below 7.5", 5, 7, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, proxy.Config{})
			r.srv.Script("/f/b", fakeproxy.OK(fakeproxy.Pattern(c.cl)))
			res := r.download(r.job("/f/b", "b.zip", c.link))
			if res.State != proxy.StateDone {
				t.Fatalf("%+v err %v", res, res.Err)
			}
			if res.SizeDrift != c.wantDrift {
				t.Fatalf("link %d, content-length %d: drift %v, want %v", c.link, c.cl, res.SizeDrift, c.wantDrift)
			}
		})
	}
}
