//go:build !windows

package proxy_test

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ageLink sets the mtime of a symlink itself (os.Chtimes would follow it), so the no-follow
// branch of the cleanup is exercised with an old link.
func ageLink(t *testing.T, path string, when time.Time) {
	t.Helper()
	ts := []unix.Timespec{unix.NsecToTimespec(when.UnixNano()), unix.NsecToTimespec(when.UnixNano())}
	if err := unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		t.Fatal(err)
	}
}
