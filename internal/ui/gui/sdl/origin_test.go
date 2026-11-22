//go:build gui && linux

package sdl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The word for where SDL2 was loaded from, and never a path (ports.md PRT-G9): a system directory, an entry of
// LD_LIBRARY_PATH, or anywhere else.
func TestClassifyOriginSaysSystemLibraryPathOrOther(t *testing.T) {
	tests := []struct {
		name, path, ld, want string
	}{
		{"debian multiarch", "/usr/lib/x86_64-linux-gnu/libSDL2-2.0.so.0.3200.10", "", OriginSystem},
		{"arm hard float", "/usr/lib/arm-linux-gnueabihf/libSDL2-2.0.so.0.2800.5", "", OriginSystem},
		{"plain usr lib", "/usr/lib/libSDL2-2.0.so.0.3200.10", "", OriginSystem},
		{"lib64", "/lib64/libSDL2-2.0.so.0", "/emuelec/lib:/emuelec/lib32", OriginSystem},
		{"emuelec first entry", "/emuelec/lib/libSDL2-2.0.so.0", "/emuelec/lib:/emuelec/lib32", OriginLibraryPath},
		{"a later entry", "/storage/lib/libSDL2-2.0.so.0", "/emuelec/lib:/storage/lib/", OriginLibraryPath},
		{"entry that is also a system dir", "/usr/lib/libSDL2-2.0.so.0", "/usr/lib", OriginLibraryPath},
		{"an empty entry is the current directory", "/opt/x/libSDL2-2.0.so.0", ":/emuelec/lib", OriginOther},
		{"somewhere else", "/media/share/libSDL2-2.0.so.0", "/emuelec/lib", OriginOther},
		{"no path", "", "/emuelec/lib", OriginUnknown},
		{"a relative path", "libSDL2-2.0.so.0", "", OriginUnknown},
	}
	for _, tc := range tests {
		if got := classifyOrigin(tc.path, tc.ld); got != tc.want {
			t.Errorf("%s: %q with LD_LIBRARY_PATH %q is %q, want %q", tc.name, tc.path, tc.ld, got, tc.want)
		}
	}
}

// A path list that goes through a symbolic link still matches the real directory of the library, and only the word comes out.
func TestClassifyOriginFollowsLinksAndNeverReturnsAPath(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.Mkdir(real, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symbolic links are not available here")
	}
	got := classifyOrigin(filepath.Join(real, "libSDL2-2.0.so.0"), link)
	if got != OriginLibraryPath {
		t.Errorf("a link in LD_LIBRARY_PATH to the library's directory is %q, want %q", got, OriginLibraryPath)
	}
	for _, w := range []string{OriginSystem, OriginLibraryPath, OriginOther, OriginUnknown} {
		if strings.ContainsAny(w, "/\\ ") {
			t.Errorf("the word %q is not a plain word", w)
		}
	}
}

// A relative LD_LIBRARY_PATH entry is read from the current directory, as the loader does: LD_LIBRARY_PATH=lib with the
// library in ./lib is a path-list origin, not "other" (Crivo, T-89 S1).
func TestClassifyOriginResolvesRelativeEntriesFromTheCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "other", "deep"), 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	lib := filepath.Join(dir, "lib", "libSDL2-2.0.so.0")
	for _, ld := range []string{"lib", "./lib", "other/../lib", "/nonexistent:lib"} {
		if got := classifyOrigin(lib, ld); got != OriginLibraryPath {
			t.Errorf("LD_LIBRARY_PATH %q with the library in ./lib is %q, want %q", ld, got, OriginLibraryPath)
		}
	}
	t.Chdir(filepath.Join(dir, "other", "deep"))
	if got := classifyOrigin(lib, "../../lib"); got != OriginLibraryPath {
		t.Errorf("a ../ entry from another directory is %q, want %q", got, OriginLibraryPath)
	}
	if got := classifyOrigin(lib, "lib"); got != OriginOther { // from here ./lib is another place
		t.Errorf("the same relative entry from a directory without lib is %q, want %q", got, OriginOther)
	}
}
