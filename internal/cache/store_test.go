package cache

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func testConfig() Config {
	return Config{Enabled: true, MaxBytes: 10_000, TTL: TTL{
		Search: 24 * time.Hour, Entry: 24 * time.Hour, Meta: 168 * time.Hour, Info: time.Hour, Covers: 720 * time.Hour}}
}

func open(t *testing.T, cfg Config) (Store, *fakeClock, string) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	root := filepath.Join(t.TempDir(), "cache")
	s, err := Open(root, cfg, clock, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s, clock, root
}

type doc struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func putDoc(t *testing.T, s Store, ns, key string) {
	t.Helper()
	if err := s.PutJSON(ns, key, doc{"x", 1}); err != nil {
		t.Fatalf("PutJSON %s/%s: %v", ns, key, err)
	}
}

func hit(t *testing.T, s Store, ns, key string, opts ...GetOption) Entry {
	t.Helper()
	var d doc
	e, err := s.GetJSON(ns, key, &d, opts...)
	if err != nil {
		t.Fatalf("GetJSON %s/%s: %v", ns, key, err)
	}
	return e
}

func blob(n int) io.Reader { return bytes.NewReader(bytes.Repeat([]byte{'x'}, n)) }

func TestJSONRoundTripAndSidecar(t *testing.T) {
	s, clock, root := open(t, testConfig())
	if err := s.PutJSON(NSSearch, "k1", doc{"doom", 3}); err != nil {
		t.Fatal(err)
	}
	var got doc
	e, err := s.GetJSON(NSSearch, "k1", &got)
	if err != nil || !e.Hit || e.Stale || got != (doc{"doom", 3}) {
		t.Fatalf("got %+v %+v %v", got, e, err)
	}
	m := e.Meta
	if m.V != 1 || m.NS != NSSearch || !m.CreatedAt.Equal(clock.Now()) || m.ExpiresAt == nil || !m.ExpiresAt.Equal(clock.Now().Add(24*time.Hour)) || m.Size == 0 {
		t.Fatalf("sidecar %+v", m)
	}
	for _, f := range []string{"k1.json", "k1.meta.json"} {
		st, err := os.Stat(filepath.Join(root, NSSearch, f))
		if err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v, want 0600", f, st.Mode().Perm())
		}
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(root, NSSearch)); st.Mode().Perm() != 0o700 {
			t.Errorf("dir mode %v, want 0700", st.Mode().Perm())
		}
	}
	if e, _ := s.GetJSON(NSSearch, "missing", &got); e.Hit {
		t.Fatal("absent key must miss")
	}
}

func TestTTLBoundaryAndStale(t *testing.T) {
	s, clock, _ := open(t, testConfig())
	putDoc(t, s, NSInfo, "k") // TTL 1h
	clock.Advance(time.Hour - time.Nanosecond)
	if !hit(t, s, NSInfo, "k").Hit {
		t.Fatal("one tick before expiry must hit")
	}
	clock.Advance(time.Nanosecond)
	if hit(t, s, NSInfo, "k").Hit {
		t.Fatal("at the expiry instant the item is expired")
	}
	clock.Advance(2 * time.Hour) // expired for 2h
	tests := []struct {
		name      string
		maxAge    time.Duration
		wantHit   bool
		wantStale bool
	}{
		{"within allowance", 3 * time.Hour, true, true},
		{"exactly at allowance", 2 * time.Hour, true, true},
		{"beyond allowance", time.Hour, false, false},
	}
	for _, tt := range tests {
		e := hit(t, s, NSInfo, "k", AllowStale(tt.maxAge))
		if e.Hit != tt.wantHit || e.Stale != tt.wantStale {
			t.Errorf("%s: %+v", tt.name, e)
		}
	}
}

