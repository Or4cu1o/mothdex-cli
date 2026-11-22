//go:build gui && windows

package sdl

import "golang.org/x/sys/windows"

// searchFlags are the only places a DLL may come from: the folder of the executable and System32. The current
// directory and PATH are never searched (docs/contracts/cli/gui.md section 2).
const searchFlags = windows.LOAD_LIBRARY_SEARCH_APPLICATION_DIR | windows.LOAD_LIBRARY_SEARCH_SYSTEM32

// openLibrary loads SDL2.dll from the folder of the executable or from System32, and nowhere else: never
// the current directory, which would let a planted DLL run.
// purego has no Dlopen on Windows, so this goes through x/sys/windows; the handle is what
// purego.RegisterLibFunc takes there too.
//
// LoadLibraryEx with these flags protects SDL2.dll and its own imports, but SDL loads more DLLs later, by
// name, with a plain LoadLibrary (vulkan-1, opengl32, XInput, hid, ...), and a plain LoadLibrary searches the
// current directory and PATH. SetDefaultDllDirectories makes the same two places the default of the whole
// process, and it runs first, so no later load can take a planted DLL. If it fails the library is not loaded:
// carrying on with the unsafe search order would be worse than no GUI (T-72, IC-8).
func openLibrary() (uintptr, error) {
	if err := windows.SetDefaultDllDirectories(searchFlags); err != nil {
		return 0, err
	}
	h, err := windows.LoadLibraryEx("SDL2.dll", 0, searchFlags)
	return uintptr(h), err
}
