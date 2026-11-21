package proxy

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"syscall"
)

// osFinalizer is the real Finalizer: a no-replace rename per OS (T-16 F8, TH-12). os.Rename and
// os.Root.Rename both replace an existing target, so the plain call is never used unless the
// caller asked for replace after checking the target.
type osFinalizer struct{}

// NewFinalizer returns the Finalizer of the running OS. It works on a *RootFS only.
func NewFinalizer() Finalizer { return osFinalizer{} }

// Rename implements Finalizer.
func (osFinalizer) Rename(fsys FS, from, to string, replace bool) error {
	r, ok := fsys.(*RootFS)
	if !ok {
		return fmt.Errorf("proxy: the OS finalizer needs a *RootFS, got %T", fsys)
	}
	if replace {
		return r.root.Rename(from, to)
	}
	return renameNoReplace(r, from, to)
}

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
