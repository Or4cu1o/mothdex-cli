//go:build !windows

package update

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func isReparse(_ os.FileInfo) bool { return false }
func trustedDir(dir string) (string, error) {
	st, err := os.Lstat(dir)
	if err != nil {
		return "untrusted_dir", err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
		return "untrusted_dir", fmt.Errorf("unsafe install directory")
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid != uint32(os.Geteuid()) {
		return "not_owner", fmt.Errorf("install directory owner differs")
	}
	return "", nil
}
func acquireLock(dir string) (func(), error) {
	path := dir + "/.mothdex-update.lock"
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = f.Close() }, nil
}
func backup(path string) error              { return os.Link(path, path+".old") }
func replaceFile(staged, path string) error { return os.Rename(staged, path) }
func restore(path string) error             { return os.Rename(path+".old", path) }
