//go:build !linux && !windows

package proxy

import "golang.org/x/sys/unix"

func blockSize(st *unix.Statfs_t) int64 { return int64(st.Bsize) }

// IsMemoryBacked is only implemented on Linux; elsewhere it reports false.
func IsMemoryBacked(string) bool { return false }
