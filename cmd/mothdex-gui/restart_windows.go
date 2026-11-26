//go:build gui && windows

package main

import (
	"os"
	"os/exec"
)

// restart starts the new program, by its absolute path and with argv only; this process then ends.
func restart(path string, args []string) error {
	cmd := exec.Command(path, args...) //nolint:gosec // this program's own path
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Start()
}
