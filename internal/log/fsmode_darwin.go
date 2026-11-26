//go:build darwin

package log

import (
	"slices"

	"golang.org/x/sys/unix"
)

// permissionless says, from statfs and never from the mode, whether dir is on a file system
// without permission bits. Tests replace it.
var permissionless = func(dir string) bool {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) != nil {
		return false
	}
	return slices.Contains([]string{"msdos", "exfat", "ntfs"}, unix.ByteSliceToString(st.Fstypename[:]))
}
