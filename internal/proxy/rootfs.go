package proxy

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// RootFS is the real FS: every operation goes through an *os.Root, so a name can never leave
// the root through "..", an absolute name or a symlink (the Go runtime refuses it). It
// implements FS; the real Finalizer needs its directory handles.
type RootFS struct {
	dir  string
	root *os.Root
}

var _ FS = (*RootFS)(nil)

// OpenRootFS opens dir as the root of a confined file system. dir must exist.
func OpenRootFS(dir string) (*RootFS, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("proxy: resolve %q: %w", dir, err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("proxy: open root: %w", err)
	}
	return &RootFS{dir: abs, root: root}, nil
}

// Close releases the root.
func (r *RootFS) Close() error { return r.root.Close() }

// Dir is the absolute directory of the root.
func (r *RootFS) Dir() string { return r.dir }

// OpenExcl implements FS: a new file, 0600, never following a link at the final name.
func (r *RootFS) OpenExcl(name string) (File, error) {
	f, err := r.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL|oNoFollow, 0o600)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Open implements FS.
func (r *RootFS) Open(name string) (io.ReadCloser, error) {
	f, err := r.root.Open(name)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Lstat implements FS.
func (r *RootFS) Lstat(name string) (fs.FileInfo, error) { return r.root.Lstat(name) }

// MkdirAll implements FS.
func (r *RootFS) MkdirAll(name string, perm fs.FileMode) error { return r.root.MkdirAll(name, perm) }

// Remove implements FS.
func (r *RootFS) Remove(name string) error { return r.root.Remove(name) }

// ReadDir implements FS.
func (r *RootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(r.root.FS(), path.Clean(filepath.ToSlash(name)))
}

// Link implements FS. A file system without hard links reports errors.ErrUnsupported.
func (r *RootFS) Link(from, to string) error {
	err := r.root.Link(from, to)
	if err != nil && linkUnsupported(err) {
		return fmt.Errorf("%w: %w", errors.ErrUnsupported, err)
	}
	return err
}

// Free implements FS.
func (r *RootFS) Free(name string) (int64, uint64, error) {
	return freeSpace(filepath.Join(r.dir, filepath.FromSlash(name)))
}

// SyncDir implements FS.
func (r *RootFS) SyncDir(name string) error {
	d, err := r.root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return syncDirHandle(d)
}
