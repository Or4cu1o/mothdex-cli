package fakefs

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
)

// Errors a test can inject with FS.Fail or FS.LimitWrites.
var (
	// ErrNoSpace is ENOSPC (disk full while writing).
	ErrNoSpace error = syscall.ENOSPC
	// ErrDiskFullWindows is ERROR_DISK_FULL (112), what a full Windows volume
	// reports; ErrHandleDiskFullWindows is ERROR_HANDLE_DISK_FULL (39). The
	// disk_full classification must hold for them as well as for ENOSPC.
	ErrDiskFullWindows       error = syscall.Errno(112)
	ErrHandleDiskFullWindows error = syscall.Errno(39)
	// ErrCrossDevice is EXDEV (rename across file systems).
	ErrCrossDevice error = syscall.EXDEV
	// ErrNoReplaceUnsupported is what a file system without a no-replace rename
	// returns; errors.Is(err, errors.ErrUnsupported) holds, as the port requires.
	ErrNoReplaceUnsupported error = fmt.Errorf("fakefs: no-replace rename unsupported: %w", errors.ErrUnsupported)
	// ErrHardLinkUnsupported is the same for hard links (some FUSE and exFAT drivers).
	ErrHardLinkUnsupported error = fmt.Errorf("fakefs: hard links unsupported: %w", errors.ErrUnsupported)
)

// Finalizer is the test double of proxy.Finalizer. It works only on an *FS from
// this package. Without a replace flag it refuses an existing target with
// fs.ErrExist, like renameat2(RENAME_NOREPLACE); with replace it overwrites.
// Faults on OpRename let a test simulate EXDEV and an unsupported file system
// independently of Link faults, so each step of the fallback chain is driven.
type Finalizer struct{}

// Rename implements proxy.Finalizer.
func (Finalizer) Rename(fsys proxy.FS, from, to string, replace bool) error {
	f, ok := fsys.(*FS)
	if !ok {
		return fmt.Errorf("fakefs: Finalizer needs a *fakefs.FS, got %T", fsys)
	}
	if err := f.enter(OpRename, from, to); err != nil {
		return err
	}
	f.renameMu.Lock() // check and rename are one atomic step, like RENAME_NOREPLACE
	defer f.renameMu.Unlock()
	if !replace {
		_, err := f.root.Lstat(to)
		switch {
		case err == nil:
			return &fs.PathError{Op: "rename", Path: to, Err: fs.ErrExist}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}
	return f.root.Rename(from, to)
}
