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

// Case 22: on_exists = overwrite from the file or the environment is refused (exit 2) without
// --force, accepted with it, and a flag value is left to the command and the downloader.
func TestCase22OverwriteFromFileOrEnvNeedsForce(t *testing.T) {
	file := Options{File: "c", ReadFile: memFile("version: 1\ndownload:\n  on_exists: overwrite\n")}
	env := Options{Environ: []string{"MOTHDEX_DOWNLOAD_ON_EXISTS=overwrite"}}
	for name, o := range map[string]Options{"file": file, "env": env} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(o)
			var e *Error
			if !errors.As(err, &e) || e.Key != "download.on_exists" || e.ExitCode() != 2 {
				t.Fatalf("err %v, want a download.on_exists config error with exit 2", err)
			}
			o.Force = true
			if s, _ := mustLoad(t, o).Get("download.on_exists"); s.Value != "overwrite" {
				t.Fatalf("with --force: %+v", s)
			}
		})
	}
	if _, err := Load(Options{Flags: map[string]string{"download.on_exists": "overwrite"}}); err != nil {
		t.Fatalf("a flag value is checked by the command: %v", err)
	}
}
