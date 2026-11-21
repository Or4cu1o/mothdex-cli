// Package fakefs implements proxy.FS and proxy.Finalizer on top of a real,
// confined temporary directory (os.Root) and adds what real disks do not do on
// demand: ENOSPC mid-write, EXDEV, unsupported no-replace rename, scripted free
// space and device ids, hooks that run between two steps of the downloader, and
// a call log. It is the double behind the disk, partial-file and race cases of
// the T-16 threat model (cases 18, 19, 21, 34 to 37, 40).
package fakefs

import (
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
)

// Operations that faults and hooks can target.
const (
	OpOpenExcl = "OpenExcl"
	OpLstat    = "Lstat"
	OpOpen     = "Open"
	OpMkdirAll = "MkdirAll"
	OpRemove   = "Remove"
	OpReadDir  = "ReadDir"
	OpLink     = "Link"
	OpFree     = "Free"
	OpSyncDir  = "SyncDir"
	OpRename   = "Rename" // proxy.Finalizer
	OpWrite    = "Write"
	OpSync     = "Sync"
	OpTruncate = "Truncate"
	OpClose    = "Close"
)

// Defaults reported by Free when no volume matches.
const (
	defaultFree = int64(1) << 50
	defaultDev  = uint64(1)
)

// Call is one operation the downloader performed.
type Call struct {
	Op   string
	Name string // for Link and Rename: the source name
	To   string // for Link and Rename: the target name
}

type fault struct {
	op, pattern string
	err         error
	remaining   int // < 0: always
}

type hook struct {
	op, pattern string
	fn          func()
	done        bool
}

type volume struct {
	prefix string
	free   int64
	dev    uint64
}

// FS is the fake file system. Create it with New.
type FS struct {
	dir  string
	root *os.Root

	renameMu   sync.Mutex // makes the no-replace check and the rename one step
	mu         sync.Mutex
	faults     []*fault
	hooks      []*hook
	volumes    []volume
	calls      []Call
	syncs      map[string]int
	written    int64
	writeLimit int64 // < 0: unlimited
	writeErr   error
}

var (
	_ proxy.FS        = (*FS)(nil)
	_ proxy.Finalizer = Finalizer{}
)

