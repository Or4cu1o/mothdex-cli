package proxy

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
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
	NotFoundRetries  int      // retries after a 404, default 3; a negative value means the default
	HeaderTimeout    time.Duration
	FirstByteTimeout time.Duration
	IdleTimeout      time.Duration
	OnExists         OnExists        // default skip
	Verify           bool            // zip CRC and upstream hashes; the size is always checked
	Platforms        map[string]bool // ids from /platforms; a job for any other id is refused
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
	// OnEvent receives state changes and throttled progress. Calls are serialized.
	OnEvent func(Event)
	Config  Config
}

// Job is one download. Every field comes from untrusted data and is validated at use.
type Job struct {
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
	return &Downloader{o: o, cfg: cfg, ledger: newLedger(), names: newNameSet(), live: map[string]bool{}}, nil
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

// Run downloads the jobs with the configured worker pool and returns one Result per job, in
// order, after every job reached its terminal state.
func (d *Downloader) Run(ctx context.Context, jobs []Job) []Result {
	results := make([]Result, len(jobs))
	sem := make(chan struct{}, d.cfg.Concurrency)
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
			}
			results[i] = d.Download(ctx, job)
		}()
	}
	wg.Wait()
	return results
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

	staging     string // name inside Staging
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

// Download runs one job to its terminal state: it never panics on hostile input, never leaves
// a staging file behind, releases every reservation and writes one history record.
func (d *Downloader) Download(ctx context.Context, job Job) Result {
	j := &jobState{job: job, started: d.o.Clock.Now(), rawName: job.Link.Filename}
	id, err := d.newJobID()
	if err != nil {
		return d.fail(j, &Error{Code: CodeInternal, Message: "cannot create a job id", Err: err})
	}
	j.id = id
	d.liveMu.Lock()
	d.live[id] = true
	d.liveMu.Unlock()
	defer func() {
		d.liveMu.Lock()
		delete(d.live, id)
		d.liveMu.Unlock()
	}()

	d.event(j, StateQueued, nil)
	state, derr := d.execute(ctx, j)
	if derr != nil && ctx.Err() != nil && (derr.Code == CodeNetwork || derr.Code == CodeCanceled) {
		state, derr = StateCanceled, &Error{Code: CodeCanceled, Message: "canceled", Err: ctx.Err()}
	}
	return d.finish(j, state, derr)
}

func (d *Downloader) fail(j *jobState, e *Error) Result { return d.finish(j, StateFailed, e) }

// finish releases everything, writes the history record and reports the terminal event.
func (d *Downloader) finish(j *jobState, state State, derr *Error) Result {
	d.cleanupJob(j, state)
	finished := d.o.Clock.Now()
	res := Result{JobID: j.id, State: state, Name: j.name, Bytes: j.bytes, SHA256: j.sum, Attempts: j.attempts, Err: derr}
	if state == StateDone {
		res.Path = joinName(j.destRel, j.name)
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

// cleanupJob removes the job's files and releases its reservations (idempotent).
func (d *Downloader) cleanupJob(j *jobState, state State) {
	if j.file != nil {
		_ = j.file.Close()
		j.file = nil
	}
	if j.staging != "" && state != StateDone {
		_ = d.o.Staging.Remove(j.staging)
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

// newJobID is 128 random bits as 26 lower-case base32 characters, never server text.
func (d *Downloader) newJobID() (string, error) {
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
