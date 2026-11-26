package log

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/buildinfo"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
)

const (
	maxBufferBytes  = 256 << 10 // lines kept in memory until Attach
	maxSessionBytes = 20 << 20  // cap of a session file and of latest.log
	maxCrashBytes   = 1 << 20
	defaultKeep     = 20
)

const (
	msgStart     = "session start"
	msgInfo      = "session info"
	msgEnd       = "session end"
	msgDropped   = "lines dropped"
	msgTruncated = "log truncated"
	msgNoModes   = "log folder has no file permissions"
)

// SessionOptions describe the process before anything is configured.
type SessionOptions struct {
	Binary string              // cli | gui
	Args   []string            // the arguments without the program name
	Flags  map[string]FlagRule // which flag values the header may show; nil redacts them all
	Build  buildinfo.Info
	Now    func() time.Time // nil: time.Now
	// DefaultDir is where Close writes when Attach was never called; nil: the default paths.logs.
	DefaultDir func() (dir, home string, err error)
}

// AttachOptions are the settings of the log files, read from the configuration.
type AttachOptions struct {
	Dir     string   // paths.logs
	Level   string   // log.session_level; empty or unknown means debug
	Keep    int      // log.sessions_keep; 0 means 20
	Secrets []string // values never written (api.base_url)
	Home    string
}

// Info is what is only known after the configuration is read; empty fields keep their value.
type Info struct {
	Mode         string // command | shell | tui | gui
	Profile      string
	Lang         string
	ConfigSource string // "defaults" or the configuration file
}

// Session is the log of one process: its own file under sessions/, latest.log while it is the
// newest process, and the crash file. It starts in main, keeps lines in memory until Attach says
// where they go, and never makes the program fail.
type Session struct {
	opts    SessionOptions
	started time.Time
	redact  *redactor
	level   slog.LevelVar
	errors  atomic.Int64
	handler slog.Handler

	mu       sync.Mutex
	attached bool
	closed   bool
	buf      [][]byte
	bufBytes int
	dropped  bool
	file     *os.File
	size     int64
	latest   *latestWriter
	dir, id  string
	home     string
	secrets  []string
	info     Info
	process  *slog.Logger // set by New: the session plus mothdex.log
}

// StartSession begins the log of this process. It never returns nil and touches no file.
func StartSession(o SessionOptions) *Session {
	if o.Now == nil {
		o.Now = time.Now
	}
	home, _ := os.UserHomeDir()
	// The configured API address is registered by Attach; until then these cover it.
	s := &Session{opts: o, started: o.Now(), home: home,
		redact: newRedactor(defaultAPI, os.Getenv("MOTHDEX_API_BASE_URL"))}
	s.level.Set(slog.LevelDebug)
	text := slog.NewTextHandler(sessionWriter{s}, &slog.HandlerOptions{Level: &s.level})
	s.handler = redactHandler{next: countHandler{text, &s.errors}, r: s.redact}
	slog.New(s.handler).Info(msgStart,
		"session", s.baseID(), "version", o.Build.Version, "commit", o.Build.Commit, "build_date", o.Build.Date,
		"os", runtime.GOOS, "arch", runtime.GOARCH, "binary", o.Binary, "pid", os.Getpid(),
		"args", strings.Join(maskArgs(o.Args, o.Flags, newScrubber(home, "")), " "),
		"started_at", s.started.Format(time.RFC3339))
	return s
}

func (s *Session) baseID() string {
	return fmt.Sprintf("%s-%s-%d", s.started.UTC().Format("20060102-150405"), s.opts.Binary, os.Getpid())
}

// Handler writes to the session file and latest.log, redacting every record.
func (s *Session) Handler() slog.Handler { return s.handler }

// Logger is the process logger while New has one open (session plus mothdex.log), else the
// session alone.
func (s *Session) Logger() *slog.Logger {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.process != nil {
		return s.process
	}
	return slog.New(s.handler)
}

func (s *Session) setProcessLogger(l *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.process = l
}

// Attached reports whether Attach ran, successfully or not.
func (s *Session) Attached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached
}

