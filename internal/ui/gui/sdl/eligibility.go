//go:build gui

package sdl

import "fmt"

// Check is the whole of `mothdex-gui --probe` on the SDL side (docs/contracts/cli/ports.md section 5.2): it loads
// the system SDL2 exactly as a normal start does, reads its version and applies the version rule. It starts no
// subsystem, opens no window and reads and writes no file of its own, so it can run as root on an appliance
// before the GUI is installed. The version is known whenever the library loaded, even when the rule refuses it. The one
// file it reads is the process's own memory map, to say which directory the loader took the library from.
func Check() (Found, error) {
	lib, err := Open()
	if err != nil {
		return Found{}, err
	}
	f := Found{Version: lib.Version(), Origin: lib.Origin()}
	if !f.Version.AtLeast(MinimumVersion()) {
		return f, fmt.Errorf("%w: have %s, need %s", ErrTooOld, f.Version, MinimumVersion())
	}
	return f, nil
}

// Found is the SDL2 that Check loaded: its version, and the word that says where the loader found it (Library.Origin).
type Found struct {
	Version Version
	Origin  string
}
