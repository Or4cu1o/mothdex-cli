package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const (
	dirMode  = 0o700 // like the cache: logs may name local paths
	fileMode = 0o600
)

// rotator is an io.WriteCloser that appends to path and, before a write that would take the file
// past maxBytes, shifts path -> path.1 -> path.2 ... keeping at most maxFiles files in total.
// Several processes may share the file: appends are atomic enough for log lines, and a rotation
// race at worst loses or duplicates a few lines, never corrupts the program.
type rotator struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
}

func newRotator(path string, maxBytes int64, maxFiles int) (*rotator, error) {
	if maxFiles < 1 {
		maxFiles = 1
	}
	if maxBytes < 1 {
		return nil, errors.New("maximum size must be positive")
	}
	r := &rotator{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := safeDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotator) open() error {
	f, err := openFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.file, r.size = f, info.Size()
	return nil
}

// Write implements io.Writer. A failed rotation keeps writing to the current file rather than
// dropping lines.
func (r *rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return 0, fs.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxBytes {
		_ = r.rotate()
		if r.file == nil {
			return 0, fs.ErrClosed // the fresh file could not be opened
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate closes the file (Windows cannot rename an open one), shifts the numbered copies up,
// drops the oldest and reopens a fresh file. It always leaves r.file usable.
func (r *rotator) rotate() error {
	closeErr := r.file.Close()
	r.file = nil
	oldest := r.maxFiles - 1 // number of the last copy kept; 0 means keep none
	var shiftErr error
	if oldest == 0 {
		shiftErr = os.Remove(r.path)
	} else {
		shiftErr = r.shift(oldest)
	}
	if err := r.open(); err != nil {
		return errors.Join(closeErr, shiftErr, err)
	}
	return errors.Join(closeErr, shiftErr)
}

func (r *rotator) shift(oldest int) error {
	var errs []error
	if err := os.Remove(r.numbered(oldest)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	for n := oldest - 1; n >= 1; n-- {
		if err := os.Rename(r.numbered(n), r.numbered(n+1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if err := os.Rename(r.path, r.numbered(1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (r *rotator) numbered(n int) string { return fmt.Sprintf("%s.%d", r.path, n) }

// Close implements io.Closer; later writes fail with fs.ErrClosed.
func (r *rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