// Attach opens the files in a.Dir, writes the lines kept so far and applies retention. Only the
// first call counts. A folder that is not safe is replaced by the default one when that one is.
// An error is a warning for the person: the program goes on, and without a safe folder lines are
// discarded.
func (s *Session) Attach(a AttachOptions) error {
	s.redact.add(a.Secrets...)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attached || s.closed {
		return nil
	}
	s.attached, s.dir = true, a.Dir
	s.secrets = append(s.secrets, a.Secrets...)
	if a.Home != "" {
		s.home = a.Home
	}
	level, levelErr := slog.LevelDebug, error(nil)
	if a.Level != "" {
		if level, levelErr = ParseLevel(a.Level); levelErr != nil {
			level = slog.LevelDebug
		}
	}
	s.level.Set(level)
	head := s.drain(level)
	err := s.openFiles(head)
	if err != nil && s.id == "" && errors.Is(err, errUnsafeDir) {
		err = s.fallBack(head, err)
	}
	if s.id == "" {
		return errors.Join(levelErr, err)
	}
	if permissionless(s.dir) {
		s.append(fmt.Appendf(nil, "time=%s level=WARN msg=%q\n", s.opts.Now().Format(time.RFC3339Nano), msgNoModes))
	}
	keep := a.Keep
	if keep < 1 {
		keep = defaultKeep
	}
	prune(filepath.Join(s.dir, SessionsDir), sessionFilePattern, keep, s.id+".log")
	prune(filepath.Join(s.dir, CrashDir), crashFilePattern, keepCrashes, "")
	prune(filepath.Join(s.dir, BundlesDir), bundleFilePattern, keepBundles, "")
	return errors.Join(levelErr, err)
}

// fallBack moves the session to the default folder when the configured one is refused; the caller
// holds the lock. The error returned is the warning to show, also when the move worked.
func (s *Session) fallBack(head []byte, refused error) error {
	locate := s.opts.DefaultDir
	if locate == nil {
		locate = defaultDir
	}
	configured := s.dir
	dir, _, err := locate()
	if err != nil || filepath.Clean(dir) == filepath.Clean(configured) {
		return refused
	}
	s.dir = dir
	if err := s.openFiles(head); s.id == "" {
		s.dir = configured
		return errors.Join(refused, err)
	}
	return fmt.Errorf("logging to %s instead of %s: %w", dir, configured, refused)
}

// Dir is the log folder in use, which is not the configured one after a fall back. It is empty
// while nothing is written.
func (s *Session) Dir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id == "" {
		return ""
	}
	return s.dir
}

// NoPermissions reports a folder on a file system without file permissions (FAT, exFAT, NTFS), to
// warn the person that other users of the machine may read the logs.
func NoPermissions(dir string) bool { return permissionless(dir) }

// openFiles creates the session file and claims latest.log; the caller holds the lock.
func (s *Session) openFiles(head []byte) error {
	if !filepath.IsAbs(s.dir) {
		return fmt.Errorf("%w: %q is not an absolute path", errUnsafeDir, s.dir)
	}
	if err := safeDir(s.dir); err != nil {
		return err
	}
	sessions := filepath.Join(s.dir, SessionsDir)
	if err := safeDir(sessions); err != nil {
		return err
	}
	f, id, err := createNew(sessions, s.baseID(), ".log")
	if err != nil {
		return err
	}
	s.file, s.id = f, id
	n, err := f.Write(head)
	s.size = int64(n)
	// A crash in another goroutine ends the process before anything can log it: the runtime
	// writes the trace to the session file itself.
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	latest, lerr := takeLatest(s.dir, fmt.Sprintf("%d-%d", os.Getpid(), s.started.UnixNano()), head)
	if lerr == nil {
		s.latest = latest
	}
	return errors.Join(err, lerr)
}

// drain returns the kept lines at or above level, redacted again now that every secret is known.
// The caller holds the lock.
func (s *Session) drain(level slog.Level) []byte {
	var out bytes.Buffer
	for i, line := range s.buf {
		if i == 1 && s.dropped {
			fmt.Fprintf(&out, "time=%s level=WARN msg=%q\n", s.started.Format(time.RFC3339Nano), msgDropped)
		}
		if i == 0 || lineLevel(line) >= level { // the header is always written
			out.WriteString(s.redact.text(string(line)))
		}
	}
	s.buf, s.bufBytes = nil, 0
	return out.Bytes()
}

func lineLevel(line []byte) slog.Level {
	if _, rest, ok := bytes.Cut(line, []byte(" level=")); ok {
		name, _, _ := bytes.Cut(rest, []byte(" "))
		var l slog.Level
		if l.UnmarshalText(name) == nil {
			return l
		}
	}
	return slog.LevelError
}

// sessionWriter receives one formatted record per call from the text handler.
type sessionWriter struct{ s *Session }

// Write never fails: a log that cannot be written must not reach the program.
func (w sessionWriter) Write(p []byte) (int, error) {
	s := w.s
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
	case !s.attached:
		s.keep(bytes.Clone(p))
	default:
		s.append(p)
	}
	return len(p), nil
}

// keep holds a line until Attach; past the cap the oldest go, never the header.
func (s *Session) keep(line []byte) {
	s.buf = append(s.buf, line)
	s.bufBytes += len(line)
	for s.bufBytes > maxBufferBytes && len(s.buf) > 2 {
		s.bufBytes -= len(s.buf[1])
		s.buf = slices.Delete(s.buf, 1, 2)
		s.dropped = true
	}
}

