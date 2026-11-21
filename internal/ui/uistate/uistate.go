// Package uistate writes the small files a UI keeps for itself under paths.data (the Shell history,
// the TUI recent queries). They are conveniences: callers log a failure and carry on.
package uistate

import (
	"errors"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path through a temporary file in the same folder and a rename, with
// mode 0600, creating the folder (0700) when it is missing. A reader never sees a half-written file.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*") // created with mode 0600
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
