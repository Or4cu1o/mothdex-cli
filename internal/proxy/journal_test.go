package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileJournalOneFilePerJobAcrossInstances(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data", "inflight")
	a, b := NewFileJournal(dir), NewFileJournal(dir) // two processes
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ida, idb := strings.Repeat("a", 26), strings.Repeat("b", 26)
	if err := a.Add(JournalEntry{JobID: ida, DestDir: "snes", Started: now}); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(JournalEntry{JobID: idb, DestDir: "snes", Started: now}); err != nil {
		t.Fatal(err)
	}
	if err := a.Add(JournalEntry{JobID: ida}); err == nil {
		t.Fatal("an entry is created with O_EXCL: a second Add of the same id must fail")
	}
	if got, err := a.Entries(); err != nil || len(got) != 2 {
		t.Fatalf("both entries must be visible to either instance: %+v %v", got, err)
	}
	if err := b.Remove(ida); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := a.Entries()
	if err != nil || len(got) != 1 || got[0].JobID != idb {
		t.Fatalf("entries %+v, err %v", got, err)
	}
	if err := a.Add(JournalEntry{JobID: "../evil"}); err == nil {
		t.Fatal("a job id that is not 26 base32 characters must be refused")
	}
}