func (s *Session) append(p []byte) {
	if s.latest != nil {
		s.latest.write(p)
	}
	if s.file == nil || s.size >= maxSessionBytes {
		return
	}
	if s.size+int64(len(p)) > maxSessionBytes {
		_, _ = fmt.Fprintf(s.file, "time=%s level=WARN msg=%q\n", s.opts.Now().Format(time.RFC3339Nano), msgTruncated)
		s.size = maxSessionBytes
		return
	}
	n, _ := s.file.Write(p)
	s.size += int64(n)
}

// Update records what became known about the session.
func (s *Session) Update(i Info) {
	s.mu.Lock()
	merge := func(old *string, v string) {
		if v != "" {
			*old = v
		}
	}
	merge(&s.info.Mode, i.Mode)
	merge(&s.info.Profile, i.Profile)
	merge(&s.info.Lang, i.Lang)
	merge(&s.info.ConfigSource, i.ConfigSource)
	now, scrub := s.info, newScrubber(s.home, "")
	s.mu.Unlock()
	slog.New(s.handler).Info(msgInfo, "mode", now.Mode, "profile", now.Profile, "lang", now.Lang,
		"config_source", scrub.apply(now.ConfigSource))
}

// SessionPath is this process's own log file, empty while there is none.
func (s *Session) SessionPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionPathLocked()
}

func (s *Session) sessionPathLocked() string {
	if s.id == "" {
		return ""
	}
	return filepath.Join(s.dir, SessionsDir, s.id+".log")
}

// ID is the session id, empty while there is no session file.
func (s *Session) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// LogPath is the file to tell the person about: latest.log while this process owns it, else the
// session file. Empty when nothing is written to disk.
func (s *Session) LogPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest != nil && s.latest.owns() {
		return filepath.Join(s.dir, LatestName)
	}
	return s.sessionPathLocked()
}

// attachDefault is the way out for a process whose configuration never loaded.
func (s *Session) attachDefault() error {
	if s.Attached() {
		return nil
	}
	locate := s.opts.DefaultDir
	if locate == nil {
		locate = defaultDir
	}
	dir, home, err := locate()
	if err != nil {
		return err
	}
	return s.Attach(AttachOptions{Dir: dir, Home: home})
}

// defaultDir is paths.logs with an empty configuration, portable mode included.
func defaultDir() (string, string, error) {
	plat, err := config.CurrentPlatform()
	if err != nil {
		return "", "", err
	}
	paths, err := config.ResolvePaths(plat, config.Config{}, "")
	if err != nil {
		return "", "", err
	}
	return paths.Logs, plat.HomeDir, nil
}

// WriteCrash writes crash/<session id>.txt with the panic value and the stack, and returns its
// path.
func (s *Session) WriteCrash(recovered any, stack []byte, ref string) (string, error) {
	if err := s.attachDefault(); err != nil {
		return "", err
	}
	s.mu.Lock()
	dir, id := s.dir, s.id
	s.mu.Unlock()
	if id == "" {
		return "", errors.New("there is no log folder for the crash file")
	}
	crashes := filepath.Join(dir, CrashDir)
	if err := safeDir(crashes); err != nil {
		return "", err
	}
	b := s.opts.Build
	text := s.redact.text(fmt.Sprintf("session: %s\nversion: %s (%s, %s)\nsystem: %s/%s\nbinary: %s\ntime: %s\nref: %s\npanic: %v\n\n%s",
		id, b.Version, b.Commit, b.Date, runtime.GOOS, runtime.GOARCH, s.opts.Binary, s.opts.Now().Format(time.RFC3339), ref, recovered, stack))
	text = text[:min(len(text), maxCrashBytes)]
	f, name, err := createNew(crashes, id, ".txt")
	if err != nil {
		return "", err
	}
	_, werr := io.WriteString(f, text)
	if err := errors.Join(werr, f.Sync(), f.Close()); err != nil {
		return "", err
	}
	prune(crashes, crashFilePattern, keepCrashes, name+".txt")
	return filepath.Join(crashes, name+".txt"), nil
}

// Close writes the last line and releases the files. A session that was never attached is written
// to the default folder. Later calls do nothing.
func (s *Session) Close() error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil
	}
	err := s.attachDefault()
	slog.New(s.handler).Info(msgEnd, "duration_ms", s.opts.Now().Sub(s.started).Milliseconds(), "errors", s.errors.Load())
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.latest != nil {
		s.latest.close()
		s.latest = nil
	}
	if s.file != nil {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		err = errors.Join(err, s.file.Close())
		s.file = nil
	}
	return err
}

// countHandler counts the error records for the "session end" line.
type countHandler struct {
	slog.Handler
	n *atomic.Int64
}

func (h countHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.n.Add(1)
	}
	return h.Handler.Handle(ctx, r)
}

func (h countHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return countHandler{h.Handler.WithAttrs(a), h.n}
}

func (h countHandler) WithGroup(g string) slog.Handler {
	return countHandler{h.Handler.WithGroup(g), h.n}
}
