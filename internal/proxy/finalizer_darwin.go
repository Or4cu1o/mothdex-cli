//go:build darwin

package proxy

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// renameNoReplace is renameatx_np(RENAME_EXCL) between two directory handles of the root.
func renameNoReplace(r *RootFS, from, to string) error {
	fd, fb, err := parent(r, from)
	if err != nil {
		return err
	}
	defer fd.Close()
	td, tb, err := parent(r, to)
	if err != nil {
		return err
	}
	defer td.Close()
	err = unix.RenameatxNp(int(fd.Fd()), fb, int(td.Fd()), tb, unix.RENAME_EXCL)
	runtime.KeepAlive(fd)
	runtime.KeepAlive(td)
	if err != nil {
		return unsupported("renameatx_np", from, to, err)
	}
	return nil
}
