package library

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// Env is everything detection reads from the machine. Tests build it by hand over a fake
// file tree; OSEnv builds the real one.
type Env struct {
	FS       fs.FS // rooted at "/" (on Windows the first element is the drive, "C:")
	GOOS     string
	Home     string
	ExeDir   string
	Writable func(dir string) bool // dir exists and accepts files
}

// OSEnv describes the running machine.
func OSEnv() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // detection then treats ${HOME} as unavailable instead of failing
	}
	env := Env{FS: hostFS{}, GOOS: runtime.GOOS, Home: home, Writable: dirWritable}
	if exe, err := os.Executable(); err == nil {
		env.ExeDir = filepath.Dir(exe)
	}
	return env, nil
}

// hostFS exposes the real file system as an fs.FS rooted at "/".
type hostFS struct{}

func (hostFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	root := "/"
	if runtime.GOOS == "windows" {
		root = ""
	}
	return os.Open(filepath.FromSlash(root + name))
}

// dirWritable creates and removes a temporary file, the only portable test of write access.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".mothdex-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name) == nil
}
