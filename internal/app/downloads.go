package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// JobID names one queued download.
type JobID string

// DownloadRequest asks for one entry's link to be downloaded.
type DownloadRequest struct {
	Ref     Ref
	Link    LinkSelector
	DestDir string // "" = download.dir if set, else the active profile's folder for the platform
	// SizeConsent is the consent for the real size (downloader.md 3.2 step 6): 0 is the default rule
	// (a download whose Content-Length alone crosses 500 MiB fails with confirmation_required), ConsentAny
	// is -f/--force of that command, a value above 0 is the exact Content-Length accepted through
	// "Download anyway". A UI sets ConsentAny only from a flag of the command being run, never from a
	// start-up flag of a session (the startupGrants rule of ui.md 2.1).
	SizeConsent int64
}

// ConfirmBytes is the size above which a download needs confirmation (500 MiB). The prompt of a UI and
// the downloader's check at the real size use this one constant, so the two gates cannot drift apart.
const ConfirmBytes = proxy.DefaultConfirmBytes

// ConsentAny skips the confirmation at the real size (DownloadRequest.SizeConsent).
const ConsentAny = proxy.ConsentAny

// JobSnapshot is the state of a job at one moment.
type JobSnapshot struct {
	ID        JobID
	Slug      string
	Title     string
	Platform  string
	LinkIndex int
	State     State
	Attempt   int
	Bytes     int64
	Expected  int64 // the Content-Length once the headers are in, else the catalog size
	RateBps   int64
	Name      string // sanitized file name, once known
	Path      string // final absolute path of a finished job
	SHA256    string
	Err       *Error
	Started   time.Time
	Finished  time.Time
	// CatalogSize is link.size, a rough estimate; SizeDrift reports that the real size (Expected) is beyond
	// 1.5x or below 0.5x of it (downloader.md 3.2 step 7): the UI words the warning in its own language.
	CatalogSize int64
	SizeDrift   bool
}

// Terminal reports whether the job reached a final state.
func (j JobSnapshot) Terminal() bool {
	switch j.State {
	case proxy.StateDone, proxy.StateSkipped, proxy.StateFailed, proxy.StateCanceled:
		return true
	}
	return false
}

// Event reports a job change; progress events come at most 4 per second per job from the downloader.
type Event struct{ Job JobSnapshot }

// BatchResult summarizes the jobs that finished since the last Wait.
type BatchResult struct {
	Jobs                          []JobSnapshot
	Done, Skipped, Failed, Cancel int
	RefreshWarning                string // the frontend refresh hook failed or is manual; never fatal
}

// DownloadConfig is the downloader part of the configuration.
type DownloadConfig struct {
	AllowedHosts    []string
	Concurrency     int
	OnExists        string // skip | rename | overwrite
	SkipVerify      bool   // download.verify=false; the zero value verifies
	AllowExecutable bool   // --allow-executable
	Force           bool   // --force
	Dir             string // download.dir
	Confirm         bool   // interface.confirm_downloads: the size confirmation of downloader.md 3.2 step 6
	ConfirmBytes    int64  // the threshold of that confirmation; 0 is 500 MiB
}

// Env holds what the downloader needs from the machine.
type Env struct {
	TempDir     string // paths.temp: staging lives in <temp>/downloads
	DataDir     string // paths.data: history.jsonl and the in-flight journal
	Profile     library.ProfileInfo
	Refresh     func(ctx context.Context) error // nil = no refresh hook
	Download    DownloadConfig
	History     *history.Store
	Concurrency int

	httpDoer proxy.HTTPDoer // tests only: replaces the production HTTP client
	sleeper  proxy.Sleeper  // tests only: replaces the retry sleeper
}

type jobMeta struct {
	title           string
	linkIndex       int
	expected        int64
	sourceName, raw string
	started         time.Time
	dest, host      string // destination directory and link host, for the named values of failure messages
}

