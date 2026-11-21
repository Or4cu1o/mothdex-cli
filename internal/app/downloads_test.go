package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeclock"
	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeproxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// instantSleeper makes retry back-off take no time.
type instantSleeper struct{}

func (instantSleeper) Sleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

type dlRig struct {
	svc     *Service
	srv     *fakeproxy.Server
	out     string
	hist    *history.Store
	refresh *atomic.Int32
	client  *fakeClient
}

func newDLRig(t *testing.T, dc DownloadConfig) *dlRig {
	t.Helper()
	body := fakeproxy.Pattern(2000)
	good := mothdex.Entry{Slug: "game", Title: "Game", Platform: "snes", Links: []mothdex.Link{
		{Type: "Game", Format: "zip", URL: "https://proxy.mothdex.net/f/ok", Filename: "game.zip", Size: int64(len(body)), SourceName: "Src"},
	}}
	bad := mothdex.Entry{Slug: "bad", Title: "Bad", Platform: "snes", Links: []mothdex.Link{
		{Type: "Game", Format: "zip", URL: "https://proxy.mothdex.net/f/missing", Filename: "bad.zip", Size: 10},
	}}
	off := mothdex.Entry{Slug: "off", Title: "Off", Platform: "snes", Links: []mothdex.Link{
		{Type: "Game", Format: "zip", URL: "https://archive.org/x.zip", Filename: "off.zip", Size: 10},
	}}
	unknown := mothdex.Entry{Slug: "unk", Title: "Unk", Platform: "zzz", Links: good.Links}
	c := &fakeClient{entries: map[string]mothdex.Entry{"game": good, "bad": bad, "off": off, "unk": unknown}}
	svc := newSvc(t, c)

	srv := fakeproxy.New(t, fakeclock.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	srv.Script("/f/ok", fakeproxy.OK(body))
	srv.Script("/f/missing", fakeproxy.NotFound())

	r := &dlRig{svc: svc, srv: srv, out: filepath.Join(t.TempDir(), "out"), refresh: &atomic.Int32{}, client: c}
	var err error
	if r.hist, err = history.Open(filepath.Join(t.TempDir(), "data", "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	dc.AllowedHosts = []string{"proxy.mothdex.net"}
	if err := svc.EnableDownloads(Env{
		TempDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "data"), History: r.hist, Download: dc,
		Refresh:  func(context.Context) error { r.refresh.Add(1); return nil },
		httpDoer: srv.Client(), sleeper: instantSleeper{},
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return r
}

func (r *dlRig) run(t *testing.T, slugs ...string) BatchResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var reqs []DownloadRequest
	for _, s := range slugs {
		reqs = append(reqs, DownloadRequest{Ref: Ref{Slug: s}, DestDir: r.out})
	}
	if _, err := r.svc.Enqueue(ctx, reqs); err != nil {
		t.Fatal(err)
	}
	res, err := r.svc.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// Case 46: the frontend refresh runs once per batch, only when a job finished Done.
func TestCase46RefreshOncePerBatch(t *testing.T) {
	r := newDLRig(t, DownloadConfig{OnExists: "skip"})

	res := r.run(t, "game")
	if res.Done != 1 || r.refresh.Load() != 1 {
		t.Fatalf("one Done: %+v, %d refreshes", res, r.refresh.Load())
	}
	if _, err := os.Stat(filepath.Join(r.out, "game.zip")); err != nil {
		t.Fatalf("the file is in the -o directory, the root: %v", err)
	}
	if res = r.run(t, "game"); res.Skipped != 1 || r.refresh.Load() != 1 {
		t.Fatalf("all Skipped must not refresh: %+v, %d refreshes", res, r.refresh.Load())
	}
	if res = r.run(t, "bad"); res.Failed != 1 || r.refresh.Load() != 1 {
		t.Fatalf("all Failed must not refresh: %+v, %d refreshes", res, r.refresh.Load())
	}
	r2 := newDLRig(t, DownloadConfig{OnExists: "rename"})
	if res = r2.run(t, "game", "game", "bad"); res.Done != 2 || res.Failed != 1 || r2.refresh.Load() != 1 {
		t.Fatalf("a mixed batch refreshes once: %+v, %d refreshes", res, r2.refresh.Load())
	}
}

func TestHistoryRecordCarriesTheServiceMetadata(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	r.run(t, "game", "bad")
	recs, err := r.hist.List(context.Background(), history.Filter{})
	if err != nil || len(recs) != 2 {
		t.Fatalf("%d records: %v", len(recs), err)
	}
	byTitle := map[string]history.Record{}
	for _, rec := range recs {
		byTitle[rec.Title] = rec
	}
	ok, bad := byTitle["Game"], byTitle["Bad"]
	if ok.State != "done" || ok.LinkIndex != 1 || ok.Expected != 2000 || ok.SourceName != "Src" || ok.DurationMS < 0 || ok.Error != nil || !filepath.IsAbs(ok.Path) {
		t.Fatalf("done record: %+v", ok)
	}
	if bad.State != "failed" || bad.Error == nil || bad.Error.Code == "" || bad.Error.Message == "" {
		t.Fatalf("failed record: %+v", bad)
	}
}

// Enqueue validates every request first: one bad request queues nothing.
func TestEnqueueValidatesEverythingBeforeQueueingAny(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	ctx := context.Background()
	for name, slug := range map[string]string{"off-allowlist link": "off", "platform not in /platforms": "unk", "unknown entry": "nope"} {
		_, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "game"}, DestDir: r.out}, {Ref: Ref{Slug: slug}, DestDir: r.out}})
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if len(r.svc.Jobs()) != 0 {
			t.Fatalf("%s: the valid request was queued anyway", name)
		}
	}
	if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "off"}, DestDir: r.out}}); code(err) != CodeUntrustedURL {
		t.Fatalf("off-allowlist: %v", err)
	}
	if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "unk"}, DestDir: r.out}}); code(err) != CodeUnmappedPlatform {
		t.Fatalf("unknown platform: %v", err)
	}
	if _, err := r.svc.Enqueue(ctx, nil); code(err) != CodeInvalidRequest {
		t.Fatal("empty batch")
	}
}

