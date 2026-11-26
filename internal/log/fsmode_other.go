//go:build !linux && !darwin && !windows

package log

// permissionless: unknown file systems are judged by their modes.
var permissionless = func(string) bool { return false }
