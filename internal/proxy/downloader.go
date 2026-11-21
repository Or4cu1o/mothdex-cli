package proxy

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Defaults of downloader.md (sections 3, 5 and 7).
const (
	DefaultMaxFileSize     = int64(256) << 30
	DefaultConcurrency     = 2
	maxConcurrency         = 6
	DefaultMaxAttempts     = 5
	DefaultNotFoundRetries = 3
	DefaultHeaderTimeout   = 60 * time.Second
	DefaultFirstByte       = 10 * time.Minute
	DefaultIdleTimeout     = 5 * time.Minute
	progressInterval       = 250 * time.Millisecond // 4 events per second
	copyBuffer             = 256 << 10
)

// OnExists is the policy for a final name that already exists.
type OnExists string

// Policies. OnExistsOverwrite is only valid together with Config.Force (the --force flag).
const (
	OnExistsSkip      OnExists = "skip"
	OnExistsRename    OnExists = "rename"
	OnExistsOverwrite OnExists = "overwrite"
)

// Config is the part of the application configuration the downloader reads, plus the two flags
// that no config file or environment variable can set.
type Config struct {
	AllowedHosts     []string // default DefaultAllowedHosts
	MaxFileSize      int64    // default DefaultMaxFileSize
	Concurrency      int      // clamped to 1..6, default 2
	MaxAttempts      int      // total attempts for retryable failures, default 5
	NotFoundRetries  int      // retries after a 404, default 3; a negative value means the default, 0 means none
	HeaderTimeout    time.Duration
	FirstByteTimeout time.Duration
	IdleTimeout      time.Duration
	OnExists         OnExists        // default skip
	SkipVerify       bool            // download.verify=false: skip the zip CRC and upstream hash checks; the size is always checked
	Platforms        map[string]bool // ids from /platforms (cached meta, stale included); required
	UserAgent        string          // default mothdex.DefaultUserAgent("")

	AllowExecutable bool // --allow-executable
	Force           bool // --force: the only way to OnExistsOverwrite
}

// Options holds everything that touches the outside world.
type Options struct {
	HTTP      HTTPDoer
	Clock     Clock
	Sleeper   Sleeper
	Rand      Rand
	IDs       io.Reader // job id entropy; default crypto/rand
	Staging   FS        // rooted at paths.temp; staging files live in "downloads/"
	Dest      FS        // rooted at the ROM root (or -o)
	Finalizer Finalizer
	// Confine resolves a destination directory to a name inside Dest (Confiner.Resolve).
	Confine func(destdir string) (string, error)
	History History
	Journal Journal
	// DestRoot is the absolute directory Dest is rooted at; Record.Path and Result.Path are
	// DestRoot joined with the final name, because history outlives a profile or -o change.
	// Empty (tests): paths stay relative to Dest.
	DestRoot string
	// OnEvent receives state changes and throttled progress. Calls are serialized and run on
	// the transfer goroutine, so the callback must not block.
	OnEvent func(Event)
	Config  Config
}

// Job is one download. Every field comes from untrusted data and is validated at use.
type Job struct {
	// ID is optional: 26 base32 characters from NewJobID, so the caller can hand it out before
	// the job starts. Empty means Submit assigns one.
	ID       string
	Link     mothdex.Link
	Slug     string
	Platform string // Mothdex platform id
	DestDir  string // absolute, from library.Profile.DestDir or -o
}

// Result is the outcome of one job. Exactly one terminal State is reported per job.
type Result struct {
	JobID    string
	State    State
	Name     string // sanitized name
	Path     string // final path of a Done job, relative to the destination root
	Bytes    int64
	SHA256   string
	Attempts int
	Err      *Error // nil unless State is Failed or Canceled
	// HistoryErr is set when the history record could not be written.
	HistoryErr error
}

// Downloader runs jobs. Create it with New.
type Downloader struct {
	o       Options
	cfg     Config
	ledger  *ledger
	names   *nameSet
	eventMu sync.Mutex

	liveMu sync.Mutex
	live   map[string]bool // job ids running in this process: never cleaned up

	mu      sync.Mutex
	queue   []*task // accepted, waiting for a worker, in submission order
	active  int     // running workers, at most Concurrency
	cancels map[string]context.CancelFunc
}

