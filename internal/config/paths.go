package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	appDir       = "mothdex"
	configName   = "config.yaml"
	portableFile = "mothdex.yaml"
	portableData = "mothdex-data"
)

// Platform is everything path resolution needs from the machine. Tests build it by hand.
type Platform struct {
	GOOS    string
	Getenv  func(string) string
	HomeDir string
	ExeDir  string
	Exists  func(path string) bool
}

// CurrentPlatform describes the running process.
func CurrentPlatform() (Platform, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Platform{}, fmt.Errorf("cannot determine the home directory: %w", err)
	}
	p := Platform{GOOS: runtime.GOOS, Getenv: os.Getenv, HomeDir: home, Exists: fileExists}
	if exe, err := os.Executable(); err == nil {
		p.ExeDir = filepath.Dir(exe)
	}
	return p, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ResolvedPaths are the concrete directories and files in use.
type ResolvedPaths struct {
	ConfigFile string
	Temp       string // cache and .part files
	Data       string // history.jsonl
	Logs       string
	ROMRoot    string // generic default; the platform id is appended by the downloader
	Portable   bool
}

// portable reports whether mothdex.yaml sits next to the executable.
func (p Platform) portable() bool {
	return p.ExeDir != "" && p.Exists != nil && p.Exists(filepath.Join(p.ExeDir, portableFile))
}

// FilePath picks the config file: flag > MOTHDEX_CONFIG > portable > OS default.
func FilePath(p Platform, flagPath string) (string, error) {
	if flagPath != "" {
		return flagPath, nil
	}
	if env := p.Getenv(EnvConfig); env != "" {
		return env, nil
	}
	if p.portable() {
		return filepath.Join(p.ExeDir, portableFile), nil
	}
	dir, err := osConfigDir(p)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configName), nil
}

// ResolvePaths fills the empty paths.* values from the OS defaults, or from
// <exe dir>/mothdex-data in portable mode. download.dir is not touched: ROMRoot only
// carries the generic default (<data>/ROMs, or <exe dir>/ROMs in portable mode).
func ResolvePaths(p Platform, cfg Config, flagPath string) (ResolvedPaths, error) {
	file, err := FilePath(p, flagPath)
	if err != nil {
		return ResolvedPaths{}, err
	}
	def, err := defaultDirs(p)
	if err != nil {
		return ResolvedPaths{}, err
	}
	data := firstNonEmpty(cfg.Paths.Data, def.Data)
	logs := filepath.Join(data, "logs") // <data>/logs, except on macOS where Logs has its own home
	if p.GOOS == "darwin" && !def.Portable {
		logs = def.Logs
	}
	return ResolvedPaths{
		ConfigFile: file,
		Temp:       firstNonEmpty(cfg.Paths.Temp, def.Temp),
		Data:       data,
		Logs:       firstNonEmpty(cfg.Paths.Logs, logs),
		ROMRoot:    def.ROMRoot,
		Portable:   def.Portable,
	}, nil
}

// defaultDirs are the built-in locations before user overrides.
func defaultDirs(p Platform) (ResolvedPaths, error) {
	if p.portable() {
		data := filepath.Join(p.ExeDir, portableData)
		return ResolvedPaths{Temp: filepath.Join(data, "cache"), Data: data, Logs: filepath.Join(data, "logs"),
			ROMRoot: filepath.Join(p.ExeDir, "ROMs"), Portable: true}, nil
	}
	d, err := osDirs(p)
	if err != nil {
		return ResolvedPaths{}, err
	}
	d.ROMRoot = filepath.Join(d.Data, "ROMs")
	return d, nil
}

var errNoHome = errors.New("cannot determine the home directory for default paths")

func osConfigDir(p Platform) (string, error) {
	switch p.GOOS {
	case "windows":
		return filepath.Join(winDir(p, "AppData", "Roaming"), appDir), nil
	case "darwin":
		if p.HomeDir == "" {
			return "", errNoHome
		}
		return filepath.Join(p.HomeDir, "Library", "Application Support", appDir), nil
	default:
		return xdgDir(p, "XDG_CONFIG_HOME", ".config", appDir)
	}
}

func osDirs(p Platform) (ResolvedPaths, error) {
	switch p.GOOS {
	case "windows":
		local := filepath.Join(winDir(p, "LocalAppData", "Local"), appDir)
		return ResolvedPaths{Temp: filepath.Join(local, "cache"), Data: local, Logs: filepath.Join(local, "logs")}, nil
	case "darwin":
		if p.HomeDir == "" {
			return ResolvedPaths{}, errNoHome
		}
		lib := filepath.Join(p.HomeDir, "Library")
		return ResolvedPaths{Temp: filepath.Join(lib, "Caches", appDir),
			Data: filepath.Join(lib, "Application Support", appDir), Logs: filepath.Join(lib, "Logs", appDir)}, nil
	default:
		cache, err := xdgDir(p, "XDG_CACHE_HOME", ".cache", appDir)
		if err != nil {
			return ResolvedPaths{}, err
		}
		data, err := xdgDir(p, "XDG_STATE_HOME", filepath.Join(".local", "state"), appDir)
		if err != nil {
			return ResolvedPaths{}, err
		}
		return ResolvedPaths{Temp: cache, Data: data, Logs: filepath.Join(data, "logs")}, nil
	}
}

// xdgDir honors an absolute $XDG_* value (a leading "/" counts on every host, so tests can run anywhere), else falls back to <home>/<fallback>.
func xdgDir(p Platform, envVar, fallback, leaf string) (string, error) {
	if v := p.Getenv(envVar); v != "" && (v[0] == '/' || filepath.IsAbs(v)) {
		return filepath.Join(v, leaf), nil
	}
	if p.HomeDir == "" {
		return "", errNoHome
	}
	return filepath.Join(p.HomeDir, fallback, leaf), nil
}

// winDir returns %envVar%, or <home>\AppData\<sub> when it is unset.
func winDir(p Platform, envVar, sub string) string {
	if v := p.Getenv(envVar); v != "" {
		return v
	}
	return filepath.Join(p.HomeDir, "AppData", sub)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
