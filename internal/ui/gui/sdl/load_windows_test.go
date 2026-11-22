//go:build gui && windows

package sdl

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// Once SDL is open, a plain LoadLibrary (which is what SDL itself calls later for vulkan-1, opengl32, XInput and
// hid) must not take a DLL from the current directory or from PATH (T-72, IC-8). The planted file is a copy of a
// real System32 DLL under another name, so a load that succeeds is a load of a planted file. Two controls keep
// the test honest: a file beside the executable and a System32 DLL must still load.
func TestPlainLoadLibraryIgnoresThePathAndTheWorkingFolderOnceSDLIsOpen(t *testing.T) {
	_, err := Open()
	skipWithoutSDL(t, err)
	if err != nil {
		t.Fatal(err)
	}

	dll, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), "System32", "version.dll")) //nolint:gosec // a fixed System32 file
	if err != nil {
		t.Fatal(err)
	}
	const planted, beside = "mothdex-plant-path.dll", "mothdex-plant-app.dll"
	cwd, onPath := t.TempDir(), t.TempDir()
	for _, dir := range []string{cwd, onPath} {
		if err := os.WriteFile(filepath.Join(dir, planted), dll, 0o600); err != nil { //nolint:gosec // inside t.TempDir()
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	besidePath := filepath.Join(filepath.Dir(exe), beside)
	if err := os.WriteFile(besidePath, dll, 0o600); err != nil { //nolint:gosec // next to this test binary
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(besidePath) })

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("PATH", onPath+string(os.PathListSeparator)+os.Getenv("PATH"))

	h, err := windows.LoadLibrary(planted)
	if err == nil {
		_ = windows.FreeLibrary(h)
		t.Fatal("a plain LoadLibrary took a DLL planted in the current directory and on PATH")
	}
	if !errors.Is(err, windows.ERROR_MOD_NOT_FOUND) {
		t.Fatalf("the planted DLL was refused for another reason than not being found: %v", err)
	}
	for _, name := range []string{beside, "version.dll"} { // the executable's folder and System32 stay in the search
		h, err := windows.LoadLibrary(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = windows.FreeLibrary(h)
	}
}