func TestDisabledAndZeroTTL(t *testing.T) {
	cfg := testConfig()
	cfg.TTL.Search = 0
	s, _, root := open(t, cfg)
	putDoc(t, s, NSSearch, "k")
	putDoc(t, s, NSEntry, "k")
	if hit(t, s, NSSearch, "k").Hit {
		t.Fatal("TTL 0 disables the namespace")
	}
	if _, err := os.Stat(filepath.Join(root, NSSearch)); err == nil {
		t.Fatal("TTL 0 must not write")
	}
	if !hit(t, s, NSEntry, "k").Hit {
		t.Fatal("other namespaces keep working")
	}

	cfg = testConfig()
	cfg.Enabled = false
	s, _, _ = open(t, cfg)
	putDoc(t, s, NSEntry, "k")
	putDoc(t, s, NSSession, "last")
	putDoc(t, s, NSState, "cooldown")
	if hit(t, s, NSEntry, "k").Hit {
		t.Fatal("disabled cache must miss")
	}
	if !hit(t, s, NSSession, "last").Hit || !hit(t, s, NSState, "cooldown").Hit {
		t.Fatal("session and state work even when the cache is disabled")
	}
}

func TestStateItemTTLAndSessionNeverExpires(t *testing.T) {
	s, clock, _ := open(t, testConfig())
	if err := s.PutJSON(NSState, "cooldown", doc{}, WithTTL(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	putDoc(t, s, NSSession, "last")
	clock.Advance(10 * time.Minute)
	if hit(t, s, NSState, "cooldown").Hit {
		t.Fatal("state item must expire at its own TTL")
	}
	clock.Advance(1000 * time.Hour)
	if e := hit(t, s, NSSession, "last"); !e.Hit || e.Meta.ExpiresAt != nil {
		t.Fatalf("session has no TTL: %+v", e)
	}
}

func TestCorruptItemsAreMissesAndDeleted(t *testing.T) {
	corrupt := map[string]func(dir string){
		"bad sidecar":   func(d string) { _ = os.WriteFile(filepath.Join(d, "k.meta.json"), []byte("{nope"), 0o600) },
		"no sidecar":    func(d string) { _ = os.Remove(filepath.Join(d, "k.meta.json")) },
		"no data":       func(d string) { _ = os.Remove(filepath.Join(d, "k.json")) },
		"size mismatch": func(d string) { _ = os.WriteFile(filepath.Join(d, "k.json"), []byte(`{"name":"truncated`), 0o600) },
		"bad JSON":      func(d string) { rewriteKeepingSize(d, "k.json", "not json at all!!") },
		"wrong ns": func(d string) {
			_ = os.WriteFile(filepath.Join(d, "k.meta.json"), []byte(`{"v":1,"ns":"entry","size":16}`), 0o600)
		},
	}
	for name, damage := range corrupt {
		t.Run(name, func(t *testing.T) {
			s, _, root := open(t, testConfig())
			putDoc(t, s, NSSearch, "k")
			dir := filepath.Join(root, NSSearch)
			damage(dir)
			if hit(t, s, NSSearch, "k").Hit {
				t.Fatal("corrupt item must miss")
			}
			if left, _ := os.ReadDir(dir); len(left) != 0 {
				t.Fatalf("corrupt item not deleted: %v", left)
			}
		})
	}
}

func rewriteKeepingSize(dir, name, text string) {
	path := filepath.Join(dir, name)
	st, _ := os.Stat(path)
	body := []byte(text + strings.Repeat(" ", max(int(st.Size())-len(text), 0)))
	_ = os.WriteFile(path, body[:st.Size()], 0o600)
}

func TestBlobRoundTrip(t *testing.T) {
	s, _, _ := open(t, testConfig())
	if err := s.PutBlob(NSCovers, "c1", strings.NewReader("PNGDATA"), "image/png", 100); err != nil {
		t.Fatal(err)
	}
	rc, e, err := s.GetBlob(NSCovers, "c1")
	if err != nil || rc == nil || !e.Hit || e.Meta.ContentType != "image/png" {
		t.Fatalf("got %+v %v", e, err)
	}
	defer func() { _ = rc.Close() }()
	if b, _ := io.ReadAll(rc); string(b) != "PNGDATA" {
		t.Fatalf("body %q", b)
	}
	if rc, e, _ := s.GetBlob(NSCovers, "none"); rc != nil || e.Hit {
		t.Fatal("miss must give a nil reader")
	}
}

func TestSizeLimits(t *testing.T) {
	s, _, root := open(t, testConfig()) // cap 10000, item limit 2500
	if err := s.PutBlob(NSCovers, "big", blob(2501), "image/png", 1<<20); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over a quarter of the cap: %v", err)
	}
	if err := s.PutBlob(NSCovers, "over", blob(60), "image/png", 50); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the caller limit: %v", err)
	}
	if err := s.PutBlob(NSCovers, "ok", blob(2500), "image/png", 1<<20); err != nil {
		t.Fatalf("exactly a quarter must fit: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, NSCovers))
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "ok.") {
			t.Fatalf("leftover file %s", e.Name())
		}
	}
}

