//go:build !linux && !darwin && !windows

package proxy

import (
	"errors"
	"os"
)

// renameNoReplace: there is no no-replace rename primitive wired for this OS, so the downloader
// takes the fallback chain (Link, then an O_EXCL placeholder).
func renameNoReplace(_ *RootFS, from, to string) error {
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: errors.ErrUnsupported}
}
