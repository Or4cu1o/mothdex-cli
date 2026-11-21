//go:build windows

package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// renameNoReplace is MoveFileExW without MOVEFILE_REPLACE_EXISTING and with
// MOVEFILE_WRITE_THROUGH. MoveFileEx takes paths, not handles: the names are joined to the
// root directory that was confined and checked (CR-13) before the job started; the residual
// race with a local attacker who can swap directories is documented in the threat model.
func renameNoReplace(r *RootFS, from, to string) error {
	src, err := windows.UTF16PtrFromString(filepath.Join(r.dir, filepath.FromSlash(from)))
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(filepath.Join(r.dir, filepath.FromSlash(to)))
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(src, dst, windows.MOVEFILE_WRITE_THROUGH)
	if err == nil {
		return nil
	}
	var errno windows.Errno
	switch {
	case errors.As(err, &errno) && errno == windows.ERROR_NOT_SAME_DEVICE:
		err = syscall.EXDEV
	case errors.As(err, &errno) && (errno == windows.ERROR_ALREADY_EXISTS || errno == windows.ERROR_FILE_EXISTS):
		err = os.ErrExist
	}
	return &os.LinkError{Op: "movefileex", Old: from, New: to, Err: err}
}