func TestLRUEvictionDownToNinetyPercent(t *testing.T) {
	s, clock, _ := open(t, testConfig()) // cap 10000
	for _, k := range []string{"a", "b", "c", "d"} {
		if err := s.PutBlob(NSCovers, k, blob(2000), "image/png", 1<<20); err != nil {
			t.Fatal(err)
		}
		clock.Advance(2 * time.Minute)
	}
	// a hit after more than a minute refreshes last access: "a" becomes the newest.
	if rc, e, _ := s.GetBlob(NSCovers, "a"); !e.Hit {
		t.Fatal("a must hit")
	} else {
		_ = rc.Close()
	}
	clock.Advance(2 * time.Minute)
	if err := s.PutBlob(NSCovers, "e", blob(2000), "image/png", 1<<20); err != nil { // total passes the cap
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		rc, e, _ := s.GetBlob(NSCovers, k)
		if rc != nil {
			_ = rc.Close()
		}
		present[k] = e.Hit
	}
	if present["b"] || !present["a"] || !present["e"] {
		t.Fatalf("LRU should drop b (oldest unread), keep a and e: %v", present)
	}
	st, _ := s.Stats()
	if st.TotalBytes > 9000 {
		t.Fatalf("total %d must be at most 90%% of the cap", st.TotalBytes)
	}
}

