package proxy

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Confiner decides where a download may land (downloader.md 6.2). The destination directory
// must be inside the root of FS in every layout, checked per component and again after the
// operating system resolved every link (symlinks, 8.3 names, Windows junctions), and it must
// not be a launcher location.
type Confiner struct {
	// FS is rooted at the ROM root (or at the explicit -o directory, which becomes the root).
	FS *RootFS
	// IsLauncher is library.Profile.IsLauncherPath; the check is lexical, so it is applied to
	// the given path and again to the resolved one. nil disables it.
	IsLauncher func(abs string) bool
	// AllowExecutable is the --allow-executable flag: it lifts the launcher-folder refusal.
	AllowExecutable bool
}

// Resolve checks destdir and returns it as a slash-separated name inside FS ("." for the root
// itself), creating the directory (0700) through the confined root if needed. Errors are
// *Error: dest_outside_root, or path_rejected for a launcher location.
func (c *Confiner) Resolve(destdir string) (string, error) {
	abs, err := filepath.Abs(destdir)
	if err != nil || destdir == "" {
		return "", &Error{Code: CodePathRejected, Message: "destination directory is not usable"}
	}
	rel, ok := within(c.FS.Dir(), abs)
	if !ok {
		return "", outside()
	}
	if c.launcher(abs) {
		return "", launcherRefused()
	}
	// A link planted in the tree is caught before anything is created: resolve what exists.
	if err := c.checkExistingPart(filepath.Join(c.FS.Dir(), rel)); err != nil {
		return "", err
	}
	name := filepath.ToSlash(rel)
	if err := c.FS.MkdirAll(name, 0o700); err != nil {
		return "", &Error{Code: CodePathRejected, Message: "cannot create the destination directory", Err: err}
	}
	realRoot, err := finalPath(c.FS.Dir())
	if err != nil {
		return "", &Error{Code: CodeInternal, Message: "cannot resolve the root directory", Err: err}
	}
	realDest, err := finalPath(filepath.Join(c.FS.Dir(), rel))
	if err != nil {
		return "", &Error{Code: CodeInternal, Message: "cannot resolve the destination directory", Err: err}
	}
	if _, ok := within(realRoot, realDest); !ok {
		return "", outside()
	}
	if c.launcher(realDest) {
		return "", launcherRefused()
	}
	return name, nil
}

// checkExistingPart resolves the longest existing ancestor of dir and requires it to be inside
// the resolved root, so a symlink or junction that leaves the root is reported as
// dest_outside_root instead of a generic failure from the confined root.
func (c *Confiner) checkExistingPart(dir string) error {
	realRoot, err := finalPath(c.FS.Dir())
	if err != nil {
		return &Error{Code: CodeInternal, Message: "cannot resolve the root directory", Err: err}
	}
	for p := dir; ; p = filepath.Dir(p) {
		real, err := finalPath(p)
		if err == nil {
			if _, ok := within(realRoot, real); !ok {
				return outside()
			}
			return nil
		}
		if filepath.Dir(p) == p {
			return &Error{Code: CodePathRejected, Message: "cannot resolve the destination directory", Err: err}
		}
	}
}

func (c *Confiner) launcher(path string) bool {
	return !c.AllowExecutable && c.IsLauncher != nil && c.IsLauncher(path)
}

func outside() *Error {
	return &Error{Code: CodeDestOutsideRoot, Message: "the destination directory resolves outside the ROM root; pass the real path with -o"}
}

func launcherRefused() *Error {
	return &Error{Code: CodePathRejected, Message: "the destination is a launcher folder (use --allow-executable only if you mean it)"}
}

// within reports whether target is root or lies inside it, per path component and never as a
// string prefix (C:\roms against C:\roms2). On Windows the comparison ignores case and both
// paths must be on the same volume. rel is the path of target below root ("." for root).
func within(root, target string) (rel string, ok bool) {
	root, target = filepath.Clean(root), filepath.Clean(target)
	if !sameVolume(root, target) {
		return "", false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return rel, true
	}
	if !filepath.IsLocal(rel) {
		return "", false
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".." {
			return "", false
		}
	}
	return rel, true
}

func sameVolume(a, b string) bool {
	va, vb := filepath.VolumeName(a), filepath.VolumeName(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(va, vb)
	}
	return va == vb
}
