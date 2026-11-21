//go:build gui && !linux && !windows

package sdl

import "errors"

// The GUI ships for linux and windows only (docs/contracts/cli/gui.md section 6). This keeps `go vet -tags gui`
// working on a developer's other system.
func openLibrary() (uintptr, error) {
	return 0, errors.New("the GUI is not built for this operating system")
}
