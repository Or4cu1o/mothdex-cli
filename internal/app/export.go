package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"syscall"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/history"
)

// Export formats of `search -e` and `history -e` (cli-output.md section 5).
const (
	FormatTXT  = history.FormatTXT
	FormatJSON = history.FormatJSON
	FormatCSV  = history.FormatCSV
)

// HistoryExportMeta fills the envelope of a JSON history export.
type HistoryExportMeta = history.Meta

// ExportTarget resolves the -e/--export path and --format flag: a directory gets
// mothdex-<command>-<UTC yyyymmddThhmmss>.<fmt>, an empty format is inferred from the extension.
// A format that cannot be inferred is a usage error.
func ExportTarget(path, format, command string, now time.Time) (target, fmtName string, err error) {
	target, fmtName, err = history.ExportTarget(path, format, command, now)
	if err != nil {
		return "", "", &Error{Code: CodeUsage, Message: SanitizeText(err.Error()), Args: map[string]any{"detail": SanitizeText(err.Error()), "command": command}}
	}
	return target, fmtName, nil
}

// WriteExport writes through render to a temporary file next to target and publishes it
// atomically; without force an existing target is never replaced. Failures carry stable codes
// (exists, path_rejected, permission_denied, disk_full: exit 6) and the target path, never the
// temporary name or the text of the Go error.
func WriteExport(target string, force bool, render func(io.Writer) error) error {
	err := history.WriteExport(target, force, render)
	if err == nil {
		return nil
	}
	args := map[string]any{"path": SanitizeText(target), "hint_key": "ui.cli.export.write_hint"}
	switch {
	case errors.Is(err, history.ErrExists):
		args["hint_key"] = "ui.cli.export.exists_hint"
		return &Error{Code: "exists", Message: "the export file already exists", Args: args, Cause: err}
	case errors.Is(err, syscall.ENOSPC):
		args["needed"] = "-"
		return &Error{Code: "disk_full", Message: "not enough free space for the export", Args: args, Cause: err}
	case errors.Is(err, fs.ErrPermission):
		return &Error{Code: CodePermissionDenied, Message: "cannot write the export file", Args: args, Cause: err}
	case errors.Is(err, fs.ErrNotExist):
		return &Error{Code: "path_rejected", Message: "the export folder does not exist", Args: args, Cause: err}
	}
	return &Error{Code: "path_rejected", Message: "cannot write the export file", Args: args, Cause: fmt.Errorf("export: %w", err)}
}

// ExportHistory writes records in the given format.
func ExportHistory(w io.Writer, format string, records []HistoryRecord, meta HistoryExportMeta) error {
	return history.Export(w, format, records, meta)
}

// EscapeC1 wraps w so that the C1 control runes U+0080-U+009F in JSON text are written as \u00XX
// escapes (case 49): encoding/json writes them raw, and U+009B and U+009D act as escape sequences
// on xterm-class terminals. Every JSON the CLI writes goes through it.
func EscapeC1(w io.Writer) io.Writer { return history.C1Escaper{W: w} }

// CSVCell prepares one text cell of a CSV export: control and format characters are removed and a
// leading = + - @ tab or CR is neutralized (formula injection).
func CSVCell(s string) string { return history.CSVCell(s) }
