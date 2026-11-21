package config

// T-16 catalog cases 14 and 15 (loader side): a profile.folders value that is not one plain
// folder name, or that names a launcher folder, is a configuration error (exit 2).

import (
	"errors"
	"testing"
)

func TestCase14And15FolderValuesFailTheLoad(t *testing.T) {
	bad := map[string]string{
		"traversal":         "../x",
		"subfolder":         "a/b",
		"backslash":         `a\b`,
		"absolute":          "/etc",
		"launcher ports":    "ports",
		"launcher scripts":  "ports_scripts",
		"launcher windows":  "windows",
		"launcher any case": "Ports",
		"trailing dot":      "ports.",
		"look-alike":        "portſ",
		"device name":       "nul",
	}
	for name, folder := range bad {
		t.Run(name, func(t *testing.T) {
			file := "version: 1\nprofile:\n  folders:\n    md: '" + folder + "'\n"
			_, err := Load(Options{File: "c", ReadFile: memFile(file)})
			var e *Error
			if !errors.As(err, &e) || e.Key != "profile.folders.md" || e.ExitCode() != 2 {
				t.Fatalf("folders.md = %q: err %v, want a profile.folders.md config error with exit 2", folder, err)
			}
		})
	}
	if _, err := Load(Options{Flags: map[string]string{"profile.folders.md": "../x"}}); err == nil {
		t.Fatal("a flag value must be validated like a file value")
	}
}

// Case 22 (configuration half): the schema accepts on_exists = overwrite from the file and the
// environment and reports where it came from, so the command layer can refuse it without
// --force. The refusal itself is enforced by the downloader (proxy.New) and the command.
func TestCase22OverwriteIsReportedWithItsSource(t *testing.T) {
	file := mustLoad(t, Options{File: "c", ReadFile: memFile("version: 1\ndownload:\n  on_exists: overwrite\n")})
	if s, _ := file.Get("download.on_exists"); s.Value != "overwrite" || s.Source != SourceFile {
		t.Fatalf("file: %+v", s)
	}
	env := mustLoad(t, Options{Environ: []string{"MOTHDEX_DOWNLOAD_ON_EXISTS=overwrite"}})
	if s, _ := env.Get("download.on_exists"); s.Value != "overwrite" || s.Source != SourceEnv {
		t.Fatalf("env: %+v", s)
	}
}