// New creates a fake file system over a fresh temp directory.
func New(t testing.TB) *FS {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("fakefs: resolve temp dir: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("fakefs: open root: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return &FS{dir: dir, root: root, syncs: map[string]int{}, writeLimit: -1}
}

// Dir is the real directory behind the fake, for assertions and for planting
// hostile entries (symlinks, pre-existing files) before the downloader runs.
func (f *FS) Dir() string { return f.dir }

// Path returns the real path of a name inside the fake.
func (f *FS) Path(name string) string { return filepath.Join(f.dir, filepath.FromSlash(name)) }

// Fail makes the next times calls of op on names matching pattern return err.
// Pattern rules: "*" or "**" match every name, a pattern without "/" also
// matches the base name (so "*.part" selects dest/x.part), anything else is a
// path.Match pattern on the whole name. times <= 0 means every call.
func (f *FS) Fail(op, pattern string, err error, times int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if times <= 0 {
		times = -1
	}
	f.faults = append(f.faults, &fault{op: op, pattern: pattern, err: err, remaining: times})
}

// Before registers fn to run once, just before the next op on a matching name,
// so a test can change the world between two steps of the downloader (a second
// writer creating the final name, a path swapped for a symlink).
func (f *FS) Before(op, pattern string, fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks = append(f.hooks, &hook{op: op, pattern: pattern, fn: fn})
}

// LimitWrites lets total more bytes through, across all files, then fails
// every write with err after writing what still fits (a disk that fills up).
// Bytes freed by Truncate and Remove are credited back, as on a real disk.
func (f *FS) LimitWrites(total int64, err error) {
	if err == nil {
		panic("fakefs: LimitWrites needs a non-nil error")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeLimit = f.written + total
	f.writeErr = err
}

// SetVolume gives names under prefix a free-space figure and a device id. The
// longest matching prefix wins, "" or "." matches everything but loses to any
// real prefix, and a later registration of the same prefix replaces an earlier one.
func (f *FS) SetVolume(prefix string, free int64, dev uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes = append(f.volumes, volume{prefix: path.Clean(prefix), free: free, dev: dev})
}

// Calls returns the operations seen so far, in order.
func (f *FS) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// Syncs returns how many times Sync was called on the file called name.
func (f *FS) Syncs(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.syncs[path.Clean(name)]
}

// enter logs the call, runs a due hook and returns an injected fault, if any.
func (f *FS) enter(op, name, to string) error {
	f.mu.Lock()
	f.calls = append(f.calls, Call{Op: op, Name: name, To: to})
	var run []func()
	for _, h := range f.hooks {
		if !h.done && h.op == op && matches(h.pattern, name, to) {
			h.done = true
			run = append(run, h.fn)
		}
	}
	f.mu.Unlock()
	for _, fn := range run { // outside the lock: a hook may use the FS
		fn()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ft := range f.faults {
		if ft.remaining == 0 || ft.op != op || !matches(ft.pattern, name, to) {
			continue
		}
		if ft.remaining > 0 {
			ft.remaining--
		}
		return &fs.PathError{Op: strings.ToLower(op), Path: name, Err: ft.err}
	}
	return nil
}

// matches reports whether pattern selects name or, for Link and Rename, the
// target name: a test usually cares about the final name, not the staging one.
func matches(pattern, name, to string) bool {
	return match(pattern, name) || (to != "" && match(pattern, to))
}

// match implements the pattern rules of Fail and Before: "*" and "**" select
// every name (staging files live in sub-directories, and path.Match's * stops
// at a slash); a pattern without a slash is matched against the base name too;
// anything else is a path.Match pattern on the whole name.
func match(pattern, name string) bool {
	name = path.Clean(filepath.ToSlash(name))
	if pattern == "*" || pattern == "**" {
		return true
	}
	if ok, err := path.Match(pattern, name); err == nil && ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, err := path.Match(pattern, path.Base(name))
		return err == nil && ok
	}
	return false
}

// OpenExcl implements proxy.FS.
func (f *FS) OpenExcl(name string) (proxy.File, error) {
	if err := f.enter(OpOpenExcl, name, ""); err != nil {
		return nil, err
	}
	fh, err := f.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return &file{fs: f, name: path.Clean(name), f: fh}, nil
}

// Open implements proxy.FS.
func (f *FS) Open(name string) (io.ReadCloser, error) {
	if err := f.enter(OpOpen, name, ""); err != nil {
		return nil, err
	}
	return f.root.Open(name)
}

// Lstat implements proxy.FS.
func (f *FS) Lstat(name string) (fs.FileInfo, error) {
	if err := f.enter(OpLstat, name, ""); err != nil {
		return nil, err
	}
	return f.root.Lstat(name)
}

// MkdirAll implements proxy.FS.
func (f *FS) MkdirAll(name string, perm fs.FileMode) error {
	if err := f.enter(OpMkdirAll, name, ""); err != nil {
		return err
	}
	return f.root.MkdirAll(name, perm)
}

// Remove implements proxy.FS.
func (f *FS) Remove(name string) error {
	if err := f.enter(OpRemove, name, ""); err != nil {
		return err
	}
	if info, err := f.root.Lstat(name); err == nil && info.Mode().IsRegular() {
		f.credit(info.Size())
	}
	return f.root.Remove(name)
}

func (f *FS) credit(n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = max(f.written-n, 0)
}

// ReadDir implements proxy.FS.
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if err := f.enter(OpReadDir, name, ""); err != nil {
		return nil, err
	}
	return fs.ReadDir(f.root.FS(), path.Clean(filepath.ToSlash(name)))
}

// Link implements proxy.FS.
func (f *FS) Link(from, to string) error {
	if err := f.enter(OpLink, from, to); err != nil {
		return err
	}
	return f.root.Link(from, to)
}

// Free implements proxy.FS from the scripted volumes.
func (f *FS) Free(name string) (int64, uint64, error) {
	if err := f.enter(OpFree, name, ""); err != nil {
		return 0, 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	clean := path.Clean(filepath.ToSlash(name))
	best, bestLen := volume{free: defaultFree, dev: defaultDev}, -1
	for _, v := range f.volumes {
		if v.prefix != "." && clean != v.prefix && !strings.HasPrefix(clean, v.prefix+"/") {
			continue
		}
		l := len(v.prefix)
		if v.prefix == "." {
			l = 0 // the catch-all loses to every real prefix
		}
		if l >= bestLen { // on equal length the last registration wins
			best, bestLen = v, l
		}
	}
	return best.free, best.dev, nil
}

// SyncDir implements proxy.FS. It counts as a Sync of the directory name (see Syncs).
func (f *FS) SyncDir(name string) error {
	if err := f.enter(OpSyncDir, name, ""); err != nil {
		return err
	}
	d, err := f.root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	f.mu.Lock()
	f.syncs[path.Clean(name)]++
	f.mu.Unlock()
	if runtime.GOOS == "windows" {
		return nil // a directory cannot be flushed there; the real FS does the same
	}
	return d.Sync()
}

// file wraps an os.File with fault injection and a sync spy.
type file struct {
	fs   *FS
	name string
	f    *os.File
}

func (x *file) Write(p []byte) (int, error) {
	if err := x.fs.enter(OpWrite, x.name, ""); err != nil {
		return 0, err
	}
	x.fs.mu.Lock()
	allowed, limitErr := len(p), error(nil)
	if x.fs.writeLimit >= 0 && x.fs.written+int64(len(p)) > x.fs.writeLimit {
		allowed = int(max(x.fs.writeLimit-x.fs.written, 0))
		limitErr = &fs.PathError{Op: "write", Path: x.name, Err: x.fs.writeErr}
	}
	x.fs.written += int64(allowed)
	x.fs.mu.Unlock()
	n, err := x.f.Write(p[:allowed])
	if err != nil {
		return n, err
	}
	return n, limitErr
}

func (x *file) Read(p []byte) (int, error)                { return x.f.Read(p) }
func (x *file) Seek(off int64, whence int) (int64, error) { return x.f.Seek(off, whence) }
func (x *file) Stat() (fs.FileInfo, error)                { return x.f.Stat() }
func (x *file) Truncate(size int64) error {
	if err := x.fs.enter(OpTruncate, x.name, ""); err != nil {
		return err
	}
	if info, err := x.f.Stat(); err == nil && info.Size() > size {
		x.fs.credit(info.Size() - size)
	}
	return x.f.Truncate(size)
}

func (x *file) Sync() error {
	if err := x.fs.enter(OpSync, x.name, ""); err != nil {
		return err
	}
	x.fs.mu.Lock()
	x.fs.syncs[x.name]++
	x.fs.mu.Unlock()
	return x.f.Sync()
}

func (x *file) Close() error {
	if err := x.fs.enter(OpClose, x.name, ""); err != nil {
		_ = x.f.Close()
		return err
	}
	return x.f.Close()
}

var _ io.ReadWriteSeeker = (*file)(nil)
