package update

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var oldSuffix = regexp.MustCompile(`^(?:\.old(?:-[a-zA-Z0-9]+)?|\.failed)$`)

// CleanupOld touches only known regular backup files and old unlocked staging directories.
func CleanupOld(exeDir string, log *slog.Logger) {
	if !filepath.IsAbs(exeDir) {
		return
	}
	if _, err := trustedDir(exeDir); err != nil {
		return
	}
	unlock, err := acquireLock(exeDir)
	if err != nil {
		return
	}
	defer unlock()
	names := []string{"mothdex", "mothdex-gui"}
	if runtime.GOOS == "windows" {
		names = []string{"mothdex.exe", "mothdex-gui.exe", "SDL2.dll"}
	}
	b, err := readBounded(filepath.Join(exeDir, stateName), 16<<10)
	var s State
	if err == nil && decode(b, &s, false) == nil && validState(s) && samePath(s.Dir, exeDir) && s.Source == "installer" && s.SchemaVersion == 1 && s.Alias != "" && runtime.GOOS == "windows" {
		names = append(names, s.Alias+".exe")
	}
	entries, err := os.ReadDir(exeDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(exeDir, entry.Name())
		st, err := os.Lstat(path)
		if err != nil || isReparse(st) || st.Mode()&os.ModeSymlink != 0 {
			continue
		}
		remove := false
		for _, base := range names {
			suffix, ok := strings.CutPrefix(entry.Name(), base)
			if ok && oldSuffix.MatchString(suffix) && st.Mode().IsRegular() {
				remove = true
			}
		}
		if remove {
			if err = os.Remove(path); err != nil && log != nil {
				log.Debug("update cleanup retained file", "file", entry.Name())
			}
			continue
		}
		// No recursive deletion of arbitrary trees: staging cleanup accepts only known regular members.
		if strings.HasPrefix(entry.Name(), ".mothdex-update-") && st.IsDir() && time.Since(st.ModTime()) > time.Hour {
			files, e := os.ReadDir(path)
			if e != nil {
				continue
			}
			safe := true
			for _, f := range files {
				if _, e = regular(filepath.Join(path, f.Name())); e != nil {
					safe = false
					break
				}
			}
			if safe {
				for _, f := range files {
					_ = os.Remove(filepath.Join(path, f.Name()))
				}
				_ = os.Remove(path)
			}
		}
	}
}