func TestEvictionDropsExpiredBeforeLRU(t *testing.T) {
	s, clock, _ := open(t, testConfig())
	if err := s.PutBlob(NSCovers, "fresh", blob(2400), "image/png", 1<<20, WithTTL(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"e1", "e2"} {
		if err := s.PutBlob(NSCovers, k, blob(2400), "image/png", 1<<20, WithTTL(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(2 * time.Minute) // e1 and e2 expired
	if err := s.PutBlob(NSCovers, "new1", blob(2400), "image/png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBlob(NSCovers, "new2", blob(2400), "image/png", 1<<20); err != nil { // 12000 > cap: purge expired only
		t.Fatal(err)
	}
	st, _ := s.Stats()
	for _, ns := range st.Namespaces {
		if ns.Name == NSCovers && (ns.Items != 3 || ns.Expired != 0) {
			t.Fatalf("expired items should be purged first and LRU skipped: %+v", ns)
		}
	}
}

func TestExemptNamespacesAreNotEvicted(t *testing.T) {
	s, _, _ := open(t, testConfig())
	putDoc(t, s, NSSession, "last")
	putDoc(t, s, NSState, "flag")
	for _, k := range []string{"a", "b", "c", "d", "e", "f"} {
		if err := s.PutBlob(NSCovers, k, blob(2400), "image/png", 1<<20); err != nil {
			t.Fatal(err)
		}
	}
	if !hit(t, s, NSSession, "last").Hit || !hit(t, s, NSState, "flag").Hit {
		t.Fatal("session and state must survive eviction")
	}
}

func TestLastAccessRefreshedAtMostOncePerMinute(t *testing.T) {
	s, clock, root := open(t, testConfig())
	putDoc(t, s, NSEntry, "k")
	path := filepath.Join(root, NSEntry, "k.json")
	created := clock.Now()
	mtime := func() time.Time { st, _ := os.Stat(path); return st.ModTime() }
	clock.Advance(30 * time.Second)
	hit(t, s, NSEntry, "k")
	if !mtime().Equal(created) {
		t.Fatal("a hit within a minute must not touch the file")
	}
	clock.Advance(31 * time.Second)
	hit(t, s, NSEntry, "k")
	if !mtime().Equal(clock.Now()) {
		t.Fatalf("a hit after a minute must refresh last access, got %v", mtime())
	}
}

func TestDeleteClearAndStats(t *testing.T) {
	s, clock, _ := open(t, testConfig())
	putDoc(t, s, NSSearch, "a")
	putDoc(t, s, NSSearch, "b")
	putDoc(t, s, NSEntry, "c")
	putDoc(t, s, NSSession, "last")
	clock.Advance(25 * time.Hour) // search and entry (24h) are now expired
	st, err := s.Stats()
	if err != nil || len(st.Namespaces) != 7 || st.MaxBytes != 10_000 {
		t.Fatalf("stats %+v %v", st, err)
	}
	for _, ns := range st.Namespaces {
		switch ns.Name {
		case NSSearch:
			if ns.Items != 2 || ns.Expired != 2 || ns.Bytes == 0 {
				t.Errorf("search row %+v", ns)
			}
		case NSSession:
			if ns.Items != 1 || ns.Expired != 0 {
				t.Errorf("session row %+v", ns)
			}
		}
	}
	if err := s.Delete(NSSearch, "a"); err != nil || s.Delete(NSSearch, "nothing") != nil {
		t.Fatal("Delete must succeed, including for absent items")
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	st, _ = s.Stats()
	if st.TotalBytes != 0 {
		t.Fatalf("clear left %d bytes", st.TotalBytes)
	}
	if !hit(t, s, NSSession, "last").Hit {
		t.Fatal("Clear() with no arguments must keep session")
	}
	if err := s.Clear(NSSession); err != nil || hit(t, s, NSSession, "last").Hit {
		t.Fatal("explicit Clear(session) empties it")
	}
}

func TestPruneRemovesExpiredAndStaleTemps(t *testing.T) {
	s, clock, root := open(t, testConfig())
	putDoc(t, s, NSInfo, "old")
	putDoc(t, s, NSMeta, "keep")
	tmp := filepath.Join(root, NSInfo, ".tmp-abandoned")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, clock.Now(), clock.Now()); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Hour) // info (1h) expired, temp older than an hour
	if err := s.Prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); err == nil {
		t.Fatal("stale temp file must be removed")
	}
	if hit(t, s, NSInfo, "old").Hit || !hit(t, s, NSMeta, "keep").Hit {
		t.Fatal("Prune must drop only expired items")
	}
}

func TestReopenMeasuresExistingSize(t *testing.T) {
	s, clock, root := open(t, testConfig())
	for _, k := range []string{"a", "b", "c", "d"} {
		if err := s.PutBlob(NSCovers, k, blob(2000), "image/png", 1<<20); err != nil {
			t.Fatal(err)
		}
		clock.Advance(time.Minute)
	}
	again, err := Open(root, testConfig(), clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.PutBlob(NSCovers, "e", blob(2000), "image/png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if st, _ := again.Stats(); st.TotalBytes > 9000 {
		t.Fatalf("a reopened store must enforce the cap using the measured size, total %d", st.TotalBytes)
	}
}

func TestInvalidArguments(t *testing.T) {
	s, _, _ := open(t, testConfig())
	var d doc
	if _, err := s.GetJSON("nope", "k", &d); !errors.Is(err, ErrUnknownNamespace) {
		t.Errorf("unknown namespace: %v", err)
	}
	if err := s.PutJSON(NSSearch, "../escape", d); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("bad key: %v", err)
	}
	if err := s.PutBlob(NSSearch, "k", blob(1), "x", 10); !errors.Is(err, ErrWrongKind) {
		t.Errorf("blob into a JSON namespace: %v", err)
	}
	if err := s.PutJSON(NSCovers, "k", d); !errors.Is(err, ErrWrongKind) {
		t.Errorf("JSON into the covers namespace: %v", err)
	}
	if err := s.Clear("nope"); !errors.Is(err, ErrUnknownNamespace) {
		t.Errorf("clear unknown: %v", err)
	}
	if _, err := Open("", testConfig(), nil, nil); err == nil {
		t.Error("empty root must fail")
	}
}

func TestOverwriteKeepsTotalAccurate(t *testing.T) {
	s, _, _ := open(t, testConfig())
	for i := 0; i < 20; i++ {
		if err := s.PutBlob(NSCovers, "same", blob(2000), "image/png", 1<<20); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := s.Stats()
	if st.TotalBytes > 2500 {
		t.Fatalf("rewriting one key must not accumulate: %d", st.TotalBytes)
	}
	if err := s.PutBlob(NSCovers, "other", blob(2000), "image/png", 1<<20); err != nil {
		t.Fatal(err)
	}
	if rc, e, _ := s.GetBlob(NSCovers, "same"); !e.Hit {
		t.Fatal("overwrites must not trigger spurious eviction")
	} else {
		_ = rc.Close()
	}
}

// Regression: a reader that sees a half-written item must not delete the fresh one.
func TestConcurrentGetAndPutKeepTheLatestItem(t *testing.T) {
	s, _, _ := open(t, testConfig())
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		var d doc
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = s.GetJSON(NSSearch, "k", &d)
			}
		}
	}()
	for round := 0; round < 40; round++ {
		for i := 0; i < 100; i++ {
			if err := s.PutJSON(NSSearch, "k", doc{"x", i}); err != nil {
				t.Fatal(err)
			}
		}
		var d doc
		if e, _ := s.GetJSON(NSSearch, "k", &d); !e.Hit || d.N != 99 {
			close(stop)
			wg.Wait()
			t.Fatalf("round %d: the last Put was lost (%+v, %+v)", round, e, d)
		}
	}
	close(stop)
	wg.Wait()
}

