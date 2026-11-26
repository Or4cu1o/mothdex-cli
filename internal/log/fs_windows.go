//go:build windows

package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"
)

// lockOffset is where the lock byte sits: far past the token, because a Windows lock also blocks
// reads of the locked range by other handles.
const lockOffset = 1 << 30

// openFile opens a log file sharing read, write and delete, so a tester can copy it while the
// program runs and another process can rotate or delete it. It refuses a reparse point and, for a
// file this call did not create, one with a second name or another user's.
func openFile(path string, flags int) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	var access uint32 = windows.GENERIC_READ
	switch {
	case flags&os.O_APPEND != 0:
		access = windows.FILE_APPEND_DATA | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
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
	if disposition != windows.CREATE_NEW && info.NumberOfLinks > 1 {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("%w: %s has another name", errUnsafeDir, path)
	}
	if disposition != windows.CREATE_NEW && !permissionless(filepath.Dir(path)) {
		sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
		if err := ownedByUser(path, sd, err); err != nil {
			_ = windows.CloseHandle(h)
			return nil, err
		}
	}
	return os.NewFile(uintptr(h), path), nil
}

// permissionless says whether dir is on a volume that keeps no access lists, so no owners
// either (FAT, exFAT). Tests replace it.
var permissionless = func(dir string) bool {
	path, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return false
	}
	root := make([]uint16, windows.MAX_PATH+1)
	if windows.GetVolumePathName(path, &root[0], uint32(len(root))) != nil { //nolint:gosec // a fixed small length
		return false
	}
	var flags uint32
	if windows.GetVolumeInformation(&root[0], nil, 0, nil, nil, &flags, nil, 0) != nil {
		return false
	}
	return flags&windows.FILE_PERSISTENT_ACLS == 0
}

// checkOwner refuses a folder another user owns: in a portable install under a shared folder,
// anyone could have created it to receive the logs (TM-20).
func checkOwner(dir string, _ fs.FileInfo) error {
	if permissionless(dir) {
		return nil
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	return ownedByUser(dir, sd, err)
}

// ownedByUser is the outcome of reading the owner of path: an owner that cannot be read is not
// trusted.
func ownedByUser(path string, sd *windows.SECURITY_DESCRIPTOR, readErr error) error {
	if readErr != nil {
		return fmt.Errorf("%w: cannot read the owner of %s: %w", errUnsafeDir, path, readErr)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !isOwn(owner) && !isSystem(owner) {
		return fmt.Errorf("%w: %s belongs to another user", errUnsafeDir, path)
	}
	return nil
}

// systemOwners are the accounts more privileged than the user, so none of them can be the other
// unprivileged user this check is against: Administrators, SYSTEM and TrustedInstaller, as in
// Test-FolderTrusted of install.ps1. An elevated run leaves its folders to Administrators.
var systemOwners = []string{"S-1-5-32-544", "S-1-5-18", "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"}

func isSystem(sid *windows.SID) bool { return slices.Contains(systemOwners, sid.String()) }

// isOwn reports whether sid is the user of this process, or the owner Windows gives to what the
// process creates (the Administrators group when it runs elevated). Tests replace it.
var isOwn = func(sid *windows.SID) bool {
	token := windows.GetCurrentProcessToken()
	if user, err := token.GetTokenUser(); err == nil && user.User.Sid.Equals(sid) {
		return true
	}
	var size uint32
	_ = windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &size) // asks for the size
	if size < uint32(unsafe.Sizeof(uintptr(0))) {
		return false
	}
	buf := make([]byte, size)
	if windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], size, &size) != nil {
		return false
	}
	// TOKEN_OWNER is one pointer to a SID stored in the same buffer.
	owner := *(**windows.SID)(unsafe.Pointer(&buf[0])) // #nosec G103 -- the layout the Windows API documents
	return owner.Equals(sid)
}

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
