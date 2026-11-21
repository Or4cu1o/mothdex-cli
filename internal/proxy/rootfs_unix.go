//go:build !windows

package proxy

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// oNoFollow makes a final symlink an error instead of being followed.
const oNoFollow = syscall.O_NOFOLLOW

// freeSpace reports the bytes available to an unprivileged writer and the device id of name.
func freeSpace(name string) (int64, uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(name, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs: %w", err)
	}
	info, err := os.Stat(name)
	if err != nil {
		return 0, 0, err
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, errors.New("proxy: no device id on this platform")
	}
	free := int64(st.Bavail) * int64(st.Bsize) //nolint:gosec // block counts fit int64
	return free, uint64(sys.Dev), nil          //nolint:gosec,unconvert // Dev is int32 on some platforms
}

// linkUnsupported recognizes the errors of file systems without hard links (some FUSE and
// exFAT drivers answer EPERM).
func linkUnsupported(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.ENOSYS)
}

func syncDirHandle(d *os.File) error { return d.Sync() }
