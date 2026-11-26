//go:build linux

package log

import "golang.org/x/sys/unix"

// Magic numbers of statfs(2) for the file systems that have no permission bits.
var noModeFilesystems = map[uint32]bool{
	0x4d44:     true, // msdos (FAT)
	0x2011BAB0: true, // exfat
	0x7366746e: true, // ntfs3
	0x5346544e: true, // ntfs
	0x65735546: true, // fuseblk (ntfs-3g, exfat-fuse)
}

// permissionless says, from statfs and never from the mode, whether dir is on such a file system.
// Tests replace it.
var permissionless = func(dir string) bool {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) != nil {
		return false
	}
	return noModeFilesystems[uint32(st.Type)] //nolint:gosec,unconvert // the magic is 32 bits on every architecture
}
