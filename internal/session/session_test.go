package session

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
)

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }

func store(t *testing.T, enabled bool) *Store {
	t.Helper()
	c, err := cache.Open(filepath.Join(t.TempDir(), "cache"), cache.Config{Enabled: enabled}, clock{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return New(c)
}

func TestResolveByIndex(t *testing.T) {
	for _, enabled := range []bool{true, false} { // the session works with the cache disabled
		s := store(t, enabled)
		if _, err := s.Resolve(1); !errors.Is(err, ErrNoSession) {
			t.Fatalf("no session: %v", err)
		}
		if err := s.Save(Last{Query: Query{Text: "mario"}, Page: 1, Slugs: []string{"a", "b", "c"}}); err != nil {
			t.Fatal(err)
		}
		for index, want := range map[int]string{1: "a", 3: "c"} {
			if got, err := s.Resolve(index); err != nil || got != want {
				t.Fatalf("cache enabled=%v index %d: %q %v", enabled, index, got, err)
			}
		}
		for _, bad := range []int{0, -1, 4} {
			err := func() error { _, err := s.Resolve(bad); return err }()
			var e *Error
			if !errors.Is(err, ErrIndexOutOfRange) || !errors.As(err, &e) || e.Code() != "index_out_of_range" || e.ExitCode() != 3 {
				t.Fatalf("index %d: %v", bad, err)
			}
		}
	}
}

func TestSaveReplacesAndBoundsTheSession(t *testing.T) {
	s := store(t, true)
	_ = s.Save(Last{Slugs: []string{"old"}})
	slugs := make([]string, maxSlugs+50)
	for i := range slugs {
		slugs[i] = "x"
	}
	if err := s.Save(Last{Slugs: slugs}); err != nil {
		t.Fatal(err)
	}
	l, err := s.Load()
	if err != nil || len(l.Slugs) != maxSlugs {
		t.Fatalf("%d slugs, %v", len(l.Slugs), err)
	}
	if s.Save(Last{Slugs: []string{string(make([]byte, maxSlugBytes+1))}}) == nil {
		t.Fatal("an oversized slug must be refused")
	}
}
