package proxy

import (
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

func TestDiskFullIsOSGuarded(t *testing.T) {
	wrap := func(e syscall.Errno) error { return &fs.PathError{Op: "write", Path: "x", Err: fmt.Errorf("w: %w", e)} }
	tests := []struct {
		goos string
		err  error
		want bool
	}{
		{"linux", wrap(syscall.ENOSPC), true},
		{"linux", wrap(syscall.EDQUOT), true},
		{"linux", wrap(112), false}, // EHOSTDOWN
		{"linux", wrap(39), false},  // ENOTEMPTY
		{"darwin", wrap(syscall.ENOSPC), true},
		{"darwin", wrap(112), false},
		{"windows", wrap(112), true},
		{"windows", wrap(39), true},
		{"windows", wrap(1295), true},
		{"linux", wrap(1295), false},
		{"windows", wrap(syscall.ENOENT), false},
		{"linux", wrap(syscall.EACCES), false},
		{"linux", fs.ErrPermission, false},
		{"linux", nil, false},
	}
	for _, tt := range tests {
		if got := diskFullOn(tt.goos, tt.err); got != tt.want {
			t.Errorf("%s %v: got %v, want %v", tt.goos, tt.err, got, tt.want)
		}
	}
}
