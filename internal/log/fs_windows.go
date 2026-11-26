//go:build windows

package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is where the lock byte sits: far past the token, because a Windows lock also blocks
// reads of the locked range by other handles.
const lockOffset = 1 << 30

// openFile opens a log file sharing read, write and delete, so a tester can copy it while the
// program runs and another process can rotate or delete it, and refuses a reparse point.
func openFile(path string, flags int) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var access uint32 = windows.GENERIC_READ
	switch {
	case flags&os.O_APPEND != 0:
		access = windows.FILE_APPEND_DATA | windows.FILE_READ_ATTRIBUTES | windows.SYNCHRONIZE
	case flags&(os.O_WRONLY|os.O_RDWR) != 0:
		access |= windows.GENERIC_WRITE
	}
	var disposition uint32 = windows.OPEN_EXISTING
	switch {
	case flags&os.O_CREATE != 0 && flags&os.O_EXCL != 0:
		disposition = windows.CREATE_NEW
	case flags&os.O_CREATE != 0:
		disposition = windows.OPEN_ALWAYS
	}
	h, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, disposition, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil ||
		info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	return os.NewFile(uintptr(h), path), nil
}

// checkOwner: the default folders are inside the user profile, whose access list protects them.
func checkOwner(string, fs.FileInfo) error { return nil }

// tryLock takes the exclusive OS lock of f without waiting; false means someone else holds it.
func tryLock(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &windows.Overlapped{Offset: lockOffset})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{Offset: lockOffset})
}

// shortHome is the 8.3 spelling of the home folder, when the volume has one.
func shortHome(home string) string {
	long, err := windows.UTF16PtrFromString(home)
	if err != nil {
		return ""
	}
	buf := make([]uint16, windows.MAX_PATH)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf))) //nolint:gosec // a fixed small length
	if err != nil || n == 0 || int(n) > len(buf) {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
