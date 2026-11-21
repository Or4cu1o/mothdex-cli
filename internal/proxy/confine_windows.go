//go:build windows

package proxy

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// finalPath asks Windows for the final path of an opened directory handle
// (GetFinalPathNameByHandle). Unlike EvalSymlinks in Go 1.23 and later it resolves junctions
// and mount points too (CR-13, F7).
func finalPath(dir string) (string, error) {
	f, err := os.Open(dir) //nolint:gosec // dir is the root or destination that Resolve already vetted; opens directories with backup semantics
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0) //nolint:gosec // len is the constant MAX_LONG_PATH
	if err != nil {
		return "", err
	}
	if int(n) > len(buf) {
		return "", windows.ERROR_INSUFFICIENT_BUFFER
	}
	p := windows.UTF16ToString(buf[:n])
	switch {
	case strings.HasPrefix(p, `\\?\UNC\`):
		p = `\\` + p[len(`\\?\UNC\`):]
	case strings.HasPrefix(p, `\\?\`):
		p = p[len(`\\?\`):]
	}
	return p, nil
}
