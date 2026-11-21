//go:build gui && windows

package sdl

import "golang.org/x/sys/windows"

// openLibrary loads SDL2.dll from the folder of the executable or from System32, and nowhere else: never
// the current directory, which would let a planted DLL run (docs/contracts/cli/gui.md section 2).
// purego has no Dlopen on Windows, so this goes through x/sys/windows; the handle is what
// purego.RegisterLibFunc takes there too.
func openLibrary() (uintptr, error) {
	h, err := windows.LoadLibraryEx("SDL2.dll", 0, windows.LOAD_LIBRARY_SEARCH_APPLICATION_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	return uintptr(h), err
}