// Without -o and without a profile folder the platform has no destination: unmapped_platform.
func TestNoDestinationIsUnmappedPlatform(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	_, err := r.svc.Enqueue(context.Background(), []DownloadRequest{{Ref: Ref{Slug: "game"}}})
	if code(err) != CodeUnmappedPlatform {
		t.Fatalf("%v", err)
	}
}

func TestEventsAndCancel(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := r.svc.Events(ctx)
	r.run(t, "game")
	seen := map[proxy.State]bool{}
	for len(events) > 0 {
		seen[(<-events).Job.State] = true
	}
	if !seen[proxy.StateDone] {
		t.Fatalf("states seen: %v", seen)
	}
	if err := r.svc.Cancel("nope"); code(err) != CodeNotFound {
		t.Fatalf("cancel unknown: %v", err)
	}
}

// Crivo (PR 32): the downloader's refusal of overwrite without --force is a configuration error
// (exit 2), whatever layer supplied the value.
func TestOverwriteWithoutForceIsAConfigError(t *testing.T) {
	r := newDLRig(t, DownloadConfig{OnExists: "overwrite"})
	_, err := r.svc.Enqueue(context.Background(), []DownloadRequest{{Ref: Ref{Slug: "game"}, DestDir: r.out}})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeInvalidConfig || e.ExitCode() != 2 {
		t.Fatalf("%v", err)
	}
	r2 := newDLRig(t, DownloadConfig{OnExists: "overwrite", Force: true})
	if res := r2.run(t, "game"); res.Done != 1 {
		t.Fatalf("with --force: %+v", res)
	}
}
