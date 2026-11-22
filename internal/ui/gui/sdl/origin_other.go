//go:build gui && !linux

package sdl

// originOf is empty off Linux: the question is where the system loader searched, and on Windows the DLL is the one next to
// the executable (docs/contracts/cli/gui.md section 2).
func originOf() string { return "" }
