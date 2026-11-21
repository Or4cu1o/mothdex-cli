//go:build !windows

package proxy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// unsupported wraps the errors that mean "this file system or kernel has no no-replace
// rename" so that the caller falls back to Link and then to a placeholder.
func unsupported(op, from, to string, err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.ENOTSUP) {
		err = fmt.Errorf("%w: %w", errors.ErrUnsupported, err)
	}
	return &os.LinkError{Op: op, Old: from, New: to, Err: err}
}

// parent opens the directory of name through the root, so the rename is anchored to handles
// that cannot leave it, and returns the base name.
func parent(r *RootFS, name string) (*os.File, string, error) {
	dir := path.Dir(filepath.ToSlash(name))
	f, err := r.openDir(dir)
	if err != nil {
		return nil, "", err
	}
	return f, path.Base(filepath.ToSlash(name)), nil
}

// openDir opens a directory handle confined to the root, for the Finalizer.
func (r *RootFS) openDir(name string) (*os.File, error) { return r.root.Open(name) }
