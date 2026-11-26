//go:build !windows

package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	if !ok || int64(sys.Uid) != int64(os.Geteuid()) {
		return "not_owner", fmt.Errorf("install directory owner differs")
	}
	for parent := filepath.Dir(dir); ; parent = filepath.Dir(parent) {
		p, err := os.Stat(parent)
		if err != nil {
			return "untrusted_dir", err
		}
		if p.Mode().Perm()&0022 != 0 && p.Mode()&os.ModeSticky == 0 {
			return "untrusted_dir", fmt.Errorf("writable install ancestor")
		}
		if parent == filepath.Dir(parent) {
			break
		}
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
func backup(path string) error {
	if err := os.Link(path, path+".old"); err == nil {
		return nil
	}
	source, err := os.Open(path) //nolint:gosec // fixed installed binary checked by regular() before backup.
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	st, err := source.Stat()
	if err != nil {
		return err
	}
	dest, err := os.OpenFile(path+".old", os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm()) //nolint:gosec // fixed backup name, exclusive creation.
	if err != nil {
		return err
	}
	_, err = io.Copy(dest, source)
	if err == nil {
		err = dest.Sync()
	}
	ce := dest.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		_ = os.Remove(path + ".old")
	}
	return err
}
func replaceFile(staged, path string) error { return os.Rename(staged, path) }
func restore(path string) error             { return os.Rename(path+".old", path) }
