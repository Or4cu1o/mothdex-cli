//go:build linux

package proxy

import "golang.org/x/sys/unix"

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
// The caller warns; it is false when the check is not possible.
func IsMemoryBacked(dir string) bool {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) != nil {
		return false
	}
	return int64(st.Type) == tmpfsMagic || int64(st.Type) == ramfsMagic
}
