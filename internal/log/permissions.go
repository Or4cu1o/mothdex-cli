package log

// Permissionless reports the installer's statfs exception for FAT, exFAT and other
// filesystems without Unix permission bits. The updater uses the same policy.
func Permissionless(dir string) bool { return permissionless(dir) }
