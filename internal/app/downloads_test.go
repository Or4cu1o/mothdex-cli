package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	svc        *Service
	srv        *fakeproxy.Server
	refreshErr error        // returned by the refresh hook
	missing    atomic.Int32 // refreshes that ran before the downloaded file existed
	out        string
	hist       *history.Store
	refresh    *atomic.Int32
	client     *fakeClient
	temp, data string
}

func newDLRig(t *testing.T, dc DownloadConfig) *dlRig { return newDLRigEnv(t, dc, nil) }

func newDLRigEnv(t *testing.T, dc DownloadConfig, mod func(*Env)) *dlRig {
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
	slow := mothdex.Entry{Slug: "slow", Title: "Slow", Platform: "snes", Links: []mothdex.Link{
		{Type: "Game", Format: "zip", URL: "https://proxy.mothdex.net/f/slow", Filename: "slow.zip", Size: 50},
	}}
	unknown := mothdex.Entry{Slug: "unk", Title: "Unk", Platform: "zzz", Links: good.Links}
	c := &fakeClient{entries: map[string]mothdex.Entry{"game": good, "bad": bad, "off": off, "unk": unknown, "slow": slow}}
	svc := newSvc(t, c)

	srv := fakeproxy.New(t, fakeclock.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	srv.Script("/f/ok", fakeproxy.OK(body))
	srv.Script("/f/missing", fakeproxy.NotFound())
	srv.Script("/f/slow", fakeproxy.StallBeforeFirstByte(fakeproxy.Pattern(50), time.Hour))

	r := &dlRig{svc: svc, srv: srv, out: filepath.Join(t.TempDir(), "out"), refresh: &atomic.Int32{}, client: c}
	var err error
	if r.hist, err = history.Open(filepath.Join(t.TempDir(), "data", "history.jsonl")); err != nil {
		t.Fatal(err)
	}
	dc.AllowedHosts = []string{"proxy.mothdex.net"}
	r.temp, r.data = t.TempDir(), filepath.Join(t.TempDir(), "data")
	env := Env{
		TempDir: r.temp, DataDir: r.data, History: r.hist, Download: dc,
		Refresh: func(context.Context) error {
			r.refresh.Add(1)
			if _, err := os.Stat(filepath.Join(r.out, "game.zip")); err != nil {
				r.missing.Add(1) // a refresh must run after the file landed
			}
			return r.refreshErr
		},
		httpDoer: srv.Client(), sleeper: instantSleeper{},
	}
	if mod != nil {
		mod(&env)
	}
	if err := svc.EnableDownloads(env); err != nil {
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
	if r.missing.Load() != 0 || r2.missing.Load() != 0 {
		t.Fatal("the refresh hook ran before the finished file existed")
	}
}

// Ensaio: a failing refresh hook is a sanitised warning in the result, never an error, and the
// batch stays Done.
func TestFailingRefreshHookIsASanitisedWarning(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	r.refreshErr = errors.New("hook said \x1b]52;c;ZXZpbA==\x07no\x1b[2J\n")
	res := r.run(t, "game")
	if res.Done != 1 || res.RefreshWarning == "" || strings.ContainsAny(res.RefreshWarning, "\x1b\x07\n") {
		t.Fatalf("%+v", res)
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
	if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "unk"}, DestDir: r.out}}); code(err) != "path_rejected" {
		t.Fatalf("a platform missing from /platforms is path_rejected (downloader 6.2): %v", err)
	}
	r.client.entries["nolinks"] = mothdex.Entry{Slug: "nolinks", Platform: "snes"}
	if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "nolinks"}, DestDir: r.out}}); code(err) != CodeNotFound {
		t.Fatalf("an entry without links is not_found: %v", err)
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
	timeout := time.After(5 * time.Second)
	for !seen[proxy.StateDone] {
		select {
		case ev := <-events:
			seen[ev.Job.State] = true
		case <-timeout:
			t.Fatalf("no done event; states seen: %v", seen)
		}
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

// Alicerce m2: the Events channel closes when its context ends, and when the service closes.
func TestEventsChannelClosesWithItsContextAndTheService(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	ev := r.svc.Events(ctx)
	cancel()
	select {
	case _, ok := <-ev:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the channel was never closed: a UI ranging over it would hang")
	}
	ev2 := r.svc.Events(context.Background())
	r.run(t, "game") // sends while another subscriber closes: must not panic under -race
	_ = r.svc.Close()
	for range ev2 { // drains and ends because Close closed it
	}
}

// Sentinela m1: the Confiner is built with the profile's launcher locations, so a destination inside
// a launcher folder is refused while -o elsewhere becomes the root and works.
func TestLauncherFoldersAreRefusedThroughTheConfiner(t *testing.T) {
	romRoot := t.TempDir()
	prof := ProfileInfo{ID: "batocera-like", RomRoot: romRoot}
	prof.Profile.Roms.Root = romRoot
	prof.Profile.Launcher.Dir = filepath.Join(romRoot, "ports")
	if err := os.MkdirAll(prof.Profile.Launcher.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	r := newDLRigEnv(t, DownloadConfig{}, func(e *Env) { e.Profile = prof })
	ctx := context.Background()
	for _, dir := range []string{filepath.Join(romRoot, "ports"), filepath.Join(romRoot, "ports", "sub")} {
		if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "game"}, DestDir: dir}}); err != nil {
			t.Fatal(err)
		}
		res, _ := r.svc.Wait(ctx)
		if res.Failed != 1 || res.Jobs[0].Err == nil || res.Jobs[0].Err.Code != "path_rejected" {
			t.Fatalf("%s must be refused as a launcher folder: %+v", dir, res)
		}
		if _, err := os.Stat(filepath.Join(dir, "game.zip")); err == nil {
			t.Fatalf("a file was written into %s", dir)
		}
	}
	ok := t.TempDir()
	r.out = ok
	if res := r.run(t, "game"); res.Done != 1 {
		t.Fatalf("-o outside the profile root becomes the root: %+v", res)
	}
}

