//go:build linux

package update

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestManagedAndOptOutGateDoNotWriteToInstall(t *testing.T) {
	u, s := fixture(t)
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(fd) }()
	if _, err = unix.InotifyAddWatch(fd, s.Dir, unix.IN_CREATE|unix.IN_DELETE); err != nil {
		t.Fatal(err)
	}
	if _, err = u.Managed(); err != nil {
		t.Fatal(err)
	}
	if reason := u.SkipReason(true, func(string) string { return "" }); reason != "" {
		t.Fatal(reason)
	}
	events := make([]byte, 4096)
	n, err := unix.Read(fd, events)
	if n > 0 || !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("managed check wrote in installation: bytes=%d err=%v", n, err)
	}
	// The explicit Apply gate still proves writability.
	if err = writeProbe(s.Dir); err != nil {
		t.Fatal(err)
	}
	n, err = unix.Read(fd, events)
	if n <= 0 || err != nil {
		t.Fatalf("write probe did not run: bytes=%d err=%v", n, err)
	}
}
