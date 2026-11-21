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
	f, err := os.Open(dir) // opens directories with backup semantics
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
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
