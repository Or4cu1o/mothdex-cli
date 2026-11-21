package history

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/width"
)

// Export formats (cli-output.md section 5).
const (
	FormatTXT  = "txt"
	FormatJSON = "json"
	FormatCSV  = "csv"
)

const (
	txtWidth      = 120
	schemaVersion = 1
)

// CSVColumns is the history column set of cli-output.md.
var CSVColumns = []string{"id", "started_at", "finished_at", "state", "slug", "title", "platform", "filename", "path", "bytes", "sha256", "crc", "attempts", "error_code"}

// Meta fills the envelope of the JSON export.
type Meta struct {
	CLIVersion string
	FetchedAt  time.Time
}

// Export writes records in the given format.
func Export(w io.Writer, format string, records []Record, meta Meta) error {
	switch format {
	case FormatJSON:
		return exportJSON(w, records, meta)
	case FormatCSV:
		return exportCSV(w, records)
	case FormatTXT:
		return exportTXT(w, records)
	}
	return fmt.Errorf("history: unknown export format %q", format)
}

// exportJSON writes the same envelope as --json, with the raw values (the encoder escapes them).
func exportJSON(w io.Writer, records []Record, meta Meta) error {
	if records == nil {
		records = []Record{}
	}
	env := map[string]any{
		"schema_version": schemaVersion,
		"command":        "history",
		"ok":             true,
		"data":           map[string]any{"records": records},
		"error":          nil,
		"meta":           map[string]any{"cached": false, "stale": false, "fetched_at": meta.FetchedAt.UTC().Format(time.RFC3339), "cli_version": meta.CLIVersion},
		"warnings":       []string{},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// exportCSV is RFC 4180 (CRLF, header row, no BOM). Every cell is stripped of control and format
// characters and guarded against spreadsheet formulas.
func exportCSV(w io.Writer, records []Record) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	if err := cw.Write(CSVColumns); err != nil {
		return err
	}
	for _, r := range records {
		row := []string{r.ID, stamp(r.StartedAt), stamp(r.FinishedAt), r.State, r.Slug, r.Title, r.Platform, r.Filename, r.Path,
			strconv.FormatInt(r.Bytes, 10), r.SHA256, r.CRC, strconv.Itoa(r.Attempts), r.errorCode()}
		for i, c := range row {
			row[i] = CSVCell(c)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// CSVCell removes Cc and Cf runes first (so a hidden control character cannot hide the leading
// '=') and then prefixes a quote to a cell that starts with = + - @ tab or CR.
func CSVCell(s string) string {
	s = StripUntrusted(s)
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// StripUntrusted drops Cc and Cf runes (ANSI escapes, bidi controls, zero-width) from external
// text before it reaches a terminal or a spreadsheet.
func StripUntrusted(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
}

func (r Record) errorCode() string {
	if r.Error == nil {
		return ""
	}
	return r.Error.Code
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// exportTXT is the human table without colors, every line at most 120 columns wide.
func exportTXT(w io.Writer, records []Record) error {
	cols := []struct {
		head  string
		width int
	}{{"STARTED", 20}, {"STATE", 9}, {"PLATFORM", 10}, {"SLUG", 28}, {"FILENAME", 0}}
	line := func(cells []string) string {
		var b strings.Builder
		used := 0
		for i, c := range cells {
			c = StripUntrusted(c)
			if cols[i].width == 0 {
				b.WriteString(fit(c, txtWidth-used))
				break
			}
			b.WriteString(pad(fit(c, cols[i].width-1), cols[i].width))
			used += cols[i].width
		}
		return strings.TrimRight(b.String(), " ")
	}
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.head
	}
	if _, err := io.WriteString(w, line(heads)+"\n"); err != nil {
		return err
	}
	for _, r := range records {
		if _, err := io.WriteString(w, line([]string{stamp(r.StartedAt), r.State, r.Platform, r.Slug, r.Filename})+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// cells is the display width of s in terminal cells: East Asian wide and fullwidth runes take
// two, combining marks none (golang.org/x/text/width, already a dependency).
func cells(s string) int {
	n := 0
	for _, r := range s {
		n += runeCells(r)
	}
	return n
}

func runeCells(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if k := width.LookupRune(r).Kind(); k == width.EastAsianWide || k == width.EastAsianFullwidth {
		return 2
	}
	return 1
}

// fit cuts s to at most n cells, ending in an ellipsis when it was longer.
func fit(s string, n int) string {
	if cells(s) <= n {
		return s
	}
	if n <= 1 {
		return ""
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		if w := runeCells(r); used+w <= n-1 {
			b.WriteRune(r)
			used += w
		} else {
			break
		}
	}
	return b.String() + "…"
}

func pad(s string, n int) string {
	if m := cells(s); m < n {
		return s + strings.Repeat(" ", n-m)
	}
	return s
}
