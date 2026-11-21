package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// FileJournal keeps one small file per in-flight job, <dir>/<jobID>.json (the "inflight"
// directory under paths.data). One file per job means two processes (CLI and TUI, or two runs)
// never rewrite each other's entries: Add creates the file with O_EXCL, Remove deletes it.
// Add flushes the file and the directory so that the entry survives a crash, which is the
// point of a journal.
type FileJournal struct{ dir string }

var _ Journal = (*FileJournal)(nil)

// NewFileJournal returns a journal stored in dir (created on first use, 0700).
func NewFileJournal(dir string) *FileJournal { return &FileJournal{dir: dir} }

func (j *FileJournal) file(jobID string) (string, error) {
	if !jobIDRe.MatchString(jobID) {
		return "", fmt.Errorf("proxy: invalid job id in the journal")
	}
	return filepath.Join(j.dir, jobID+".json"), nil
}

// Add implements Journal.
func (j *FileJournal) Add(e JournalEntry) error {
	name, err := j.file(e.JobID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(j.dir, 0o700); err != nil {
		return fmt.Errorf("proxy: journal directory: %w", err)
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // name is dir + a validated job id
	if err != nil {
		return fmt.Errorf("proxy: create journal entry: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return fmt.Errorf("proxy: write journal entry: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return fmt.Errorf("proxy: flush journal entry: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	syncDirPath(j.dir)
	return nil
}

// Remove implements Journal. A missing entry is not an error.
func (j *FileJournal) Remove(jobID string) error {
	name, err := j.file(jobID)
	if err != nil {
		return err
	}
	if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("proxy: remove journal entry: %w", err)
	}
	syncDirPath(j.dir)
	return nil
}

// Entries implements Journal. Files that do not parse or are not named <jobID>.json are
// skipped, never fatal.
func (j *FileJournal) Entries() ([]JournalEntry, error) {
	items, err := os.ReadDir(j.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proxy: read journal: %w", err)
	}
	var out []JournalEntry
	for _, it := range items {
		id, ok := strings.CutSuffix(it.Name(), ".json")
		if !ok || !it.Type().IsRegular() || !jobIDRe.MatchString(id) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(j.dir, it.Name())) //nolint:gosec // dir + a validated name
		if err != nil {
			continue
		}
		var e JournalEntry
		if json.Unmarshal(b, &e) == nil && e.JobID == id {
			out = append(out, e)
		}
	}
	return out, nil
}

// syncDirPath flushes a directory; failures (Windows cannot) are ignored.
func syncDirPath(dir string) {
	if d, err := os.Open(dir); err == nil { //nolint:gosec // the journal directory
		_ = d.Sync()
		_ = d.Close()
	}
}