// task is one accepted job.
type task struct {
	j      *jobState
	ctx    context.Context
	cancel context.CancelFunc
	out    chan Result
}

// New validates the options and applies the defaults.
func New(o Options) (*Downloader, error) {
	switch {
	case o.HTTP == nil || o.Clock == nil || o.Sleeper == nil || o.Rand == nil:
		return nil, errors.New("proxy: HTTP, Clock, Sleeper and Rand are required")
	case o.Staging == nil || o.Dest == nil || o.Finalizer == nil || o.Confine == nil:
		return nil, errors.New("proxy: Staging, Dest, Finalizer and Confine are required")
	case o.History == nil || o.Journal == nil:
		return nil, errors.New("proxy: History and Journal are required")
	case len(o.Config.Platforms) == 0:
		return nil, errors.New("proxy: Config.Platforms is required (ids from /platforms)")
	}
	cfg := o.Config
	if len(cfg.AllowedHosts) == 0 {
		cfg.AllowedHosts = DefaultAllowedHosts
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = mothdex.DefaultUserAgent("")
	}
	cfg.MaxFileSize = defaultInt64(cfg.MaxFileSize, DefaultMaxFileSize)
	cfg.Concurrency = min(max(defaultInt(cfg.Concurrency, DefaultConcurrency), 1), maxConcurrency)
	cfg.MaxAttempts = defaultInt(cfg.MaxAttempts, DefaultMaxAttempts)
	if cfg.NotFoundRetries < 0 {
		cfg.NotFoundRetries = DefaultNotFoundRetries
	}
	cfg.HeaderTimeout = defaultDuration(cfg.HeaderTimeout, DefaultHeaderTimeout)
	cfg.FirstByteTimeout = defaultDuration(cfg.FirstByteTimeout, DefaultFirstByte)
	cfg.IdleTimeout = defaultDuration(cfg.IdleTimeout, DefaultIdleTimeout)
	switch cfg.OnExists {
	case "":
		cfg.OnExists = OnExistsSkip
	case OnExistsSkip, OnExistsRename:
	case OnExistsOverwrite:
		if !cfg.Force {
			return nil, errors.New("proxy: on_exists=overwrite needs the --force flag")
		}
	default:
		return nil, fmt.Errorf("proxy: unknown on_exists %q", cfg.OnExists)
	}
	if o.IDs == nil {
		o.IDs = rand.Reader
	}
	return &Downloader{o: o, cfg: cfg, ledger: newLedger(), names: newNameSet(), live: map[string]bool{}, cancels: map[string]context.CancelFunc{}}, nil
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func defaultInt64(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	return v
}

func defaultDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// Run submits the jobs in order and returns one Result per job, in order, after every job
// reached its terminal state. At most Config.Concurrency jobs run at once across all callers.
func (d *Downloader) Run(ctx context.Context, jobs []Job) []Result {
	results := make([]Result, len(jobs))
	outs := make([]<-chan Result, len(jobs))
	for i, job := range jobs {
		if _, ch, err := d.Submit(ctx, job); err != nil {
			results[i] = Result{State: StateFailed, Err: &Error{Code: CodeInternal, Message: "job rejected", Err: err}}
		} else {
			outs[i] = ch
		}
	}
	for i, ch := range outs {
		if ch != nil {
			results[i] = <-ch
		}
	}
	return results
}

// Download submits one job and waits for its result.
func (d *Downloader) Download(ctx context.Context, job Job) Result {
	_, ch, err := d.Submit(ctx, job)
	if err != nil {
		return Result{State: StateFailed, Err: &Error{Code: CodeInternal, Message: "job rejected", Err: err}}
	}
	return <-ch
}

// Submit accepts a job: it returns the job id at once, emits Queued, and runs the job when a
// worker is free (first in, first out, at most Concurrency at a time for the whole Downloader).
// The Result arrives on the channel once the job reached its terminal state. ctx cancels this
// job and, derived per job, Cancel(id) cancels it alone.
func (d *Downloader) Submit(ctx context.Context, job Job) (string, <-chan Result, error) {
	id := job.ID
	if id == "" {
		var err error
		if id, err = d.NewJobID(); err != nil {
			return "", nil, err
		}
	} else if !jobIDRe.MatchString(id) {
		return "", nil, errors.New("proxy: invalid job id")
	}
	jctx, cancel := context.WithCancel(ctx)
	d.mu.Lock()
	if _, dup := d.cancels[id]; dup {
		d.mu.Unlock()
		cancel()
		return "", nil, errors.New("proxy: job id already in use")
	}
	d.cancels[id] = cancel
	d.mu.Unlock()

	d.emit(Event{JobID: id, State: StateQueued, Expected: job.Link.Size})
	t := &task{j: &jobState{id: id, job: job, started: d.o.Clock.Now(), rawName: job.Link.Filename},
		ctx: jctx, cancel: cancel, out: make(chan Result, 1)}
	d.mu.Lock()
	d.queue = append(d.queue, t)
	start := d.active < d.cfg.Concurrency
	if start {
		d.active++
	}
	d.mu.Unlock()
	if start {
		go d.worker()
	}
	return id, t.out, nil
}

// Cancel cancels one job by id, queued or running; it reports whether the id was known.
func (d *Downloader) Cancel(id string) bool {
	d.mu.Lock()
	cancel := d.cancels[id]
	d.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// worker takes jobs from the queue in order until it is empty.
func (d *Downloader) worker() {
	for {
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.active--
			d.mu.Unlock()
			return
		}
		t := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()

		res := d.runTask(t)
		d.mu.Lock()
		delete(d.cancels, t.j.id)
		d.mu.Unlock()
		t.cancel()
		t.out <- res
	}
}

// runTask runs one job and turns a panic into Failed(internal), after cleaning up.
func (d *Downloader) runTask(t *task) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = d.finish(t.j, StateFailed, &Error{Code: CodeInternal, Message: "unexpected failure"})
		}
	}()
	return d.run(t.ctx, t.j)
}

