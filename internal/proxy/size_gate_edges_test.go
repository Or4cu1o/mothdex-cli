package proxy_test

import (
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

// T-64 boundaries around the 2x rule: the threshold itself, the off switch, and the default constant.
func TestSizeGate2xBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		cfg     proxy.Config
		est     int
		cl      int
		consent bool // true: want Done
	}{
		{"estimate exactly at the threshold keeps the old rule: cl above it asks", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 10_000, 10_001, false},
		{"estimate exactly at the threshold: cl at the threshold passes", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 10_000, 10_000, true},
		{"estimate exactly at the threshold: 2x does not apply, 20_000 asks", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 10_000, 20_000, false},
		{"estimate one above the threshold: 2x passes", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 10_001, 20_002, true},
		{"estimate one above the threshold: 2x+1 asks", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 10_001, 20_003, false},
		{"estimate above the threshold: a smaller real size passes", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 15_000, 12_000, true},
		{"confirm_downloads off: even 3x the estimate passes", proxy.Config{ConfirmBytes: 10_000}, 15_000, 45_000, true},
		{"estimate below the threshold: 2x rule not used, real size above asks", proxy.Config{ConfirmDownloads: true, ConfirmBytes: 10_000}, 9_000, 10_001, false},
		{"default threshold is used when ConfirmBytes is unset: 500 MiB estimate, small body", proxy.Config{ConfirmDownloads: true}, 524_288_000, 5_000, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.cfg)
			r.srv.Script("/f/b", fakeproxy.OK(fakeproxy.Pattern(c.cl)))
			res := r.download(r.job("/f/b", "b.zip", c.est))
			if c.consent {
				if res.State != proxy.StateDone {
					t.Fatalf("want done: %+v err %v", res, res.Err)
				}
				return
			}
			wantFailed(t, res, proxy.CodeConfirmationRequired)
		})
	}
}
