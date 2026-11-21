//go:build !windows

package cache

import "os"

// renameOver renames src over dst; on POSIX systems this is atomic and works while readers hold dst open.
func renameOver(src, dst string) error { return os.Rename(src, dst) }
