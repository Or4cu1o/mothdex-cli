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

var stagingName = regexp.MustCompile(`^\.mothdex-update-[0-9]+$`)
var stagingMember = regexp.MustCompile(`^(?:mothdex(?:-gui)?(?:\.exe)?|SDL2\.dll|mothdex(?:-gui)?_[0-9A-Za-z.+_-]+\.(?:tar\.gz|zip))$`)
var oldSuffix = regexp.MustCompile(`^(?:\.old(?:-[a-zA-Z0-9]+)?|\.failed)$`)

// CleanupOld touches only known regular backup files and old unlocked staging directories.
func CleanupOld(exeDir string, log *slog.Logger) { cleanupOld(exeDir, log, time.Now()) }
func cleanupOld(exeDir string, log *slog.Logger, now time.Time) {
	if !filepath.IsAbs(exeDir) {
		return
	}
	if _, err := trustedDir(exeDir); err != nil {
		return
	}
	names := []string{"mothdex", "mothdex-gui"}
	if runtime.GOOS == "windows" {
		names = []string{"mothdex.exe", "mothdex-gui.exe", "SDL2.dll"}
	}
	b, err := readBounded(filepath.Join(exeDir, stateName), 16<<10)
	var s State
	if err != nil || decode(b, &s, false) != nil || !validState(s) || !samePath(s.Dir, exeDir) || s.Source != "installer" || s.SchemaVersion != 1 {
		return
	}
	unlock, err := acquireLock(exeDir)
	if err != nil {
		return
	}
	defer unlock()
	if s.Alias != "" && runtime.GOOS == "windows" {
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
		if stagingName.MatchString(entry.Name()) && st.IsDir() && now.Sub(st.ModTime()) > time.Hour {
			cleanupStage(path, log)
		}
	}
}

func cleanupStage(path string, log *slog.Logger) {
	unlock, err := acquireExistingLock(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if unlock == nil {
		unlock = func() {}
	}
	files, err := os.ReadDir(path)
	if err != nil {
		unlock()
		return
	}
	for _, f := range files {
		if _, err = regular(filepath.Join(path, f.Name())); err != nil || (f.Name() != ".mothdex-update.lock" && !stagingMember.MatchString(f.Name())) {
			unlock()
			return
		}
	}
	safe := true
	for _, f := range files {
		if f.Name() == ".mothdex-update.lock" {
			continue
		}
		if err = os.Remove(filepath.Join(path, f.Name())); err != nil {
			safe = false
			if log != nil {
				log.Debug("update staging cleanup retained file", "file", f.Name())
			}
		}
	}
	unlock()
	if safe {
		if err = os.Remove(filepath.Join(path, ".mothdex-update.lock")); err != nil && !os.IsNotExist(err) {
			return
		}
		_ = os.Remove(path)
	}
}
