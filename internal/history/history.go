// Package history keeps the transfer history as JSONL (one record per terminal job) and exports
// it as txt, json or csv (app-service.md section 2, cli-output.md section 5).
//
// The file holds untrusted text (file names come from the network): it is JSON-escaped on
// write, control and format characters are stripped on every human or CSV output, and a damaged
// line is skipped on read, never fatal.
package history

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
	"unicode/utf8"
)

// Field caps keep a record under maxLine so that one write is one line.
const (
	maxLine      = 64 << 10
	maxShortText = 1024
	maxIDText    = 128
	maxPathText  = 4096
	dirMode      = 0o700
	fileMode     = 0o600
)

// Record is one history entry; the JSON names are the stable field names of cli-output.md.
type Record struct {
	ID            string    `json:"id"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	State         string    `json:"state"`
	Slug          string    `json:"slug"`
	Title         string    `json:"title"`
	Platform      string    `json:"platform"`
	SourceName    string    `json:"source_name"`
	LinkIndex     int       `json:"link_index"`
	Filename      string    `json:"filename"`
	RawName       string    `json:"raw_filename"`
	Path          string    `json:"path"`
	Bytes         int64     `json:"bytes"`
	Expected      int64     `json:"expected_bytes"` // the size the transfer was held to: content_length once known, else catalog_size
	CatalogSize   int64     `json:"catalog_size"`   // link.size, a rough estimate
	ContentLength int64     `json:"content_length"` // the proxy's Content-Length, 0 if no attempt got that far
	SHA256        string    `json:"sha256"`
	CRC           string    `json:"crc"`
	Hash          string    `json:"hash"`
	Attempts      int       `json:"attempts"`
	DurationMS    int64     `json:"duration_ms"`
	Error         *Failure  `json:"error"`
}

// Failure is the error object of a failed job: {code, message} (cli-output.md section 3), null
// when the job did not fail. The message is English and never echoes untrusted input.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Filter selects records for List. The zero value selects everything.
type Filter struct {
	Failed bool // only jobs whose state is failed
	Limit  int  // newest first; <= 0 means all
}

// Store is a JSONL history file. It is safe for concurrent use in one process.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open returns a Store for path (<paths.data>/history.jsonl). The file is created on the first
// Append.
func Open(path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("history: path must be absolute")
	}
	return &Store{path: path}, nil
}

// Append writes one record as a single line and syncs it. The downloader treats a failed
// append as best effort.
func (s *Store) Append(rec Record) error {
	rec.ID, rec.State, rec.Slug, rec.Platform = capText(rec.ID, maxIDText), capText(rec.State, maxIDText), capText(rec.Slug, maxShortText), capText(rec.Platform, maxIDText)
	rec.SHA256, rec.CRC, rec.Hash = capText(rec.SHA256, maxIDText), capText(rec.CRC, maxIDText), capText(rec.Hash, maxIDText)
	if rec.Error != nil {
		rec.Error = &Failure{Code: capText(rec.Error.Code, maxIDText), Message: capText(rec.Error.Message, maxShortText)}
	}
	if rec.DurationMS == 0 && !rec.StartedAt.IsZero() && rec.FinishedAt.After(rec.StartedAt) {
		rec.DurationMS = rec.FinishedAt.Sub(rec.StartedAt).Milliseconds()
	}
	rec.Title, rec.SourceName, rec.RawName = capText(rec.Title, maxShortText), capText(rec.SourceName, maxShortText), capText(rec.RawName, maxShortText)
	rec.Filename, rec.Path = capText(rec.Filename, maxShortText), capText(rec.Path, maxPathText)
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("history: encode: %w", err)
	}
	if len(line) >= maxLine {
		return errors.New("history: record too large")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), dirMode); err != nil {
		return fmt.Errorf("history: create directory: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode) //nolint:gosec // path comes from paths.data, validated absolute in Open
	if err != nil {
		return fmt.Errorf("history: open: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("history: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("history: sync: %w", err)
	}
	return f.Close()
}

// List returns the records newest first. A missing file is an empty history; lines that are not
// valid records are skipped.
func (s *Store) List(ctx context.Context, f Filter) ([]Record, error) {
	s.mu.Lock()
	file, err := os.Open(s.path) //nolint:gosec // path comes from paths.data, validated absolute in Open
	s.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: read: %w", err)
	}
	defer func() { _ = file.Close() }()
	var out []Record
	br := bufio.NewReaderSize(file, maxLine)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, tooLong, err := readLine(br)
		if len(line) > 0 && !tooLong {
			var r Record
			if json.Unmarshal(line, &r) == nil && r.ID != "" && (!f.Failed || r.State == "failed") {
				out = append(out, r)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("history: read: %w", err)
		}
	}
	slices.Reverse(out)
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// capText cuts s to at most n bytes on a rune boundary.
func capText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// readLine returns the next line without its newline. A line longer than the reader buffer is
// consumed to its end and reported as tooLong, so one oversized line costs one record, not the
// rest of the file.
func readLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	line, err = br.ReadSlice('\n')
	for errors.Is(err, bufio.ErrBufferFull) {
		tooLong = true
		_, err = br.ReadSlice('\n')
	}
	return bytes.TrimRight(line, "\r\n"), tooLong, err
}
