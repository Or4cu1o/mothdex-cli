//go:build !windows

package cache

import "os"

// openShared opens path for reading. Only Windows needs more than os.Open: elsewhere an open file
// can always be replaced by a rename.
func openShared(path string) (*os.File, error) {
	return os.Open(path) //nolint:gosec // G304: path is built from a validated namespace and key
}
