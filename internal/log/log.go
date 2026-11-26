// Package log builds the application's slog logger: a size-rotated file under paths.logs and an
// optional console copy on stderr (log.* settings, docs/contracts/cli/config.md §2). Rotation is
// plain stdlib; there is no dependency. Logs are for diagnosis, so the logger never makes the
// program fail: a file that cannot be opened is reported to the caller and the logger carries on
// with whatever other destination exists.
package log

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
)

// FileName is the active log file inside the log directory; rotated copies get .1, .2, ...
const FileName = "mothdex.log"

// Options are the settings of New, mapped from the log.* config keys.
type Options struct {
	Level     string // debug | info | warn | error
	File      bool
	Console   bool
	Dir       string // directory of the log file (paths.logs)
	MaxSizeMB int    // rotate when the file would grow past this
	MaxFiles  int    // active file plus rotated copies kept (>= 1)
	Stderr    io.Writer
	Session   *Session // the process log; it receives every record the logger gets
	Secrets   []string // values never written, besides the session's
}

// FromConfig maps the typed configuration and the resolved paths to Options.
func FromConfig(cfg config.Log, paths config.ResolvedPaths, stderr io.Writer) Options {
	return Options{
		Level:     cfg.Level,
		File:      cfg.File,
		Console:   cfg.Console,
		Dir:       paths.Logs,
		MaxSizeMB: cfg.MaxSizeMB,
		MaxFiles:  cfg.MaxFiles,
		Stderr:    stderr,
	}
}

// ErrLevel is returned for a level other than debug, info, warn or error.
var ErrLevel = errors.New("log: unknown level")

// ParseLevel maps the config level names to slog levels.
func ParseLevel(name string) (slog.Level, error) {
	switch name {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("%w: %q", ErrLevel, name)
}

// New returns the logger and a Closer that flushes and closes the file. The logger is never nil
// and always safe to use. The error is non-nil when the level is unknown (info is used) or the
// log file could not be opened (the other destinations keep working); callers show it once as a
// warning and go on.
func New(opts Options) (*slog.Logger, io.Closer, error) {
	level, levelErr := ParseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{Level: level}

	var handlers []slog.Handler
	var closer io.Closer = noopCloser{}
	var fileErr error
	// Every destination is redacted (TM-16); the session's handler redacts for itself.
	redact := newRedactor(opts.Secrets...)
	if opts.Session != nil {
		redact = opts.Session.redact
		redact.add(opts.Secrets...)
		handlers = append(handlers, opts.Session.Handler())
	}
	sink := func(w io.Writer) slog.Handler {
		return redactHandler{next: slog.NewTextHandler(w, handlerOpts), r: redact}
	}
	if opts.File {
		w, err := newRotator(filepath.Join(opts.Dir, FileName), int64(opts.MaxSizeMB)<<20, opts.MaxFiles)
		if err != nil {
			fileErr = fmt.Errorf("log file: %w", err)
		} else {
			handlers = append(handlers, sink(w))
			closer = w
		}
	}
	if opts.Console && opts.Stderr != nil {
		handlers = append(handlers, sink(opts.Stderr))
	}

	var logger *slog.Logger
	switch len(handlers) {
	case 0:
		logger = slog.New(slog.DiscardHandler)
	case 1:
		logger = slog.New(handlers[0])
	default:
		logger = slog.New(slog.NewMultiHandler(handlers...))
	}
	if opts.Session != nil {
		opts.Session.setProcessLogger(logger)
		closer = processCloser{closer, opts.Session}
	}
	return logger, closer, errors.Join(levelErr, fileErr)
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

// processCloser closes mothdex.log; the session lives on and is the process logger again.
type processCloser struct {
	io.Closer
	s *Session
}

func (c processCloser) Close() error {
	c.s.setProcessLogger(nil)
	return c.Closer.Close()
}
