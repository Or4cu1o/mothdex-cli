package proxy

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileJournalPersistsAndSkipsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "inflight.jsonl")
	j := NewFileJournal(path)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "b"} {
		if err := j.Add(JournalEntry{JobID: id, Staging: "downloads/" + id + ".part", DestDir: "snes", Started: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Remove("a"); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // temp path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json\n{\"job_id\":\"\"}\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	got, err := NewFileJournal(path).Entries()
	if err != nil || len(got) != 1 || got[0].JobID != "b" {
		t.Fatalf("entries %+v, err %v", got, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("the journal must be private: %v %v", info, err)
	}
}
