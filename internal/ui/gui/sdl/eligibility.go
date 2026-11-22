//go:build gui

package sdl

import "fmt"

// Check is the whole of `mothdex-gui --probe` on the SDL side (docs/contracts/cli/ports.md section 5.2): it loads
// the system SDL2 exactly as a normal start does, reads its version and applies the version rule. It starts no
// subsystem, opens no window and reads and writes no file of its own, so it can run as root on an appliance
// before the GUI is installed. The version is known whenever the library loaded, even when the rule refuses it.
func Check() (Version, error) {
	lib, err := Open()
	if err != nil {
		return Version{}, err
	}
	v := lib.Version()
	if !v.AtLeast(MinimumVersion()) {
		return v, fmt.Errorf("%w: have %s, need %s", ErrTooOld, v, MinimumVersion())
	}
	return v, nil
}
