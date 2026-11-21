//go:build windows

package cache

import (
	"os"
	"syscall"
)

// openShared opens path for reading with FILE_SHARE_DELETE besides read and write sharing. Go's
// os.Open leaves FILE_SHARE_DELETE out, so while any reader holds a cache file open a Put cannot
// rename a new version over it ("Access is denied"), and a busy reader starves the writer past
// any retry budget. With the flag the replace goes through at once; the old file disappears when
// its last reader closes it.
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	const share = syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE | syscall.FILE_SHARE_DELETE
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
