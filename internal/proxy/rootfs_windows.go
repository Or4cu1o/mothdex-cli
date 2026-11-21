//go:build windows

package proxy

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// oNoFollow does not exist on Windows; os.Root already refuses to leave the root and O_EXCL
// does not follow a link at the final name.
const oNoFollow = 0

// Windows error numbers of volumes without hard links.
const (
	errInvalidFunction windows.Errno = 1
	errNotSupported    windows.Errno = 50
)

// freeSpace reports the bytes available to the caller and the volume serial number of name.
func freeSpace(name string) (int64, uint64, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, 0, fmt.Errorf("GetDiskFreeSpaceEx: %w", err)
	}
	var vol [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumePathName(p, &vol[0], uint32(len(vol))); err != nil {
		return 0, 0, fmt.Errorf("GetVolumePathName: %w", err)
	}
	var serial uint32
	if err := windows.GetVolumeInformation(&vol[0], nil, 0, &serial, nil, nil, nil, 0); err != nil {
		return 0, 0, fmt.Errorf("GetVolumeInformation: %w", err)
	}
	return int64(avail), uint64(serial), nil //nolint:gosec // free bytes fit int64
}

func linkUnsupported(err error) bool {
	var errno windows.Errno
	return errors.As(err, &errno) && (errno == errInvalidFunction || errno == errNotSupported)
}

// syncDirHandle: a directory cannot be flushed on Windows; MOVEFILE_WRITE_THROUGH covers it.
func syncDirHandle(*os.File) error { return nil }

// checkTempDir and checkPrivateDir: Windows has no mode bits; the per-user temp directory ACL
// is the protection (not verified on a real box).
func checkTempDir(string) error { return nil }

func checkPrivateDir(string) error { return nil }