// jobState is everything one job owns; finish releases it.
type jobState struct {
	id        string
	job       Job
	url       string
	name      string // sanitized, possibly renamed
	rawName   string
	destRel   string // destination directory inside Dest
	started   time.Time
	attempts  int
	overwrite bool         // an existing regular file is to be replaced (--force)
	baseName  string       // the sanitized name before any " (n)" suffix
	renameN   int          // the n of the current " (n)" name, 0 for the original
	nameOpts  []NameOption // the options Sanitize ran with

	staging     string // name of the staging file, inside stagingFS(j)
	stageInDest bool   // staging is <destdir>/.<jobID>.part: same device, plain rename (M2)
	file        File
	tmp         string // name of the cross-device copy inside Dest, "" until made
	claims      []claim
	nameHeld    bool
	journaled   bool
	bytes       int64
	sum         string
	crc, hash   string
	lastEmit    time.Time
	transferAt  time.Time
	transferred int64
}

// run takes one job to its terminal state: it never leaves a staging file behind, releases
// every reservation and writes one history record.
func (d *Downloader) run(ctx context.Context, j *jobState) Result {
	id := j.id
	d.liveMu.Lock()
	d.live[id] = true
	d.liveMu.Unlock()
	defer func() {
		d.liveMu.Lock()
		delete(d.live, id)
		d.liveMu.Unlock()
	}()

	state, derr := d.execute(ctx, j)
	if derr != nil && ctx.Err() != nil && (derr.Code == CodeNetwork || derr.Code == CodeCanceled) {
		state, derr = StateCanceled, &Error{Code: CodeCanceled, Message: "canceled", Err: ctx.Err()}
	}
	return d.finish(j, state, derr)
}

