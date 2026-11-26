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
	tok := windows.GetCurrentProcessToken()
	user, err := tok.GetTokenUser()
	if err != nil || !owner.Equals(user.User.Sid) {
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
		if (sid.IsWellKnown(windows.WinWorldSid) || sid.IsWellKnown(windows.WinBuiltinUsersSid) || sid.IsWellKnown(windows.WinAuthenticatedUserSid)) && ace.Mask&0x500d0156 != 0 {
			return "untrusted_dir", fmt.Errorf("broad write ACL")
		}
	}
	return "", nil
}
func acquireLock(dir string) (func(), error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(dir, ".mothdex-update.lock"))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_ALWAYS, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
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
		if err = os.Rename(path, path+".failed"); err != nil {
			return err
		}
	}
	return os.Rename(path+".old", path)
}
