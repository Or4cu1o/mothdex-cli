package history

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrExists is returned by WriteExport when the target exists and force is false.
var ErrExists = errors.New("history: export file exists; use --force to replace it")

// ErrFormat is returned when the format is not txt, json or csv and cannot be inferred.
var ErrFormat = errors.New("history: export format must be txt, json or csv (pass --format)")

// ExportTarget resolves the -e/--export path and --format flag of cli-output.md section 5: a
// directory gets mothdex-<command>-<UTC yyyymmddThhmmss>.<fmt>; an empty format is inferred from
// the extension.
func ExportTarget(path, format, command string, now time.Time) (target, fmtName string, err error) {
	format = strings.ToLower(format)
	if info, serr := os.Stat(path); serr == nil && info.IsDir() {
		if format == "" {
			return "", "", ErrFormat
		}
		path = filepath.Join(path, fmt.Sprintf("mothdex-%s-%s.%s", command, now.UTC().Format("20060102T150405"), format))
	}
	format = strings.ToLower(format)
	if format == "" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	switch format {
	case FormatTXT, FormatJSON, FormatCSV:
		return path, format, nil
	}
	return "", "", ErrFormat
}

// WriteExport writes through render to a temp file next to target and publishes it atomically.
// Without force an existing target is never replaced (hard-link publish, so there is no
// check-then-rename race where links work).
func WriteExport(target string, force bool, render func(io.Writer) error) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".mothdex-export-*")
	if err != nil {
		return fmt.Errorf("history: export: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := render(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("history: export: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("history: export: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("history: export: %w", err)
	}
	if force {
		return os.Rename(name, target)
	}
	err = os.Link(name, target)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case errors.Is(err, errors.ErrUnsupported), errors.Is(err, os.ErrPermission):
		// No hard links here (FAT, some network shares): fall back to Lstat then Rename. This has
		// a small check-then-rename window; it only exists where Link cannot be used.
		if _, serr := os.Lstat(target); serr == nil {
			return ErrExists
		}
		return os.Rename(name, target)
	}
	return fmt.Errorf("history: export: %w", err)
}
