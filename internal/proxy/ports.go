// Package proxy is the resilient downloader (docs/contracts/cli/downloader.md): worker pool,
// retry policy, verification, confined destination directories and a finalize step that never
// replaces an existing file. Everything that touches the outside world is injected through
// the ports of this file, so tests drive the downloader with fakes and no real network,
// clock, resolver or file system faults.
package proxy

import (
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"time"
)

// HTTPDoer sends a request. *http.Client and fakeproxy's client satisfy it. The downloader
// forces its own policy (https, exact host, no redirects) on the requests it builds; it
// never relies on the doer for that.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Clock supplies time and timers; tests inject a fake and advance it by hand.
type Clock interface {
	Now() time.Time
	// NewTimer returns a channel that fires after d and a stop function that reports whether
	// it stopped the timer before it fired.
	NewTimer(d time.Duration) (<-chan time.Time, func() bool)
}

// Sleeper waits for d or until ctx ends, returning ctx.Err() in the latter case.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

// Rand supplies the jitter of the retry backoff. The job id source is a separate io.Reader.
type Rand interface {
	Int63n(n int64) int64
}

// FS is a confined file system: every name is relative to one root and may never leave it
// (os.Root semantics). The real implementation wraps an *os.Root; tests wrap a real temporary
// root and inject faults (ENOSPC, EXDEV, device ids, hooks between steps).
type FS interface {
	// OpenExcl creates a new regular file with O_CREATE|O_EXCL|O_NOFOLLOW and mode 0600. It is
	// used for staging files, the .<jobID>.mothdex-tmp cross-device copy and the placeholder
	// of the no-replace fallback chain. An existing name gives fs.ErrExist.
	OpenExcl(name string) (File, error)
	// Lstat never follows a final symlink.
	Lstat(name string) (fs.FileInfo, error)
	MkdirAll(name string, perm fs.FileMode) error
	Remove(name string) error
	ReadDir(name string) ([]fs.DirEntry, error)
	// Link makes a hard link; it is the fallback when a no-replace rename is unsupported.
	// An existing target gives fs.ErrExist; unsupported file systems return an error for which
	// errors.Is(err, errors.ErrUnsupported) holds.
	Link(from, to string) error
	// Free reports the free bytes of the file system holding name and a device id, for the
	// space ledger and the EXDEV decision.
	Free(name string) (free int64, dev uint64, err error)
}

// File is an open staging file. The same descriptor is reused across retries: Truncate(0)
// then Seek(0, io.SeekStart).
type File interface {
	io.Writer
	io.Reader
	io.Seeker
	Truncate(size int64) error
	Sync() error
	Close() error
}

// Finalizer moves a file inside one FS without replacing an existing target (T-16 F8). The
// real implementations are per OS: renameat2(RENAME_NOREPLACE) on Linux, MoveFileExW without
// MOVEFILE_REPLACE_EXISTING on Windows, renameatx_np(RENAME_EXCL) on macOS.
//
// Errors: fs.ErrExist when the target exists; an error satisfying errors.ErrUnsupported when
// the file system cannot do a no-replace rename (the downloader then falls back to Link, then
// to an O_EXCL placeholder); syscall.EXDEV across file systems. replace=true is the one
// replacing rename, used only for --force overwrite after an Lstat regular-file check.
type Finalizer interface {
	Rename(fsys FS, from, to string, replace bool) error
}

// Resolver looks up host addresses for the cover fetch SSRF guard (cache.md section 4).
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

// Record is the one history entry written for every job.
type Record struct {
	JobID string
	Slug  string
	State State
	Code  Code // "" unless the job failed
	// Further fields (platform, title, path, bytes, hashes) are added with the downloader.
}

// History receives one record per job; every job reaches exactly one terminal state.
type History interface {
	Append(rec Record) error
}

// Event reports job progress. Progress events are throttled to 4 per second.
type Event struct {
	JobID    string
	State    State
	Attempt  int
	Bytes    int64
	Expected int64
	RateBps  int64
	Err      *Error
}

// State is a job state.
type State string

// Job states; the last four are terminal.
const (
	StateQueued   State = "queued"
	StateRunning  State = "running"
	StateDone     State = "done"
	StateSkipped  State = "skipped"
	StateFailed   State = "failed"
	StateCanceled State = "canceled"
)
