//go:build windows

package proxy

// IsMemoryBacked is only implemented on Linux; Windows reports false.
func IsMemoryBacked(string) bool { return false }
