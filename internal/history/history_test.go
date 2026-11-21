package history

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func rec(id, state string) Record {
	return Record{ID: id, StartedAt: t0, FinishedAt: t0.Add(time.Second), State: state, Slug: "slug-" + id, Title: "Title " + id, Platform: "snes",
		Filename: id + ".zip", Path: "/roms/snes/" + id + ".zip", Bytes: 10, Attempts: 1}
}

func open(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data", "history.jsonl")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestAppendListNewestFirstAndFailedFilter(t *testing.T) {
	s, p := open(t)
	if got, err := s.List(context.Background(), Filter{}); err != nil || got != nil {
		t.Fatalf("missing file: %v %v", got, err)
	}
	for i, st := range []string{"done", "failed", "done", "failed", "skipped"} {
		if err := s.Append(rec(fmt.Sprint(i), st)); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.List(context.Background(), Filter{})
	if len(all) != 5 || all[0].ID != "4" || all[4].ID != "0" {
		t.Fatalf("order %v", ids(all))
	}
	failed, _ := s.List(context.Background(), Filter{Failed: true})
	if got := ids(failed); strings.Join(got, ",") != "3,1" {
		t.Fatalf("--failed gave %v", got)
	}
	if lim, _ := s.List(context.Background(), Filter{Limit: 2}); len(lim) != 2 || lim[0].ID != "4" {
		t.Fatalf("limit: %v", ids(lim))
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("history must be private: %v %v", info, err)
	}
}

func ids(rs []Record) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

// A damaged or hostile line never breaks the history; a record with a newline in a field stays
// one line.
func TestDamagedLinesAreSkippedAndFieldsStayOnOneLine(t *testing.T) {
	s, p := open(t)
	evil := rec("e", "done")
	evil.RawName = "a\nb\r\n{\"id\":\"forged\"}"
	_ = s.Append(rec("1", "done"))
	_ = s.Append(evil)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // test path
	_, _ = f.WriteString("not json\n{\"state\":\"done\"}\n" + strings.Repeat("x", maxLine+5) + "\n")
	_ = f.Close()
	_ = s.Append(rec("2", "done"))

	got, err := s.List(context.Background(), Filter{})
	if err != nil || strings.Join(ids(got), ",") != "2,e,1" {
		t.Fatalf("%v %v", ids(got), err)
	}
}

func TestAppendCapsFieldsAndRefusesHugeRecords(t *testing.T) {
	s, _ := open(t)
	r := rec("1", "done")
	r.RawName = strings.Repeat("é", maxShortText)
	if err := s.Append(r); err != nil {
		t.Fatal(err)
	}
	got, _ := s.List(context.Background(), Filter{})
	if len(got[0].RawName) > maxShortText || !utf8.ValidString(got[0].RawName) {
		t.Fatalf("raw name %d bytes valid=%v", len(got[0].RawName), utf8.ValidString(got[0].RawName))
	}
	if _, err := Open("relative/history.jsonl"); err == nil {
		t.Fatal("a relative path must be refused")
	}
}

func TestConcurrentAppendsKeepEveryLine(t *testing.T) {
	s, _ := open(t)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.Append(rec(fmt.Sprint(i), "done")) }()
	}
	wg.Wait()
	if got, _ := s.List(context.Background(), Filter{}); len(got) != 40 {
		t.Fatalf("%d records", len(got))
	}
}

