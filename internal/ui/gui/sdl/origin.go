//go:build gui

package sdl

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Where the SDL2 that was loaded came from, as one word and never a path (docs/contracts/cli/ports.md section 10, PRT-G9).
// The probe runs with a scrubbed environment while a Port inherits whatever the frontend set (EmuELEC puts /emuelec/lib
// first in LD_LIBRARY_PATH), so the version alone does not say which library a report is about.
const (
	OriginSystem      = "system"          // a standard system library directory
	OriginLibraryPath = "ld_library_path" // a directory named in LD_LIBRARY_PATH (also when it is a system directory too)
	OriginOther       = "other"           // anywhere else, for example a directory the ld.so cache points to
	OriginUnknown     = "unknown"         // the library was loaded but the loader's answer could not be read
)

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

// originOf is the word for the SDL2 this process has mapped, or "" where the question does not apply (it is asked on Linux,
// where the system loader searches; on Windows the library is the one next to the executable, gui.md section 2).
func originOf() string {
	if !originApplies {
		return ""
	}
	path := mappedSDLPath()
	if path == "" {
		return OriginUnknown
	}
	return classifyOrigin(path, os.Getenv("LD_LIBRARY_PATH"))
}
