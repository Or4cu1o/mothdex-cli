package proxy

import (
	"errors"
	"runtime"
	"syscall"
)

// Windows error numbers that mean a full volume. They are only disk errors on Windows: on
// Linux 112 is EHOSTDOWN and 39 is ENOTEMPTY (T-16, Crivo), so the classifier is OS-guarded.
const (
	winErrorDiskFull       syscall.Errno = 112 // ERROR_DISK_FULL
	winErrorHandleDiskFull syscall.Errno = 39  // ERROR_HANDLE_DISK_FULL
)

// isDiskFull reports whether err means the volume is full or the quota is used up on the
// running OS.
func isDiskFull(err error) bool { return diskFullOn(runtime.GOOS, err) }

// diskFullOn is isDiskFull for a named GOOS, so that one test run covers every table.
func diskFullOn(goos string, err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	if goos == "windows" {
		return errno == winErrorDiskFull || errno == winErrorHandleDiskFull
	}
	return errno == syscall.ENOSPC || errno == syscall.EDQUOT
}
