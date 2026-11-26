package update

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

// noticeDirectory validates the entire folder before any known file can be removed.
func noticeDirectory(dir string) ([]os.DirEntry, error) {
	st, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || isReparse(st) || st.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe licence folder")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !slices.Contains(guiNotices, e.Name()) {
			return nil, fmt.Errorf("unknown licence entry")
		}
		if _, err = regular(filepath.Join(dir, e.Name())); err != nil {
			return nil, err
		}
	}
	return entries, nil
}
func removeNoticeDirectory(dir string) error {
	entries, err := noticeDirectory(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if runtime.GOOS == "windows" {
			if err = os.Chmod(path, 0600); err != nil {
				return err
			}
		}
		if err = os.Remove(path); err != nil {
			return err
		}
	}
	return os.Remove(dir)
}
func replaceNotices(dir string, notices map[string][]byte) error {
	if len(notices) == 0 {
		return nil
	}
	dest := filepath.Join(dir, "mothdex-licenses")
	_, err := noticeDirectory(dest)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.MkdirTemp(dir, ".mothdex-licenses.new.")
	if err != nil {
		return err
	}
	defer func() { _ = removeNoticeDirectory(tmp) }()
	for name, data := range notices {
		if !slices.Contains(guiNotices, name) {
			return fmt.Errorf("unknown licence name")
		}
		path := filepath.Join(tmp, name)
		if err = writeExclusive(path, data, 0600); err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if runtime.GOOS == "windows" {
			mode = 0444
		}
		if err = os.Chmod(path, mode); err != nil {
			return err
		}
	}
	//nolint:gosec // Installer contract: licence directory 0755, files 0644.
	if err = os.Chmod(tmp, 0755); err != nil {
		return err
	}
	backup := ""
	if exists {
		// Revalidate before renaming: a foreign entry preserves the entire original folder.
		if _, err = noticeDirectory(dest); err != nil {
			return err
		}
		backup, err = os.MkdirTemp(dir, ".mothdex-licenses.old.")
		if err != nil {
			return err
		}
		if err = os.Remove(backup); err != nil {
			return err
		}
		if err = os.Rename(dest, backup); err != nil {
			return err
		}
	}
	if err = os.Rename(tmp, dest); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dest)
		}
		return err
	}
	if backup != "" {
		return removeNoticeDirectory(backup)
	}
	return nil
}
func (u *Updater) writeNotices(dir string, notices map[string][]byte) {
	if err := replaceNotices(dir, notices); err != nil {
		u.o.Log.Warn("update licence write failed", "cause", err)
	}
}
