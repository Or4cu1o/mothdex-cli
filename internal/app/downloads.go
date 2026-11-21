package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	Boxart  *bool  // nil = download.boxart; the CLI saves the cover (not done by the service)
}

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
	Expected  int64
	RateBps   int64
	Name      string // sanitized file name, once known
	Path      string // final absolute path of a finished job
	SHA256    string
	Err       *Error
	Started   time.Time
	Finished  time.Time
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
}

// downloads is the Downloads capability state of a Service.
type downloads struct {
	env     Env
	mu      sync.Mutex
	dls     map[string]*proxy.Downloader // by absolute destination root
	closers []io.Closer                  // the os.Root handles the downloaders hold
	jobs    map[JobID]*JobSnapshot
	meta    map[JobID]jobMeta
	subs    map[int]chan Event
	next    int

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
	s.dl = &downloads{env: env, dls: map[string]*proxy.Downloader{}, jobs: map[JobID]*JobSnapshot{}, meta: map[JobID]jobMeta{}, subs: map[int]chan Event{}, ctx: ctx, cancel: cancel}
	return nil
}

type plan struct {
	job   proxy.Job
	root  string
	meta  jobMeta
	entry mothdex.Entry
}

// Enqueue validates every request, then queues them all; nothing is queued if one is invalid.
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
	ids := make([]JobID, 0, len(plans))
	for _, p := range plans {
		dl, err := d.downloaderFor(p.root, known)
		if err != nil {
			s.cancelAll(ids)
			return nil, err
		}
		jobID, err := dl.NewJobID()
		if err != nil {
			s.cancelAll(ids)
			return nil, wrap(err)
		}
		p.job.ID = jobID
		id := JobID(jobID)
		d.register(id, p)
		_, results, err := dl.Submit(d.ctx, p.job)
		if err != nil {
			d.forget(id)
			s.cancelAll(ids)
			return nil, wrap(err)
		}
		ids = append(ids, id)
		d.mu.Lock()
		d.active++
		d.mu.Unlock()
		go d.collect(id, results)
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
	root, dest, err := s.dl.destination(r.DestDir, e.Platform)
	if err != nil {
		return plan{}, err
	}
	return plan{
		job:   proxy.Job{Link: c.Link, Slug: e.Slug, Platform: e.Platform, DestDir: dest},
		root:  root,
		entry: e,
		meta:  jobMeta{title: e.Title, linkIndex: c.Index, expected: c.Link.Size, sourceName: c.Link.SourceName, raw: c.Link.Filename},
	}, nil
}

// destination decides the root the downloader is confined to and the directory inside it:
// -o (or download.dir) becomes the absolute root and the files go straight into it; otherwise
// the root is the profile's ROM root and the directory is its folder for the platform.
func (d *downloads) destination(explicit, platform string) (root, dest string, err error) {
	if dir := firstNonEmpty(explicit, d.env.Download.Dir); dir != "" {
		abs, aerr := filepath.Abs(dir)
		if aerr != nil {
			return "", "", &Error{Code: CodeInvalidRequest, Message: "the output directory is not usable", Cause: aerr}
		}
		if merr := os.MkdirAll(abs, 0o700); merr != nil {
			return "", "", &Error{Code: string(proxy.CodePathRejected), Message: "the output directory cannot be created", Cause: merr}
		}
		return abs, abs, nil
	}
	p := d.env.Profile
	dest, derr := p.Profile.DestDir(platform)
	if derr != nil {
		return "", "", &Error{Code: CodeUnmappedPlatform, Message: "no folder is set for the platform", Cause: derr}
	}
	return p.RomRoot, dest, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// downloaderFor returns the Downloader confined to root, building it (and running its startup
// Cleanup) the first time.
func (d *downloads) downloaderFor(root string, known map[string]bool) (*proxy.Downloader, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dl, ok := d.dls[root]; ok {
		return dl, nil
	}
	dl, closers, err := d.build(root, known)
	if err != nil {
		return nil, err
	}
	d.dls[root] = dl
	d.closers = append(d.closers, closers...)
	return dl, nil
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
	conf := &proxy.Confiner{FS: dest, IsLauncher: prof.Profile.IsLauncherPath, LauncherPaths: launchers, AllowExecutable: d.env.Download.AllowExecutable}
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
			Platforms: known, AllowExecutable: cfg.AllowExecutable, Force: cfg.Force, NotFoundRetries: -1,
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
	d.jobs[id] = &JobSnapshot{ID: id, Slug: p.entry.Slug, Title: p.entry.Title, Platform: p.entry.Platform, LinkIndex: p.meta.linkIndex, State: proxy.StateQueued, Expected: p.meta.expected, Started: p.meta.started}
}

func (d *downloads) forget(id JobID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.jobs, id)
	delete(d.meta, id)
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
	if !ok {
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
				j.Err = ae
			}
		}
		if res.State == proxy.StateDone {
			d.doneSinceWait++
		}
		snap = *j
	}
	if d.jobs[id] != nil {
		d.publish(snap)
	}
	d.active--
	if d.active == 0 && d.idle != nil {
		close(d.idle)
		d.idle = nil
	}
	d.mu.Unlock()
}

// publish sends a snapshot to every subscriber. The caller holds d.mu, so a channel cannot be
// closed under the send; the sends never block (a slow UI loses a tick).
func (d *downloads) publish(snap JobSnapshot) {
	for _, c := range d.subs {
		select {
		case c <- Event{Job: snap}:
		default:
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

// Events streams job changes until ctx ends. The channel is buffered and lossy under a slow reader.
func (s *Service) Events(ctx context.Context) <-chan Event {
	out := make(chan Event, 64)
	d := s.dl
	if d == nil {
		close(out)
		return out
	}
	d.mu.Lock()
	id := d.next
	d.next++
	d.subs[id] = out
	d.mu.Unlock()
	go func() {
		<-ctx.Done()
		d.mu.Lock()
		if c, ok := d.subs[id]; ok { // Close may have closed it already
			delete(d.subs, id)
			close(c)
		}
		d.mu.Unlock()
	}()
	return out
}

// Cancel stops one job (queued or running).
func (s *Service) Cancel(id JobID) error {
	d := s.dl
	if d == nil {
		return newError(CodeNotFound, "no such job")
	}
	d.mu.Lock()
	dls := make([]*proxy.Downloader, 0, len(d.dls))
	for _, dl := range d.dls {
		dls = append(dls, dl)
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
	slicesSortJobs(res.Jobs)
	return res, nil
}

func slicesSortJobs(js []JobSnapshot) {
	for i := 1; i < len(js); i++ {
		for j := i; j > 0 && js[j].Started.Before(js[j-1].Started); j-- {
			js[j], js[j-1] = js[j-1], js[j]
		}
	}
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
		SourceName: m.sourceName, LinkIndex: m.linkIndex, Filename: r.Name, RawName: r.RawName, Path: r.Path, Bytes: r.Bytes, Expected: m.expected,
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

// Close cancels running jobs and waits for them to settle.
func (s *Service) closeDownloads() {
	if d := s.dl; d != nil {
		d.cancel()
		_ = d.waitIdle(context.Background())
		d.mu.Lock()
		for id, c := range d.subs {
			delete(d.subs, id)
			close(c)
		}
		closers := d.closers
		d.closers = nil
		d.mu.Unlock()
		for _, c := range closers {
			_ = c.Close()
		}
	}
}