// Case 50: CSV formula guard, RFC 4180, control characters stripped before the guard looks.
func TestCase50CSVFormulaGuard(t *testing.T) {
	r := rec("1", "done")
	r.Filename = `=HYPERLINK("http://evil","x")`
	r.Title = "+1+1"
	r.Slug = string(rune(0x202E)) + "-2" // bidi override hides the minus
	r.Platform = "\x1b@SUM(A1)"          // ANSI escape hides the @
	r.Path = "\t=cmd"
	r.SHA256 = "\r=x"
	r.Error = &Failure{Code: "ok, with comma", Message: "m"}
	var buf bytes.Buffer
	if err := Export(&buf, FormatCSV, []Record{r}, Meta{}); err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(buf.Bytes(), []byte("\xef\xbb\xbf")) || !bytes.Contains(buf.Bytes(), []byte("\r\n")) {
		t.Fatal("CSV must be RFC 4180 without a BOM")
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil || len(rows) != 2 || strings.Join(rows[0], ",") != strings.Join(CSVColumns, ",") {
		t.Fatalf("rows %v %v", rows, err)
	}
	row := rows[1]
	col := func(name string) string {
		for i, c := range CSVColumns {
			if c == name {
				return row[i]
			}
		}
		t.Fatalf("no column %s", name)
		return ""
	}
	for name, want := range map[string]string{
		"filename": `'=HYPERLINK("http://evil","x")`, "title": "'+1+1", "slug": "'-2", "platform": "'@SUM(A1)",
		"path": "'=cmd", "sha256": "'=x", "error_code": "ok, with comma",
	} {
		if got := col(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	for i, c := range row {
		if c != "" && strings.ContainsRune("=+-@\t\r", rune(c[0])) {
			t.Errorf("cell %s starts with a formula character: %q", CSVColumns[i], c)
		}
	}
}

func TestExportJSONEnvelopeAndTXTWidth(t *testing.T) {
	r := rec("1", "failed")
	r.Filename = strings.Repeat("long-name-", 30) + "\x1b[2J"
	var j bytes.Buffer
	if err := Export(&j, FormatJSON, []Record{r}, Meta{CLIVersion: "0.1.0", FetchedAt: t0}); err != nil {
		t.Fatal(err)
	}
	var env struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
		Data          struct {
			Records []Record `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(j.Bytes(), &env); err != nil || env.SchemaVersion != 1 || !env.OK || len(env.Data.Records) != 1 || env.Data.Records[0].Filename != r.Filename {
		t.Fatalf("envelope: %v %+v", err, env)
	}
	var tx bytes.Buffer
	if err := Export(&tx, FormatTXT, []Record{r, rec("2", "done")}, Meta{}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(tx.String(), "\n"), "\n") {
		if utf8.RuneCountInString(line) > txtWidth || strings.ContainsRune(line, 0x1b) {
			t.Fatalf("line of %d runes or with an escape: %q", utf8.RuneCountInString(line), line)
		}
	}
	if Export(&tx, "xml", nil, Meta{}) == nil {
		t.Fatal("unknown format must fail")
	}
}

// Crivo minor 1: the JSON record has the fields of a download job plus the history ones
// (cli-output.md section 3), with error as an object or null.
func TestJSONRecordHasTheContractFields(t *testing.T) {
	ok, bad := rec("1", "done"), rec("2", "failed")
	bad.Error = &Failure{Code: "disk_full", Message: "no space left"}
	var buf bytes.Buffer
	if err := Export(&buf, FormatJSON, []Record{ok, bad}, Meta{}); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data struct {
			Records []map[string]any `json:"records"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil || len(env.Data.Records) != 2 {
		t.Fatal(err)
	}
	want := []string{"id", "started_at", "finished_at", "platform", "title", "source_name", "slug", "link_index", "filename", "raw_filename",
		"path", "state", "bytes", "expected_bytes", "sha256", "crc", "hash", "attempts", "duration_ms", "error"}
	for _, r := range env.Data.Records {
		for _, k := range want {
			if _, present := r[k]; !present {
				t.Errorf("field %q missing", k)
			}
		}
	}
	if env.Data.Records[0]["error"] != nil || env.Data.Records[1]["error"].(map[string]any)["code"] != "disk_full" {
		t.Fatalf("error: %v / %v", env.Data.Records[0]["error"], env.Data.Records[1]["error"])
	}
	if ok.DurationMS != 0 {
		t.Fatal("test record must start without a duration")
	}
}

func TestAppendFillsTheDurationAndCapsEveryField(t *testing.T) {
	s, _ := open(t)
	r := rec("1", "done")
	r.Slug = strings.Repeat("s", 1<<20)
	r.Platform, r.SHA256 = strings.Repeat("p", 5000), strings.Repeat("h", 5000)
	if err := s.Append(r); err != nil {
		t.Fatalf("a huge slug must be cut, not lose the record: %v", err)
	}
	got, _ := s.List(context.Background(), Filter{})
	if len(got) != 1 || len(got[0].Slug) > maxShortText || len(got[0].Platform) > maxIDText || got[0].DurationMS != 1000 {
		t.Fatalf("%+v", got)
	}
}

// Crivo minor 2: the 120 limit counts terminal cells, so wide (Japanese) names fit too.
func TestTXTWidthCountsCellsForWideRunes(t *testing.T) {
	r := rec("1", "done")
	r.Filename = strings.Repeat("ファイナルファンタジー", 20)
	var buf bytes.Buffer
	if err := Export(&buf, FormatTXT, []Record{r}, Meta{}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if cells(line) > txtWidth {
			t.Fatalf("%d cells: %q", cells(line), line)
		}
	}
	if cells("ab") != 2 || cells("日本") != 4 || cells("e\u0301") != 1 {
		t.Fatal("cells: ascii 1, wide 2, combining 0")
	}
}

// Crivo suggestion 3: the export never writes through or over something that is not ours.
func TestWriteExportDoesNotFollowOrReplaceForeignTargets(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	render := func(w io.Writer) error { _, err := w.Write([]byte("export")); return err }
	for name, link := range map[string]string{"to file": victim, "dangling": filepath.Join(dir, "absent")} {
		target := filepath.Join(dir, "link-"+strings.ReplaceAll(name, " ", "-"))
		if err := os.Symlink(link, target); err != nil {
			t.Skip("symlinks unavailable")
		}
		if err := WriteExport(target, false, render); !errors.Is(err, ErrExists) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" { //nolint:gosec // test path
		t.Fatalf("victim changed: %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("a dangling symlink target was created")
	}
}

func TestConcurrentExportsWithoutForceHaveOneWinner(t *testing.T) {
	target := filepath.Join(t.TempDir(), "out.csv")
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if WriteExport(target, false, func(w io.Writer) error { _, err := w.Write([]byte("x")); return err }) == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d writers succeeded", wins.Load())
	}
}

func TestExportTargetAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 20, 12, 30, 5, 0, time.UTC)
	if p, f, err := ExportTarget(dir, "CSV", "history", now); err != nil || f != "csv" || filepath.Base(p) != "mothdex-history-20261120T123005.csv" {
		t.Fatalf("dir target: %s %s %v", p, f, err)
	}
	if _, _, err := ExportTarget(dir, "", "history", now); !errors.Is(err, ErrFormat) {
		t.Fatalf("dir without format: %v", err)
	}
	target := filepath.Join(dir, "out.CSV")
	if _, f, err := ExportTarget(target, "", "history", now); err != nil || f != "csv" {
		t.Fatalf("inferred: %s %v", f, err)
	}
	if _, _, err := ExportTarget(filepath.Join(dir, "out.dat"), "", "history", now); !errors.Is(err, ErrFormat) {
		t.Fatalf("unknown extension: %v", err)
	}

	write := func(body string, force bool) error {
		return WriteExport(target, force, func(w io.Writer) error { _, err := w.Write([]byte(body)); return err })
	}
	if err := write("one", false); err != nil {
		t.Fatal(err)
	}
	if err := write("two", false); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite without force: %v", err)
	}
	if b, _ := os.ReadFile(target); string(b) != "one" { //nolint:gosec // test path
		t.Fatalf("the existing file changed: %q", b)
	}
	if err := write("three", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "three" { //nolint:gosec // test path
		t.Fatalf("force: %q", b)
	}
	if err := WriteExport(target, true, func(io.Writer) error { return errors.New("boom") }); err == nil {
		t.Fatal("a render error must fail the export")
	}
	if b, _ := os.ReadFile(target); string(b) != "three" { //nolint:gosec // test path
		t.Fatal("a failed export must leave the old file alone")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".mothdex-export-*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
