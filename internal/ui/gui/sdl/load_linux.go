//go:build gui && linux

package sdl

import "github.com/ebitengine/purego"

// soname is the only name tried: a versioned soname, never a path, so a planted file in the working
// directory or in $PATH is not picked up by name (docs/contracts/cli/gui.md section 2). The library is
// opened with RTLD_LOCAL: nothing else in the process needs SDL's symbols, and local avoids symbol
// interposition.
const soname = "libSDL2-2.0.so.0"

func openLibrary() (uintptr, error) {
	return purego.Dlopen(soname, purego.RTLD_NOW|purego.RTLD_LOCAL)
}
