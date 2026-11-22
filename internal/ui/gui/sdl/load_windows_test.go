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
// the test honest: a file beside the executable and a System32 DLL must still load. The System32 control is a
// DLL this process has not loaded yet: LoadLibrary returns a module that is already loaded before it searches
// any folder, so a DLL the process holds (version.dll, which SDL2.dll imports) would pass even with System32
// out of the search.
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
	// The executable's folder stays in the search.
	h, err = windows.LoadLibrary(beside)
	if err != nil {
		t.Fatalf("%s: %v", beside, err)
	}
	_ = windows.FreeLibrary(h)
	// System32 stays in the search: a DLL from there that this process does not hold, so the search really runs.
	const system32Only = "winhttp.dll"
	name, err := windows.UTF16PtrFromString(system32Only)
	if err != nil {
		t.Fatal(err)
	}
	var held windows.Handle
	if err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, name, &held); err == nil {
		t.Fatalf("%s is already loaded in this process, so loading it by name proves nothing: pick another System32 DLL", system32Only)
	}
	h, err = windows.LoadLibrary(system32Only)
	if err != nil {
		t.Fatalf("%s: %v", system32Only, err)
	}
	_ = windows.FreeLibrary(h)
}