// finish releases everything, writes the history record and reports the terminal event.
func (d *Downloader) finish(j *jobState, state State, derr *Error) Result {
	d.cleanupJob(j, state)
	finished := d.o.Clock.Now()
	res := Result{JobID: j.id, State: state, Name: j.name, Bytes: j.bytes, SHA256: j.sum, Attempts: j.attempts, Err: derr}
	if state == StateDone {
		res.Path = d.finalPath(j)
	}
	rec := Record{JobID: j.id, Slug: j.job.Slug, Platform: j.job.Platform, State: state, RawName: j.rawName,
		Name: j.name, Path: res.Path, Bytes: j.bytes, SHA256: j.sum, CRC: j.crc, Hash: j.hash,
		Attempts: j.attempts, Started: j.started, Finished: finished}
	if derr != nil {
		rec.Code = derr.Code
	}
	res.HistoryErr = d.o.History.Append(rec)
	d.event(j, state, derr)
	return res
}

// finalPath is the absolute final path when DestRoot is known, else relative to Dest.
func (d *Downloader) finalPath(j *jobState) string {
	rel := joinName(j.destRel, j.name)
	if d.o.DestRoot == "" {
		return rel
	}
	return filepath.Join(d.o.DestRoot, filepath.FromSlash(rel))
}

// cleanupJob removes the job's files and releases its reservations (idempotent).
func (d *Downloader) cleanupJob(j *jobState, state State) {
	if j.file != nil {
		_ = j.file.Close()
		j.file = nil
	}
	if j.staging != "" && state != StateDone {
		_ = d.stagingFS(j).Remove(j.staging)
	}
	if j.tmp != "" {
		_ = d.o.Dest.Remove(j.tmp)
	}
	if j.claims != nil {
		d.ledger.release(j.claims)
		j.claims = nil
	}
	if j.nameHeld {
		d.names.release(j.destRel, j.name)
		j.nameHeld = false
	}
	if j.journaled {
		_ = d.o.Journal.Remove(j.id)
		j.journaled = false
	}
}

// NewJobID is 128 random bits as 26 lower-case base32 characters, never server text.
func (d *Downloader) NewJobID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(d.o.IDs, b[:]); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])), nil
}

// execute runs the state machine and returns the terminal state.
func (d *Downloader) execute(ctx context.Context, j *jobState) (State, *Error) {
	if state, err := d.preflight(ctx, j); state != "" || err != nil {
		return orFailed(state, err), err
	}
	if err := d.openStaging(j); err != nil {
		return StateFailed, err
	}
	if err := d.transfer(ctx, j); err != nil {
		return stateOf(err), err
	}
	return d.finalize(ctx, j)
}

func orFailed(state State, err *Error) State {
	if state == "" && err != nil {
		return stateOf(err)
	}
	return state
}

func stateOf(err *Error) State {
	if err != nil && err.Code == CodeCanceled {
		return StateCanceled
	}
	return StateFailed
}

// joinName joins a directory name inside an FS and a file name with a slash.
func joinName(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return dir + "/" + name
}

// event reports a state change; progress events go through progress.
func (d *Downloader) event(j *jobState, s State, err *Error) {
	d.emit(Event{JobID: j.id, State: s, Attempt: j.attempts, Bytes: j.transferred, Expected: j.job.Link.Size, Err: err})
}

func (d *Downloader) emit(ev Event) {
	if d.o.OnEvent == nil {
		return
	}
	d.eventMu.Lock()
	defer d.eventMu.Unlock()
	d.o.OnEvent(ev)
}

// progress emits a throttled transferring event (at most 4 per second).
func (d *Downloader) progress(j *jobState) {
	now := d.o.Clock.Now()
	if now.Sub(j.lastEmit) < progressInterval {
		return
	}
	j.lastEmit = now
	var rate int64
	if el := now.Sub(j.transferAt); el > 0 {
		rate = int64(float64(j.transferred) / el.Seconds())
	}
	d.emit(Event{JobID: j.id, State: StateTransferring, Attempt: j.attempts, Bytes: j.transferred,
		Expected: j.job.Link.Size, RateBps: rate})
}

// stagingFS is the file system holding the job's staging file.
func (d *Downloader) stagingFS(j *jobState) FS {
	if j.stageInDest {
		return d.o.Dest
	}
	return d.o.Staging
}
