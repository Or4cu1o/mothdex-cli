package app

import (
	"context"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
)

// T-27 at the service: the catalog says 2000 bytes, the proxy 5000. The job downloads, both sizes
// reach the history, the snapshot carries the real size and one drift note.
func TestSizeDriftIsRecordedAndWarned(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	r.srv.Script("/f/ok", fakeproxy.OK(fakeproxy.Pattern(5000)))
	res := r.run(t, "game")
	if res.Done != 1 {
		t.Fatalf("%+v", res)
	}
	j := res.Jobs[0]
	if j.Expected != 5000 || j.CatalogSize != 2000 || !j.SizeDrift {
		t.Fatalf("expected %d catalog %d drift %v", j.Expected, j.CatalogSize, j.SizeDrift)
	}
	recs, err := r.hist.List(context.Background(), history.Filter{})
	if err != nil || len(recs) != 1 || recs[0].CatalogSize != 2000 || recs[0].ContentLength != 5000 || recs[0].Expected != 5000 {
		t.Fatalf("%+v %v", recs, err)
	}
}

// The consent travels from the request to the downloader: the default rule stops a download whose
// real size crosses the threshold, with the size and a hint; the exact consent and ConsentAny pass.
func TestSizeConsentTravelsWithTheRequest(t *testing.T) {
	cfg := DownloadConfig{Confirm: true, ConfirmBytes: 3000}
	run := func(consent int64) JobSnapshot {
		t.Helper()
		r := newDLRig(t, cfg)
		r.srv.Script("/f/ok", fakeproxy.OK(fakeproxy.Pattern(5000)))
		ctx := context.Background()
		if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "game"}, DestDir: r.out, SizeConsent: consent}}); err != nil {
			t.Fatal(err)
		}
		res, err := r.svc.Wait(ctx)
		if err != nil || len(res.Jobs) != 1 {
			t.Fatalf("%+v %v", res, err)
		}
		return res.Jobs[0]
	}
	j := run(0)
	if j.State != StateFailed || j.Err == nil || j.Err.Code != CodeConfirmationNeeded || j.Expected != 5000 ||
		j.Err.Args["size"] != int64(5000) || j.Err.Args["estimate"] != int64(2000) || j.Err.Args["hint_key"] != "errors.confirmation_required.size_hint" {
		t.Fatalf("%+v err %v", j, j.Err)
	}
	if j := run(5000); j.State != StateDone {
		t.Fatalf("the exact consent: %+v err %v", j, j.Err)
	}
	if j := run(ConsentAny); j.State != StateDone {
		t.Fatalf("ConsentAny: %+v err %v", j, j.Err)
	}
	if j := run(4000); j.State != StateFailed || j.Err.Code != CodeConfirmationNeeded {
		t.Fatalf("another size: %+v", j)
	}
}