// downloads is the Downloads capability state of a Service.
type downloads struct {
	env       Env
	mu        sync.Mutex
	roots     map[string]*rootEntry // by absolute destination root
	jobs      map[JobID]*JobSnapshot
	meta      map[JobID]jobMeta
	done      map[JobID]doneJob // finished downloads, newest last, for SaveJobBoxart
	doneOrder []JobID
	subs      map[int]*subscriber
	next      int

	doneSinceWait int
	active        int           // jobs queued or running
	idle          chan struct{} // closed when active drops to 0
	ctx           context.Context
	cancel        context.CancelFunc
}

// EnableDownloads turns on the Downloads capability. It is separate from New so that catalog-only
// tests and commands (search, info) never touch the file system.
func (s *Service) EnableDownloads(env Env) error {
	if env.TempDir == "" || env.DataDir == "" || env.History == nil {
		return errors.New("app: TempDir, DataDir and History are required for downloads")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.dl = &downloads{env: env, roots: map[string]*rootEntry{}, jobs: map[JobID]*JobSnapshot{}, meta: map[JobID]jobMeta{}, done: map[JobID]doneJob{}, subs: map[int]*subscriber{}, ctx: ctx, cancel: cancel}
	return nil
}

type plan struct {
	job    proxy.Job
	root   string
	mkRoot bool // -o or download.dir: the directory is created once the whole batch is valid
	meta   jobMeta
	entry  mothdex.Entry
}

// Enqueue validates every request, opens every destination, and only then queues the jobs: a bad
// request, an unusable root or a failed Downloader build leaves nothing queued. Destination
// directories are created only after the whole batch validated; when a later root fails to open, the
// directories this call created for the earlier ones are removed again (only if still empty).
func (s *Service) Enqueue(ctx context.Context, reqs []DownloadRequest) ([]JobID, error) {
	d := s.dl
	if d == nil {
		return nil, newError(CodeInternal, "downloads are not enabled")
	}
	if len(reqs) == 0 {
		return nil, newError(CodeInvalidRequest, "nothing to download")
	}
	plats, err := s.Platforms(ctx, PlatformFilter{})
	if err != nil {
		return nil, err // no platform list at all: network, never path_rejected for every job
	}
	known := map[string]bool{}
	for _, p := range plats.Value {
		known[p.ID] = true
	}
	plans := make([]plan, 0, len(reqs))
	for _, r := range reqs {
		p, err := s.planFor(ctx, r, known)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	dls := make([]*proxy.Downloader, len(plans))
	var undo []func()  // removes the directories this call created, when a later root fails to open
	var built []string // roots whose Downloader this call built: nothing was queued on them yet
	fail := func(err error) ([]JobID, error) {
		for _, root := range built {
			d.release(root) // Windows cannot remove a directory that a handle still has open
		}
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return nil, err
	}
	for i, p := range plans { // phase 2: every root opens before any job starts
		if p.mkRoot {
			rm, merr := mkdirAllTracked(p.root)
			if merr != nil {
				return fail(&Error{Code: string(proxy.CodePathRejected), Message: "the output directory cannot be created", Cause: merr})
			}
			undo = append(undo, rm)
		}
		if !d.hasRoot(p.root) {
			built = append(built, p.root)
		}
		dl, err := d.downloaderFor(p.root, known)
		if err != nil {
			return fail(err)
		}
		dls[i] = dl
	}
	ids := make([]JobID, 0, len(plans))
	for i, p := range plans { // phase 3: queue
		jobID, err := dls[i].NewJobID()
		if err != nil {
			s.cancelAll(ids)
			return nil, wrap(err)
		}
		p.job.ID = jobID
		id := JobID(jobID)
		d.register(id, p) // counts the job as active before it can finish
		if _, results, err := dls[i].Submit(d.ctx, p.job); err != nil {
			d.unregister(id)
			s.cancelAll(ids)
			return nil, wrap(err)
		} else {
			go d.collect(id, results)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) planFor(ctx context.Context, r DownloadRequest, known map[string]bool) (plan, error) {
	choices, err := s.Links(ctx, r.Ref, r.Link)
	if err != nil {
		return plan{}, err
	}
	if len(choices) == 0 {
		return plan{}, newError(CodeNotFound, "the entry has no links")
	}
	if !choices[0].Available {
		return plan{}, newError(CodeUntrustedURL, "no link on the allowed download host list")
	}
	entryRes, err := s.Entry(ctx, r.Ref)
	if err != nil {
		return plan{}, err
	}
	e, c := entryRes.Value, choices[0]
	if !known[e.Platform] {
		return plan{}, newError(string(proxy.CodePathRejected), "the platform is not in the platform list") // downloader 6.2
	}
	root, dest, mk, err := s.dl.destination(r.DestDir, e.Platform)
	if err != nil {
		return plan{}, err
	}
	return plan{
		job:    proxy.Job{Link: c.Link, Slug: e.Slug, Platform: e.Platform, DestDir: dest, SizeConsent: r.SizeConsent},
		root:   root,
		mkRoot: mk,
		entry:  e,
		meta:   jobMeta{title: e.Title, linkIndex: c.Index, expected: c.Link.Size, sourceName: c.Link.SourceName, raw: c.Link.Filename, dest: dest, host: linkHost(c.Link.URL)},
	}, nil
}

// destination decides the root the downloader is confined to and the directory inside it:
// -o (or download.dir) becomes the absolute root and the files go straight into it; otherwise
// the root is the profile's ROM root and the directory is its folder for the platform. It has no
// side effects: the caller creates an explicit root after the batch validated.
func (d *downloads) destination(explicit, platform string) (root, dest string, mkRoot bool, err error) {
	if dir := firstNonEmpty(explicit, d.env.Download.Dir); dir != "" {
		abs, aerr := filepath.Abs(dir)
		if aerr != nil {
			return "", "", false, &Error{Code: CodeInvalidRequest, Message: "the output directory is not usable", Cause: aerr}
		}
		return abs, abs, true, nil
	}
	p := d.env.Profile
	dest, derr := p.Profile.DestDir(platform)
	if derr != nil {
		return "", "", false, &Error{Code: CodeUnmappedPlatform, Message: "no folder is set for the platform", Cause: derr}
	}
	return p.RomRoot, dest, false, nil
}

func firstPositive(a, b int64) int64 {
	if a > 0 {
		return a
	}
	return b
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// rootEntry builds the Downloader of one root once, outside d.mu, so a slow first use of a root
// (opening it, the startup Cleanup) never blocks the events of running jobs.
type rootEntry struct {
	once    sync.Once
	dl      *proxy.Downloader
	closers []io.Closer
	err     error
}

// downloaderFor returns the Downloader confined to root, building it (and running its startup
// Cleanup) the first time. Journal entries of a root that is not used again in this run are
// pruned only when that root is used again (a Downloader cleans its own root only).
func (d *downloads) downloaderFor(root string, known map[string]bool) (*proxy.Downloader, error) {
	d.mu.Lock()
	e, ok := d.roots[root]
	if !ok {
		e = &rootEntry{}
		d.roots[root] = e
	}
	d.mu.Unlock()
	e.once.Do(func() {
		dl, closers, err := d.build(root, known)
		d.mu.Lock()
		if err == nil && d.ctx.Err() != nil { // Close ran during the build: nothing would release these handles
			for _, c := range closers {
				_ = c.Close()
			}
			dl, closers, err = nil, nil, newError(CodeCanceled, "the service is closing")
		}
		e.dl, e.closers, e.err = dl, closers, err
		d.mu.Unlock()
	})
	d.mu.Lock()
	defer d.mu.Unlock()
	if e.err != nil {
		if d.roots[root] == e { // a failed build is retried on the next use; never drop a newer entry
			delete(d.roots, root)
		}
		return nil, e.err
	}
	return e.dl, nil
}

func (d *downloads) hasRoot(root string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.roots[root]
	return ok
}

// release closes the Downloader built for root and forgets it, so a later use opens it afresh. It is
// only for a root this call built and queued nothing on.
func (d *downloads) release(root string) {
	d.mu.Lock()
	e := d.roots[root]
	delete(d.roots, root)
	d.mu.Unlock()
	if e == nil {
		return
	}
	e.once.Do(func() {}) // a build that never ran must not run later
	for _, c := range e.closers {
		_ = c.Close()
	}
}

// confiner confines destination directories to dest and refuses the profile's launcher folders,
// through the links of either side (see proxy.Confiner).
func (d *downloads) confiner(dest *proxy.RootFS) *proxy.Confiner {
	prof := d.env.Profile
	var launchers []string
	if prof.Profile.Launcher.Dir != "" {
		launchers = append(launchers, prof.Profile.Launcher.Dir)
	}
	for _, f := range config.LauncherFolders() {
		if prof.RomRoot != "" {
			launchers = append(launchers, filepath.Join(prof.RomRoot, f))
		}
	}
	return &proxy.Confiner{FS: dest, IsLauncher: prof.Profile.IsLauncherPath, LauncherPaths: launchers, AllowExecutable: d.env.Download.AllowExecutable}
}

func (d *downloads) build(root string, known map[string]bool) (*proxy.Downloader, []io.Closer, error) {
	staging, err := proxy.OpenStagingFS(d.env.TempDir)
	if err != nil {
		return nil, nil, &Error{Code: CodeInternal, Message: "cannot open the staging directory", Cause: err}
	}
	dest, err := proxy.OpenRootFS(root)
	if err != nil {
		_ = staging.Close()
		return nil, nil, &Error{Code: CodeInvalidRequest, Message: "cannot open the destination root", Cause: err}
	}
	conf := d.confiner(dest)
	cfg := d.env.Download
	if cfg.OnExists == string(proxy.OnExistsOverwrite) && !cfg.Force {
		_ = staging.Close()
		_ = dest.Close()
		return nil, nil, newError(CodeInvalidConfig, "download.on_exists=overwrite needs --force") // exit 2, not an internal error
	}
	dl, err := proxy.New(proxy.Options{
		HTTP: d.httpClient(), Clock: sysClock{}, Sleeper: d.sleeper(), Rand: sysRand{},
		Staging: staging, Dest: dest, Finalizer: proxy.NewFinalizer(), Confine: conf.Resolve,
		History: historyAdapter{d: d}, Journal: proxy.NewFileJournal(filepath.Join(d.env.DataDir, "inflight")),
		DestRoot: root, OnEvent: d.onEvent,
		Config: proxy.Config{
			AllowedHosts: cfg.AllowedHosts, Concurrency: cfg.Concurrency, OnExists: proxy.OnExists(cfg.OnExists), SkipVerify: cfg.SkipVerify,
			Platforms: known, AllowExecutable: cfg.AllowExecutable, Force: cfg.Force, NotFoundRetries: -1, ConfirmDownloads: cfg.Confirm, ConfirmBytes: cfg.ConfirmBytes,
		},
	})
	if err != nil {
		_ = staging.Close()
		_ = dest.Close()
		return nil, nil, wrap(err)
	}
	// Startup cleanup of what a crashed run left behind; a failure is not fatal for the command.
	_ = dl.Cleanup(d.ctx)
	return dl, []io.Closer{staging, dest}, nil
}

func (d *downloads) httpClient() proxy.HTTPDoer {
	if d.env.httpDoer != nil {
		return d.env.httpDoer
	}
	return proxy.NewHTTPClient()
}

func (d *downloads) sleeper() proxy.Sleeper {
	if d.env.sleeper != nil {
		return d.env.sleeper
	}
	return sysSleeper{}
}

func (d *downloads) register(id JobID, p plan) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p.meta.started = time.Now()
	d.meta[id] = p.meta
	d.jobs[id] = &JobSnapshot{ID: id, Slug: p.entry.Slug, Title: p.entry.Title, Platform: p.entry.Platform, LinkIndex: p.meta.linkIndex, State: StateQueued, Expected: p.meta.expected, Started: p.meta.started}
	d.active++
}

// unregister undoes register for a job whose Submit failed.
func (d *downloads) unregister(id JobID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.jobs, id)
	delete(d.meta, id)
	d.finishLocked()
}

// finishLocked counts one job out and wakes waiters when none is left; the caller holds d.mu.
func (d *downloads) finishLocked() {
	d.active--
	if d.active == 0 && d.idle != nil {
		close(d.idle)
		d.idle = nil
	}
}

func (s *Service) cancelAll(ids []JobID) {
	for _, id := range ids {
		_ = s.Cancel(id)
	}
}

// onEvent applies a downloader event to the snapshot and fans it out; it must not block.
func (d *downloads) onEvent(e proxy.Event) {
	d.mu.Lock()
	j, ok := d.jobs[JobID(e.JobID)]
	if !ok || j.Terminal() { // an unknown job, or a late event after the terminal state
		d.mu.Unlock()
		return
	}
	j.State, j.Attempt, j.Bytes, j.RateBps = e.State, e.Attempt, e.Bytes, e.RateBps
	if e.Expected > 0 {
		j.Expected = e.Expected
	}
	d.publish(*j)
	d.mu.Unlock()
}

// collect waits for the terminal result of one job, finalizes its snapshot and publishes it.
func (d *downloads) collect(id JobID, results <-chan proxy.Result) {
	res := <-results
	d.mu.Lock()
	var snap JobSnapshot
	if j := d.jobs[id]; j != nil {
		j.State, j.Name, j.Path, j.Bytes, j.SHA256, j.Attempt, j.Finished = res.State, res.Name, res.Path, res.Bytes, res.SHA256, res.Attempts, time.Now()
		if res.Err != nil {
			var ae *Error
			if errors.As(wrap(res.Err), &ae) {
				j.Err = d.withJobArgs(ae, d.meta[id], res)
			}
		}
		j.CatalogSize = d.meta[id].expected
		j.SizeDrift = res.SizeDrift
		if res.State == proxy.StateDone {
			d.doneSinceWait++
			d.rememberDone(id, doneJob{slug: j.Slug, path: res.Path})
		}
		snap = *j
	}
	if d.jobs[id] != nil {
		d.publish(snap)
	}
	d.finishLocked()
	d.mu.Unlock()
}

// doneJob is what the service itself recorded about a finished download: SaveJobBoxart trusts this, never
// the Path or Slug a caller puts in a snapshot.
type doneJob struct{ slug, path string }

// maxDoneJobs bounds the memory of finished downloads kept for SaveJobBoxart.
const maxDoneJobs = 1000

func (d *downloads) rememberDone(id JobID, j doneJob) {
	d.done[id] = j
	d.doneOrder = append(d.doneOrder, id)
	if len(d.doneOrder) > maxDoneJobs {
		delete(d.done, d.doneOrder[0])
		d.doneOrder = d.doneOrder[1:]
	}
}

func (d *downloads) doneJob(id JobID) (doneJob, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	j, ok := d.done[id]
	return j, ok
}

// publish queues a snapshot for every subscriber; it never blocks. The caller holds d.mu.
func (d *downloads) publish(snap JobSnapshot) {
	for _, sb := range d.subs {
		sb.push(Event{Job: snap})
	}
}

// subscriber delivers events to one reader without ever blocking the transfer and without losing
// a state change: progress ticks of a job coalesce into the latest one, everything else is queued
// in order. One goroutine owns the output channel, so it is closed exactly once and never sent on
// after that.
type subscriber struct {
	mu     sync.Mutex
	queue  []Event
	closed bool
	notify chan struct{} // capacity 1
	done   chan struct{} // closed by close
	out    chan Event
}

func newSubscriber() *subscriber {
	return &subscriber{notify: make(chan struct{}, 1), done: make(chan struct{}), out: make(chan Event, 64)}
}

func (sb *subscriber) push(ev Event) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.closed {
		return
	}
	for i := len(sb.queue) - 1; i >= 0; i-- { // coalesce with the newest queued event of the same job
		q := sb.queue[i]
		if q.Job.ID != ev.Job.ID {
			continue
		}
		if q.Job.State == ev.Job.State {
			sb.queue[i] = ev // same state: a progress tick, or a richer final snapshot
			ev.Job.ID = ""   // consumed
		}
		break
	}
	if ev.Job.ID != "" {
		sb.queue = append(sb.queue, ev)
	}
	select {
	case sb.notify <- struct{}{}:
	default:
	}
}

func (sb *subscriber) close() {
	sb.mu.Lock()
	if !sb.closed {
		sb.closed = true
		close(sb.done)
	}
	sb.mu.Unlock()
}

func (sb *subscriber) run() {
	defer close(sb.out)
	for {
		sb.mu.Lock()
		var ev Event
		have := len(sb.queue) > 0
		if have {
			ev = sb.queue[0]
			sb.queue = sb.queue[1:]
		}
		sb.mu.Unlock()
		if have {
			select {
			case sb.out <- ev:
			case <-sb.done:
				return
			}
			continue
		}
		select {
		case <-sb.notify:
		case <-sb.done:
			return
		}
	}
}

// waitIdle blocks until no job is queued or running, or ctx ends.
func (d *downloads) waitIdle(ctx context.Context) error {
	for {
		d.mu.Lock()
		if d.active == 0 {
			d.mu.Unlock()
			return nil
		}
		if d.idle == nil {
			d.idle = make(chan struct{})
		}
		ch := d.idle
		d.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Jobs returns a snapshot of every job of this session, in no particular order.
func (s *Service) Jobs() []JobSnapshot {
	d := s.dl
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]JobSnapshot, 0, len(d.jobs))
	for _, j := range d.jobs {
		out = append(out, *j)
	}
	return out
}

// Events streams job changes until ctx ends or the service closes, then closes the channel. State
// changes are never dropped; progress ticks of a job coalesce when the reader is slow. Jobs and
// Wait are the authoritative final picture.
func (s *Service) Events(ctx context.Context) <-chan Event {
	d := s.dl
	if d == nil {
		out := make(chan Event)
		close(out)
		return out
	}
	sb := newSubscriber()
	d.mu.Lock()
	id := d.next
	d.next++
	d.subs[id] = sb
	d.mu.Unlock()
	go sb.run()
	go func() {
		select {
		case <-ctx.Done():
		case <-sb.done:
		}
		d.mu.Lock()
		delete(d.subs, id)
		d.mu.Unlock()
		sb.close()
	}()
	return sb.out
}

// Cancel stops one job (queued or running).
func (s *Service) Cancel(id JobID) error {
	d := s.dl
	if d == nil {
		return newError(CodeNotFound, "no such job")
	}
	d.mu.Lock()
	var dls []*proxy.Downloader
	for _, e := range d.roots {
		if e.dl != nil {
			dls = append(dls, e.dl)
		}
	}
	d.mu.Unlock()
	for _, dl := range dls {
		if dl.Cancel(string(id)) {
			return nil
		}
	}
	return newError(CodeNotFound, "no such job")
}

// Wait returns when every queued job reached a terminal state. When at least one job finished
// Done since the last Wait, the frontend refresh hook runs once for the whole batch (case 46);
// a failing or manual hook is a warning in the result, never an error.
func (s *Service) Wait(ctx context.Context) (BatchResult, error) {
	d := s.dl
	if d == nil {
		return BatchResult{}, nil
	}
	if err := d.waitIdle(ctx); err != nil {
		return BatchResult{}, wrap(err)
	}
	d.mu.Lock()
	var res BatchResult
	for id, j := range d.jobs {
		if !j.Terminal() {
			continue
		}
		res.Jobs = append(res.Jobs, *j)
		switch j.State {
		case proxy.StateDone:
			res.Done++
		case proxy.StateSkipped:
			res.Skipped++
		case proxy.StateFailed:
			res.Failed++
		case proxy.StateCanceled:
			res.Cancel++
		}
		delete(d.jobs, id)
		delete(d.meta, id)
	}
	refresh := d.doneSinceWait > 0
	d.doneSinceWait = 0
	d.mu.Unlock()
	if refresh && d.env.Refresh != nil {
		if err := d.env.Refresh(ctx); err != nil {
			res.RefreshWarning = SanitizeText(fmt.Sprint(err))
		}
	}
	sortJobs(res.Jobs)
	return res, nil
}

func sortJobs(js []JobSnapshot) {
	slices.SortStableFunc(js, func(a, b JobSnapshot) int { return a.Started.Compare(b.Started) })
}

// historyAdapter turns the downloader record into the history record, adding what only the
// service knows (title, link index, expected size).
type historyAdapter struct{ d *downloads }

func (h historyAdapter) Append(r proxy.Record) error {
	h.d.mu.Lock()
	m := h.d.meta[JobID(r.JobID)]
	h.d.mu.Unlock()
	rec := history.Record{
		ID: r.JobID, StartedAt: r.Started, FinishedAt: r.Finished, State: string(r.State), Slug: r.Slug, Title: m.title, Platform: r.Platform,
		SourceName: m.sourceName, LinkIndex: m.linkIndex, Filename: r.Name, RawName: r.RawName, Path: r.Path, Bytes: r.Bytes, Expected: firstPositive(r.ContentLength, m.expected), CatalogSize: r.CatalogSize, ContentLength: r.ContentLength,
		SHA256: r.SHA256, CRC: r.CRC, Hash: r.Hash, Attempts: r.Attempts,
	}
	if r.Code != "" {
		rec.Error = &history.Failure{Code: string(r.Code), Message: codeMessage(string(r.Code))}
	}
	return h.d.env.History.Append(rec)
}

// codeMessage is the stable English text of an error code for the history file.
func codeMessage(code string) string {
	switch code {
	case "disk_full":
		return "not enough free space"
	case "size_mismatch":
		return "the downloaded file has the wrong size"
	case "integrity":
		return "the downloaded file is corrupt"
	case "exists":
		return "the file already exists"
	case "path_rejected":
		return "the destination was refused"
	case "unsafe_name":
		return "the file name was refused"
	case "untrusted_url":
		return "the download host is not trusted"
	case "network":
		return "the server could not be reached"
	case "upstream":
		return "the server had a problem"
	case "rate_limited":
		return "the server asked to slow down"
	case "canceled":
		return "canceled"
	}
	return "download failed"
}

// Close cancels running jobs, waits for them to settle, closes the Events channels and releases
// the root handles of the downloaders. An Events reader may miss
// events still queued when the channel closes; Wait and Jobs are the authoritative result.
func (s *Service) closeDownloads() {
	d := s.dl
	if d == nil {
		return
	}
	d.cancel()
	_ = d.waitIdle(context.Background())
	d.mu.Lock()
	subs := make([]*subscriber, 0, len(d.subs))
	for id, sb := range d.subs {
		delete(d.subs, id)
		subs = append(subs, sb)
	}
	var closers []io.Closer
	for root, e := range d.roots {
		closers = append(closers, e.closers...)
		delete(d.roots, root)
	}
	d.mu.Unlock()
	for _, sb := range subs {
		sb.close()
	}
	for _, c := range closers {
		_ = c.Close()
	}
}

// linkHost is the host of a link URL ("" when it does not parse).
func linkHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// withJobArgs fills the named values the localized failure messages use (path, name, host,
// allowed, got, expected, needed) from what the service knows about the job, so a UI never shows a
// raw {placeholder}. The values are cleaned: they come from the API or the file system.
func (d *downloads) withJobArgs(e *Error, m jobMeta, res proxy.Result) *Error {
	if e.Args == nil {
		e.Args = map[string]any{}
	}
	set := func(k string, v any) {
		if _, ok := e.Args[k]; !ok {
			e.Args[k] = v
		}
	}
	set("path", SanitizeText(m.dest))
	set("name", SanitizeText(firstNonEmpty(res.Name, m.raw)))
	set("host", SanitizeText(m.host))
	set("allowed", SanitizeText(strings.Join(d.env.Download.AllowedHosts, ", ")))
	set("got", res.Bytes)
	set("expected", m.expected)
	set("needed", m.expected)
	if e.Code == string(proxy.CodeConfirmationRequired) { // the real size and the way out (downloader.md 3.2 step 6)
		set("size", res.ContentLength)
		set("hint_key", "errors.confirmation_required.size_hint")
	}
	return e
}

// mkdirAllTracked is os.MkdirAll that returns a function removing the directories it created, deepest
// first, each only while it is empty (os.Remove never deletes content).
func mkdirAllTracked(dir string) (remove func(), err error) {
	var created []string // deepest first
	for p := dir; ; p = filepath.Dir(p) {
		if _, serr := os.Stat(p); serr == nil {
			break
		}
		created = append(created, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		for _, p := range created {
			_ = os.Remove(p)
		}
		return nil, err
	}
	return func() {
		for _, p := range created {
			_ = os.Remove(p)
		}
	}, nil
}
