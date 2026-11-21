//go:build !windows

package proxy

import "path/filepath"

// finalPath resolves every symlink of an existing directory.
func finalPath(dir string) (string, error) { return filepath.EvalSymlinks(dir) }