// Sentinela m1: when the profile spells its root through a link (EmuELEC /roms -> /storage/roms)
// and -o uses the real spelling, only the Confiner's LauncherPaths (resolved) refuse the folder;
// the lexical IsLauncherPath check does not see it.
func TestLauncherFolderReachedThroughALinkedProfileRootIsRefused(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "storage", "roms")
	if err := os.MkdirAll(filepath.Join(realRoot, "ports"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "roms")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	prof := ProfileInfo{ID: "emuelec-like", RomRoot: link}
	prof.Profile.Roms.Root = link
	prof.Profile.Launcher.Dir = filepath.Join(link, "ports")
	r := newDLRigEnv(t, DownloadConfig{}, func(e *Env) { e.Profile = prof })
	dir := filepath.Join(realRoot, "ports")
	ctx := context.Background()
	if _, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "game"}, DestDir: dir}}); err != nil {
		t.Fatal(err)
	}
	res, _ := r.svc.Wait(ctx)
	if res.Failed != 1 || res.Jobs[0].Err == nil || res.Jobs[0].Err.Code != "path_rejected" {
		t.Fatalf("the real spelling of the launcher folder must be refused: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "game.zip")); err == nil {
		t.Fatal("a file was written into the launcher folder")
	}
}

// Sentinela m2: the startup Cleanup runs on the first use of a root, and a Downloader leaves the
// journal entries of another root alone (Sentinela s1).
func TestStartupCleanupRemovesOldOrphansAndKeepsOtherRoots(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	orphanDir := filepath.Join(r.temp, "downloads")
	if err := os.MkdirAll(orphanDir, 0o700); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(orphanDir, "aaaaaaaaaaaaaaaaaaaaaaaaaa.part")
	if err := os.WriteFile(orphan, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	inflight := filepath.Join(r.data, "inflight")
	if err := os.MkdirAll(inflight, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(inflight, "bbbbbbbbbbbbbbbbbbbbbbbbbb.json")
	entry := `{"job_id":"bbbbbbbbbbbbbbbbbbbbbbbbbb","staging":"downloads/bbbbbbbbbbbbbbbbbbbbbbbbbb.part","dest_dir":".","started_at":"2026-01-01T00:00:00Z","root":"/some/other/root"}`
	if err := os.WriteFile(other, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}

	r.run(t, "game")

	if _, err := os.Stat(orphan); err == nil {
		t.Fatal("the startup Cleanup did not remove the 48 h old staging orphan")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("a Downloader pruned the journal entry of another destination root")
	}
}

// Crivo m2: a slow reader never loses a state change.
func TestSlowEventsReaderKeepsEveryStateChange(t *testing.T) {
	r := newDLRig(t, DownloadConfig{OnExists: "rename"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := r.svc.Events(ctx) // not read until the batch is over
	slugs := make([]string, 12)
	for i := range slugs {
		slugs[i] = "game"
	}
	res := r.run(t, slugs...)
	if res.Done != 12 {
		t.Fatalf("%+v", res)
	}
	done := map[JobID]bool{}
	timeout := time.After(5 * time.Second)
	for len(done) < 12 {
		select {
		case ev := <-events:
			if ev.Job.State == StateDone {
				done[ev.Job.ID] = true
			}
		case <-timeout:
			t.Fatalf("only %d of 12 Done events arrived", len(done))
		}
	}
}

// Crivo s4: Cancel of a running and of a queued job gives Canceled, a history record and a clean Wait.
func TestCancelRunningAndQueuedJobs(t *testing.T) {
	r := newDLRigEnv(t, DownloadConfig{Concurrency: 1}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids, err := r.svc.Enqueue(ctx, []DownloadRequest{{Ref: Ref{Slug: "slow"}, DestDir: r.out}, {Ref: Ref{Slug: "game"}, DestDir: r.out}})
	if err != nil || len(ids) != 2 {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := r.svc.Cancel(id); err != nil {
			t.Fatalf("cancel %s: %v", id, err)
		}
	}
	res, err := r.svc.Wait(ctx)
	if err != nil || res.Cancel != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	recs, _ := r.hist.List(ctx, history.Filter{})
	for _, rec := range recs {
		if rec.State != "canceled" {
			t.Fatalf("history: %+v", rec)
		}
	}
}

// Sentinela s2 and Crivo s6: a refused batch creates no -o directory and queues nothing.
func TestRefusedBatchCreatesNoOutputDirectory(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	_, err := r.svc.Enqueue(context.Background(), []DownloadRequest{{Ref: Ref{Slug: "game"}, DestDir: r.out}, {Ref: Ref{Slug: "off"}, DestDir: r.out}})
	if err == nil {
		t.Fatal("accepted")
	}
	if _, serr := os.Stat(r.out); serr == nil {
		t.Fatal("the -o directory was created for a refused batch")
	}
}

// Alicerce m1: callers sharing a failed build each report the error and leave no entry behind;
// a later call with a root that now exists builds one Downloader. Close during a build never
// stores handles nobody closes (s1).
func TestFailedSharedBuildLeavesNoEntryAndRootCanRecover(t *testing.T) {
	r := newDLRig(t, DownloadConfig{})
	d := r.svc.dl
	root := filepath.Join(t.TempDir(), "later")
	known := map[string]bool{"snes": true}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := d.downloaderFor(root, known); err == nil {
				t.Error("a missing root must fail")
			}
		}()
	}
	wg.Wait()
	d.mu.Lock()
	n := len(d.roots)
	d.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d entries left after failed builds", n)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	a, err := d.downloaderFor(root, known)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := d.downloaderFor(root, known); a != b {
		t.Fatal("one Downloader per root")
	}
	r.svc.closeDownloads()
	if _, err := d.downloaderFor(filepath.Join(t.TempDir()), known); code(err) != CodeCanceled {
		t.Fatalf("a build after Close must not keep handles: %v", err)
	}
}
