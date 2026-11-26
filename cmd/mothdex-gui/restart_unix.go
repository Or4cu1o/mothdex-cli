//go:build gui && !windows

package main

import (
	"os"
	"syscall"
)

// restart replaces the process with the new program, by its absolute path and with argv only: no shell.
func restart(path string, args []string) error {
	return syscall.Exec(path, append([]string{path}, args...), os.Environ()) //nolint:gosec // this program's own path
}
