//go:build !windows

package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// openFile opens a log file without following a link and refuses anything that is not a regular
// file (O_NONBLOCK keeps a planted FIFO from hanging the program).
func openFile(path string, flags int) (*os.File, error) {
	f, err := os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, fileMode) // #nosec G304 -- path is paths.logs plus a fixed or validated name
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	return f, nil
}

func checkOwner(dir string, info fs.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%w: %s belongs to another user", errUnsafeDir, dir)
	}
	if info.Mode().Perm()&0o022 != 0 && info.Mode()&fs.ModeSticky == 0 {
		return fmt.Errorf("%w: others can write to %s", errUnsafeDir, dir)
	}
	return nil
}

// tryLock takes the exclusive OS lock of f without waiting; false means someone else holds it.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) //nolint:gosec // a file descriptor fits an int
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}

func unlock(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:gosec // a file descriptor fits an int
}

// shortHome is the Windows 8.3 spelling of the home folder; there is none here.
func shortHome(string) string { return "" }