func TestWithTTLCannotEnableADisabledNamespace(t *testing.T) {
	cfg := testConfig()
	cfg.TTL.Info = 0
	s, _, root := open(t, cfg)
	if err := s.PutJSON(NSInfo, "k", doc{}, WithTTL(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, NSInfo)); err == nil {
		t.Fatal("TTL 0 must hold even for items that ask for a TTL")
	}
}

func TestSlowBodyDoesNotBlockOtherWrites(t *testing.T) {
	s, _, _ := open(t, testConfig())
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.PutBlob(NSCovers, "slow", &gatedReader{started: started, release: release}, "image/png", 1<<20)
	}()
	<-started // the first Put is now inside its copy, holding no lock
	if err := s.PutJSON(NSEntry, "fast", doc{}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// gatedReader signals when first read, then blocks until released.
type gatedReader struct {
	started, release chan struct{}
	once             sync.Once
	done             bool
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	if g.done {
		return 0, io.EOF
	}
	g.done = true
	return copy(p, "data"), nil
}

// A reader that holds a cache file open must never block a Put that replaces it (Windows: the
// readers open with FILE_SHARE_DELETE). Deterministic: the reader stays open across the Put.
func TestPutReplacesAnItemThatAReaderHoldsOpen(t *testing.T) {
	s, _, _ := open(t, testConfig())
	if err := s.PutBlob(NSCovers, "k", strings.NewReader("old"), "image/png", 1<<20); err != nil {
		t.Fatal(err)
	}
	r, e, err := s.GetBlob(NSCovers, "k")
	if err != nil || !e.Hit {
		t.Fatalf("%v %+v", err, e)
	}
	defer func() { _ = r.Close() }()
	if err := s.PutBlob(NSCovers, "k", strings.NewReader("new"), "image/png", 1<<20); err != nil {
		t.Fatalf("a Put over an item held open by a reader must succeed: %v", err)
	}
	if b, _ := io.ReadAll(r); string(b) != "old" {
		t.Fatalf("the open reader keeps the version it opened, got %q", b)
	}
	r2, _, err := s.GetBlob(NSCovers, "k")
	if err != nil || r2 == nil {
		t.Fatalf("%v", err)
	}
	defer func() { _ = r2.Close() }()
	if b, _ := io.ReadAll(r2); string(b) != "new" {
		t.Fatalf("a new reader sees the new version, got %q", b)
	}
}
