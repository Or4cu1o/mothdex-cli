//go:build gui && linux

package sdl

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const originApplies = true

// maxMapsLines bounds the read: a process maps a few hundred files, never this many.
const maxMapsLines = 20000

// mappedSDLPath is the file the loader mapped for SDL2, read from the process's own memory map. The library is opened by
// soname (docs/contracts/cli/gui.md section 2), so the map shows which directory the loader chose. Empty when the map
// cannot be read or holds no SDL2.
func mappedSDLPath() string {
	f, err := os.Open("/proc/self/maps")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }() // read-only: a failed close loses nothing
	return sdlPathFromMaps(f)
}

// sdlPathFromMaps finds the first mapping of a libSDL2-2.0.so* file in the text of /proc/<pid>/maps: address, permissions,
// offset, device and inode, then the path (which may hold spaces).
func sdlPathFromMaps(f *os.File) string {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for n := 0; sc.Scan() && n < maxMapsLines; n++ {
		if path := sdlPathInMapsLine(sc.Text()); path != "" {
			return path
		}
	}
	return ""
}

// sdlPathInMapsLine is the path of the line when it maps a libSDL2-2.0.so* file.
func sdlPathInMapsLine(line string) string {
	fields := strings.SplitN(line, " ", 6)
	if len(fields) < 6 {
		return ""
	}
	path := strings.TrimSpace(fields[5])
	path = strings.TrimSuffix(path, " (deleted)")
	if !strings.HasPrefix(filepath.Base(path), "libSDL2-2.0.so") {
		return ""
	}
	return path
}
