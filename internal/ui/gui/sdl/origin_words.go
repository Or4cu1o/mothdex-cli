//go:build gui

package sdl

// Where the SDL2 that was loaded came from, as one word and never a path (docs/contracts/cli/ports.md section 10, PRT-G9).
// The probe runs with a scrubbed environment while a Port inherits whatever the frontend set (EmuELEC puts /emuelec/lib
// first in LD_LIBRARY_PATH), so the version alone does not say which library a report is about.
const (
	OriginSystem      = "system"          // a standard system library directory
	OriginLibraryPath = "ld_library_path" // a directory named in LD_LIBRARY_PATH (also when it is a system directory too)
	OriginOther       = "other"           // anywhere else, for example a directory the ld.so cache points to
	OriginUnknown     = "unknown"         // the library was loaded but the loader's answer could not be read
)
