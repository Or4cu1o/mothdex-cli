//go:build linux

package proxy

import (
	"runtime"

	"golang.org/x/sys/unix"
)

// renameNoReplace is renameat2(RENAME_NOREPLACE) between two directory handles of the root.
func renameNoReplace(r *RootFS, from, to string) error {
	fd, fb, err := parent(r, from)
	if err != nil {
		return err
	}
	defer func() { _ = fd.Close() }()
	td, tb, err := parent(r, to)
	if err != nil {
		return err
	}
	defer func() { _ = td.Close() }()
	err = unix.Renameat2(int(fd.Fd()), fb, int(td.Fd()), tb, unix.RENAME_NOREPLACE)
	runtime.KeepAlive(fd)
	runtime.KeepAlive(td)
	if err != nil {
		return unsupported("renameat2", from, to, err)
	}
	return nil
}
