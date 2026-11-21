// Package history keeps the transfer history as JSONL (one record per terminal job) and exports
// it as txt, json or csv (app-service.md section 2, cli-output.md section 5).
//
// The file holds untrusted text (file names come from the network): it is JSON-escaped on
// write, control and format characters are stripped on every human or CSV output, and a damaged
// line is skipped on read, never fatal.
package history

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	maxPathText  = 4096
	dirMode      = 0o700
	fileMode     = 0o600
)

// Record is one history entry; the JSON names are the stable field names of cli-output.md.
type Record struct {
	ID         string    `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	State      string    `json:"state"`
	Slug       string    `json:"slug"`
	Title      string    `json:"title,omitempty"`
	Platform   string    `json:"platform"`
	SourceName string    `json:"source_name,omitempty"`
	Filename   string    `json:"filename"`
	RawName    string    `json:"raw_filename,omitempty"`
	Path       string    `json:"path"`
	Bytes      int64     `json:"bytes"`
	SHA256     string    `json:"sha256,omitempty"`
	CRC        string    `json:"crc,omitempty"`
	Hash       string    `json:"hash,omitempty"`
	Attempts   int       `json:"attempts"`
	ErrorCode  string    `json:"error_code,omitempty"`
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
	data, err := os.ReadFile(s.path)
	s.mu.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("history: read: %w", err)
	}
	var out []Record
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(line) == 0 || len(line) >= maxLine {
			continue
		}
		var r Record
		if json.Unmarshal(line, &r) != nil || r.ID == "" {
			continue
		}
		if f.Failed && r.State != "failed" {
			continue
		}
		out = append(out, r)
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
