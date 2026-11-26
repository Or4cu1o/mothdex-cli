package update

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func isReparse(st os.FileInfo) bool {
	a, ok := st.Sys().(*syscall.Win32FileAttributeData)
	return ok && a.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
func trustedDir(dir string) (string, error) {
	st, err := os.Lstat(dir)
	if err != nil {
		return "untrusted_dir", err
	}
	if !st.IsDir() || isReparse(st) || st.Mode()&os.ModeSymlink != 0 {
		return "untrusted_dir", fmt.Errorf("unsafe directory")
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "untrusted_dir", err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return "not_owner", err
	}
	if owner == nil || !trustedWindowsOwner(owner) {
		return "not_owner", fmt.Errorf("install owner differs")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return "untrusted_dir", fmt.Errorf("missing ACL")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err = windows.GetAce(acl, i, &ace); err != nil {
			return "untrusted_dir", err
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return "untrusted_dir", fmt.Errorf("unsupported ACL type")
		}

		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Mask&0x500d0156 != 0 && !trustedWindowsOwner(sid) && sid.String() != "S-1-3-0" {
			return "untrusted_dir", fmt.Errorf("broad write ACL")
		}
	}
	return "", nil
}
func acquireLock(dir string) (func(), error)         { return lockFile(dir, true) }
func acquireExistingLock(dir string) (func(), error) { return lockFile(dir, false) }
func lockFile(dir string, create bool) (func(), error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(dir, ".mothdex-update.lock"))
	if err != nil {
		return nil, err
	}
	disposition := uint32(windows.OPEN_EXISTING)
	if create {
		disposition = windows.OPEN_ALWAYS
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, disposition, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("unsafe lock")
	}
	var ov windows.Overlapped
	if err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ov); err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return func() { _ = windows.UnlockFileEx(h, 0, 1, 0, &ov); _ = windows.CloseHandle(h) }, nil
}
func backup(path string) error              { return os.Rename(path, path+".old") }
func replaceFile(staged, path string) error { return os.Rename(staged, path) }
func restore(path string) error {
	if _, err := os.Lstat(path); err == nil {
		if _, e := regular(path + ".failed"); e == nil {
			if e = retire(path+".failed", path); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
		if err = os.Rename(path, path+".failed"); err != nil {
			return err
		}
	}
	return os.Rename(path+".old", path)
}

// isOwnWindows is injectable for Windows SID policy tests, as in the logging gate.
var isOwnWindows = func(sid *windows.SID) bool {
	token := windows.GetCurrentProcessToken()
	if user, err := token.GetTokenUser(); err == nil && user.User.Sid.Equals(sid) {
		return true
	}
	var size uint32
	_ = windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &size)
	if size < uint32(unsafe.Sizeof(uintptr(0))) || size > 65536 {
		return false
	}
	b := make([]byte, size)
	if windows.GetTokenInformation(token, windows.TokenOwner, &b[0], size, &size) != nil {
		return false
	}
	owner := *(**windows.SID)(unsafe.Pointer(&b[0])) // #nosec G103 -- TOKEN_OWNER is a SID pointer inside this API-owned buffer.
	return owner != nil && owner.Equals(sid)
}

func trustedWindowsOwner(sid *windows.SID) bool {
	if sid == nil {
		return false
	}
	if isOwnWindows(sid) {
		return true
	}
	switch sid.String() {
	case "S-1-5-18", "S-1-5-32-544", "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464":
		return true
	}
	return false
}
func aliasFileID(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var info windows.ByHandleFileInformation
	if err = windows.GetFileInformationByHandle(h, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return "", fmt.Errorf("alias is not a regular file")
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
