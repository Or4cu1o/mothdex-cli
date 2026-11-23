//go:build linux

package proxy

import (
	"errors"
	"io/fs"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Filesystem magic numbers of memory-backed file systems (statfs f_type).
const (
	tmpfsMagic = 0x01021994
	ramfsMagic = 0x858458f6
)

// blockSize is the unit of f_bavail: f_frsize, falling back to f_bsize when it is 0 (FUSE and
// NFS report block counts in fragment units).
func blockSize(st *unix.Statfs_t) int64 {
	if st.Frsize > 0 {
		return int64(st.Frsize)
	}
	return int64(st.Bsize)
}

// IsMemoryBacked reports whether dir is on tmpfs or ramfs, where a download eats RAM (TH-14).
// A not-yet-created folder inherits the nearest existing ancestor. Other errors
// leave the filesystem unknown (false); the caller warns.
func IsMemoryBacked(dir string) bool {
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	for {
		var st unix.Statfs_t
		err := unix.Statfs(dir, &st)
		if err == nil {
			return int64(st.Type) == tmpfsMagic || int64(st.Type) == ramfsMagic
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
