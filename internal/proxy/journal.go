package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// MemJournal is an in-memory Journal for tests and for runs without a data directory.
type MemJournal struct {
	mu      sync.Mutex
	entries []JournalEntry
}

var _ Journal = (*MemJournal)(nil)

// Add implements Journal.
func (m *MemJournal) Add(e JournalEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

// Remove implements Journal.
func (m *MemJournal) Remove(jobID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = removeJob(m.entries, jobID)
	return nil
}

// Entries implements Journal.
func (m *MemJournal) Entries() ([]JournalEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]JournalEntry(nil), m.entries...), nil
}

func removeJob(in []JournalEntry, jobID string) []JournalEntry {
	out := in[:0:0]
	for _, e := range in {
		if e.JobID != jobID {
			out = append(out, e)
		}
	}
	return out
}

// FileJournal keeps the journal as JSON lines in <paths.data>/inflight.jsonl. The data
// directory belongs to the application, so plain os calls are used; the file is 0600 and
// every change rewrites it through a temporary file and a rename.
type FileJournal struct {
	mu   sync.Mutex
	path string
}

var _ Journal = (*FileJournal)(nil)

// NewFileJournal returns a journal stored at path (created on first use).
func NewFileJournal(path string) *FileJournal { return &FileJournal{path: path} }

// Add implements Journal.
func (j *FileJournal) Add(e JournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	cur, err := j.read()
	if err != nil {
		return err
	}
	return j.write(append(cur, e))
}

// Remove implements Journal.
func (j *FileJournal) Remove(jobID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	cur, err := j.read()
	if err != nil {
		return err
	}
	return j.write(removeJob(cur, jobID))
}

// Entries implements Journal. Lines that do not parse are skipped, never fatal.
func (j *FileJournal) Entries() ([]JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.read()
}

func (j *FileJournal) read() ([]JournalEntry, error) {
	b, err := os.ReadFile(j.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proxy: read journal: %w", err)
	}
	var out []JournalEntry
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var e JournalEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.JobID != "" {
			out = append(out, e)
		}
	}
	return out, nil
}

func (j *FileJournal) write(entries []JournalEntry) error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return fmt.Errorf("proxy: journal directory: %w", err)
	}
	var buf bytes.Buffer
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("proxy: write journal: %w", err)
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return fmt.Errorf("proxy: replace journal: %w", err)
	}
	return nil
}
