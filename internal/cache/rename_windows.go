//go:build windows

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileRenameInfo is FILE_RENAME_INFO from the Windows SDK (the Flags member shares its place with
// ReplaceIfExists); FileName holds FileNameLength bytes and is not NUL terminated.
type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// renameOver renames src over dst. os.Rename uses MoveFileEx(MOVEFILE_REPLACE_EXISTING), which fails
// with "Access is denied" while any handle on dst is open, even one opened with FILE_SHARE_DELETE.
// FileRenameInfoEx with FILE_RENAME_POSIX_SEMANTICS (Windows 10 1809, NTFS) replaces the file at
// once and lets the old one live until its last reader closes it, like rename(2). Where the system
// or the file system does not support it, it falls back to os.Rename.
func renameOver(src, dst string) error {
	err := posixRename(src, dst)
	if err == nil {
		return nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == windows.ERROR_INVALID_PARAMETER || errno == windows.ERROR_NOT_SUPPORTED ||
		errno == windows.ERROR_INVALID_FUNCTION || errno == windows.ERROR_CALL_NOT_IMPLEMENTED) {
		return os.Rename(src, dst)
	}
	return err
}

func posixRename(src, dst string) error {
	abs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	if strings.HasPrefix(abs, `\\`) { // UNC
		abs = `\\?\UNC\` + abs[2:]
	} else {
		abs = `\\?\` + abs
	}
	name, err := windows.UTF16FromString(abs)
	if err != nil {
		return err
	}
	name = name[:len(name)-1] // drop the NUL; the length is explicit
	sp, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	// DELETE is the access a rename needs; the share flags let readers and writers coexist.
	h, err := windows.CreateFile(sp, windows.DELETE|windows.SYNCHRONIZE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	defer func() { _ = windows.CloseHandle(h) }()

	nameBytes := len(name) * 2
	size := int(unsafe.Offsetof(fileRenameInfo{}.FileName)) + nameBytes + 2
	buf := make([]uint64, (size+7)/8)                  // 8-byte aligned backing store
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0])) //nolint:gosec // FILE_RENAME_INFO is variable length, built in an aligned buffer
	info.Flags = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
	info.FileNameLength = uint32(nameBytes)               //nolint:gosec // a path is far below 4 GiB
	dstName := unsafe.Slice(&info.FileName[0], len(name)) //nolint:gosec // the buffer was sized for len(name) UTF-16 units
	copy(dstName, name)
	if err := windows.SetFileInformationByHandle(h, windows.FileRenameInfoEx, (*byte)(unsafe.Pointer(info)), uint32(size)); err != nil { //nolint:gosec // size is small
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
