//go:build gui && linux

package sdl

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

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

// systemDirs are the library directories of a normal system; the multiarch ones are matched by pattern.
var systemDirs = []string{"/lib", "/lib32", "/lib64", "/usr/lib", "/usr/lib32", "/usr/lib64", "/usr/local/lib", "/usr/local/lib64"}

// multiarchDir is /lib/<triplet> or /usr/lib/<triplet>, such as x86_64-linux-gnu or arm-linux-gnueabihf.
var multiarchDir = regexp.MustCompile(`^/(usr/)?lib/[a-z0-9_]+-linux-[a-z0-9]+$`)

// classifyOrigin names the directory of path. ldLibraryPath is the value of LD_LIBRARY_PATH: an empty entry means the
// current directory and matches nothing. Both sides are resolved through symbolic links when they can be, so a path
// list that goes through a link still matches. Only the word leaves this function.
func classifyOrigin(path, ldLibraryPath string) string {
	if path == "" || !filepath.IsAbs(path) {
		return OriginUnknown
	}
	dir := resolveDir(filepath.Dir(path))
	for _, entry := range strings.Split(ldLibraryPath, ":") {
		if entry != "" && resolveDir(entry) == dir {
			return OriginLibraryPath
		}
	}
	for _, sys := range systemDirs {
		if dir == sys {
			return OriginSystem
		}
	}
	if multiarchDir.MatchString(dir) {
		return OriginSystem
	}
	return OriginOther
}

// resolveDir cleans a directory and follows its links; a directory that cannot be resolved is only cleaned.
func resolveDir(dir string) string {
	dir = filepath.Clean(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}

// originOf is the word for the SDL2 this process has mapped. It is asked on Linux, where the system loader searches; on Windows
// the library is the one next to the executable (gui.md section 2) and the answer is "" (origin_other.go).
func originOf() string {
	path := mappedSDLPath()
	if path == "" {
		return OriginUnknown
	}
	return classifyOrigin(path, os.Getenv("LD_LIBRARY_PATH"))
}
