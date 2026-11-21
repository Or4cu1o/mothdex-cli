package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) }

// agingClock lets a test move time forward to expire cache entries.
type agingClock struct{ t time.Time }

func (c *agingClock) Now() time.Time { return c.t }

type fakeClient struct {
	mu       sync.Mutex
	search   mothdex.SearchResponse
	entries  map[string]mothdex.Entry
	failWith error
	calls    atomic.Int32
	randoms  []mothdex.Entry
	nRandom  int
}

func (f *fakeClient) Search(context.Context, mothdex.SearchRequest) (mothdex.SearchResponse, error) {
	f.calls.Add(1)
	return f.search, f.failWith
}

func (f *fakeClient) Entry(_ context.Context, slug string) (mothdex.Entry, error) {
	f.calls.Add(1)
	if f.failWith != nil {
		return mothdex.Entry{}, f.failWith
	}
	e, ok := f.entries[slug]
	if !ok {
		return mothdex.Entry{}, &mothdex.Error{Kind: mothdex.KindNotFound, Op: "entry"}
	}
	return e, nil
}

func (f *fakeClient) Random(context.Context, mothdex.Filters) (mothdex.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nRandom++
	if f.nRandom > len(f.randoms) {
		return mothdex.Entry{}, &mothdex.Error{Kind: mothdex.KindNetwork, Op: "random"}
	}
	return f.randoms[f.nRandom-1], nil
}

func (f *fakeClient) Platforms(context.Context) ([]mothdex.Platform, error) {
	f.calls.Add(1)
	return []mothdex.Platform{{ID: "snes", Brand: "Nintendo", Class: "Console"}, {ID: "gba", Brand: "Nintendo", Class: "Handheld"}, {ID: "md", Brand: "Sega", Class: "Console"}}, f.failWith
}

func (f *fakeClient) Regions(context.Context) ([]mothdex.Region, error) {
	return []mothdex.Region{{ID: "us"}, {ID: "eu"}}, f.failWith
}

func (f *fakeClient) Info(context.Context) (mothdex.DBInfo, error) {
	return mothdex.DBInfo{TotalEntries: 42}, f.failWith
}

func link(typ, fmtName, src, url string, size int64, extras ...string) mothdex.Link {
	return mothdex.Link{Type: typ, Format: fmtName, SourceName: src, URL: url, Size: size, Extras: extras, Filename: "x.zip"}
}

const goodURL = "https://proxy.mothdex.net/f/1"

func newSvc(t *testing.T, c *fakeClient, mod ...func(*Options)) *Service {
	t.Helper()
	store, err := cache.Open(filepath.Join(t.TempDir(), "cache"), cache.Config{Enabled: true, TTL: cache.TTL{Search: time.Hour, Entry: time.Hour, Meta: time.Hour, Info: time.Hour, Covers: time.Hour}}, clock{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Client: c, Cache: store, Clock: clock{}, BaseURL: "https://api.test", AllowedHosts: []string{"proxy.mothdex.net"}}
	for _, m := range mod {
		m(&o)
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func entries(slugs ...string) []mothdex.Entry {
	var out []mothdex.Entry
	for _, s := range slugs {
		out = append(out, mothdex.Entry{Slug: s, Title: strings.ToUpper(s), Platform: "snes", Links: []mothdex.Link{link("Game", "zip", "S", goodURL, int64(len(s)))}})
	}
	return out
}

var bidi = string(rune(0x202E)) // right-to-left override

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Case 28: links off the allowlist are never ranked; --link on one is untrusted_url.
func TestCase28OffProxyLinkIsExcludedAndRefused(t *testing.T) {
	c := &fakeClient{entries: map[string]mothdex.Entry{"e": {Slug: "e", Links: []mothdex.Link{
		link("Game", "zip", "Archive", "https://archive.org/download/x.zip", 1),
		link("Game", "zip", "Myrient", goodURL, 99),
	}}}}
	s := newSvc(t, c)

	got, err := s.Links(context.Background(), Ref{Slug: "e"}, LinkSelector{})
	if err != nil || len(got) != 2 || got[0].Index != 2 || !got[0].Available || got[1].Index != 1 || got[1].Available {
		t.Fatalf("ranking %+v %v", got, err)
	}
	if len(got[1].Reasons) != 1 || got[1].Reasons[0] != "host not allowed: archive.org" {
		t.Fatalf("reasons %v", got[1].Reasons)
	}
	if _, err := s.Links(context.Background(), Ref{Slug: "e"}, LinkSelector{Index: 1}); code(err) != CodeUntrustedURL {
		t.Fatalf("--link 1: %v", err)
	}
	if one, err := s.Links(context.Background(), Ref{Slug: "e"}, LinkSelector{Index: 2}); err != nil || len(one) != 1 || one[0].Index != 2 {
		t.Fatalf("--link 2: %v %v", one, err)
	}
	if _, err := s.Links(context.Background(), Ref{Slug: "e"}, LinkSelector{Index: 9}); code(err) != CodeInvalidRequest {
		t.Fatalf("--link 9: %v", err)
	}
}

func TestRankingRules(t *testing.T) {
	links := []mothdex.Link{
		link("Demo", "zip", "A", goodURL, 1, "En"),
		link("Game", "zip", "A", goodURL, 500, "Ja"),
		link("Game", "7z", "B", goodURL, 300, "En"),
		link("Game", "zip", "B", goodURL, 100, "En,Rev A"),
		link("Game", "zip", "A", goodURL, 200),
	}
	tests := []struct {
		name string
		pref Preference
		want []int // 1-based link indexes, ranked
	}{
		{"no preference: Game links in API order, Demo dropped", Preference{}, []int{2, 3, 4, 5}},
		{"language", Preference{Languages: []string{"en"}}, []int{3, 4, 2, 5}},
		{"language order", Preference{Languages: []string{"ja", "en"}}, []int{2, 3, 4, 5}},
		{"format after language", Preference{Languages: []string{"En"}, Formats: []string{"zip"}}, []int{4, 3, 2, 5}},
		{"source", Preference{Sources: []string{"b", "a"}}, []int{3, 4, 2, 5}},
		{"smallest", Preference{Size: SizeSmallest}, []int{4, 5, 3, 2}},
		{"largest", Preference{Size: SizeLargest}, []int{2, 3, 5, 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []int
			for _, c := range rankLinks(links, tt.pref, []string{"proxy.mothdex.net"}) {
				got = append(got, c.Index)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("%v, want %v", got, tt.want)
			}
		})
	}
	if got := rankLinks([]mothdex.Link{link("Demo", "zip", "A", goodURL, 1)}, Preference{}, []string{"proxy.mothdex.net"}); len(got) != 1 {
		t.Fatal("with no Game link every link stays")
	}
}

func TestSearchSessionIndexAndNormalisation(t *testing.T) {
	c := &fakeClient{search: mothdex.SearchResponse{Results: entries("b", "a", "c"), CurrentPage: 2, TotalPages: 4, TotalResults: 37}}
	s := newSvc(t, c)
	if _, err := s.Entry(context.Background(), Ref{Index: 1}); code(err) != CodeNoSession {
		t.Fatalf("no session: %v", err)
	}
	page, err := s.Search(context.Background(), SearchQuery{Text: "mario", Page: 2, Sort: SortTitle})
	if err != nil || page.Page != 2 || page.TotalPages != 4 || page.PastLastPage || page.Results[0].Slug != "a" || page.Results[0].Index != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	c.entries = map[string]mothdex.Entry{"a": entries("a")[0]}
	if res, err := s.Entry(context.Background(), Ref{Index: 1}); err != nil || res.Value.Slug != "a" {
		t.Fatalf("index 1 follows the sorted page: %v %v", res, err)
	}
	if _, err := s.Entry(context.Background(), Ref{Index: 4}); code(err) != CodeIndexOutOfRange {
		t.Fatalf("index 4: %v", err)
	}
	if _, err := s.Entry(context.Background(), Ref{Index: 1, Slug: "a"}); code(err) != CodeInvalidRequest {
		t.Fatal("index and slug together")
	}

	c.search = mothdex.SearchResponse{Results: entries("x"), CurrentPage: 4, TotalPages: 4, TotalResults: 37}
	if p, _ := s.Search(context.Background(), SearchQuery{Text: "mario", Page: 9}); !p.PastLastPage || p.Page != 4 {
		t.Fatalf("past last page: %+v", p)
	}
	c.search = mothdex.SearchResponse{CurrentPage: 0, TotalPages: 0}
	if p, _ := s.Search(context.Background(), SearchQuery{Text: "zzz"}); p.Page != 1 || p.TotalPages != 0 || len(p.Results) != 0 || p.PastLastPage {
		t.Fatalf("no results: %+v", p)
	}
	if _, err := s.Search(context.Background(), SearchQuery{Text: "x", Sort: "bogus"}); code(err) != CodeUsage {
		t.Fatal("bad sort is a usage error")
	}
}

func TestCacheStaleFallbackAndRefresh(t *testing.T) {
	c := &fakeClient{search: mothdex.SearchResponse{Results: entries("a"), CurrentPage: 1, TotalPages: 1, TotalResults: 1}}
	clk := &agingClock{t: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)}
	store, _ := cache.Open(filepath.Join(t.TempDir(), "cache"), cache.Config{Enabled: true, TTL: cache.TTL{Search: time.Hour}}, clk, nil)
	s, _ := New(Options{Client: c, Cache: store, Clock: clk, BaseURL: "u"})
	q := SearchQuery{Text: "mario"}

	if p, _ := s.Search(context.Background(), q); p.Meta.Cached {
		t.Fatal("first call is a miss")
	}
	if p, _ := s.Search(context.Background(), q); !p.Meta.Cached || c.calls.Load() != 1 {
		t.Fatalf("second call must hit the cache (%d client calls)", c.calls.Load())
	}
	q.Refresh = true
	if _, _ = s.Search(context.Background(), q); c.calls.Load() != 2 {
		t.Fatal("--refresh skips the cache read")
	}
	q.Refresh = false

	clk.t = clk.t.Add(2 * time.Hour) // expired
	c.failWith = &mothdex.Error{Kind: mothdex.KindNetwork, Op: "search"}
	p, err := s.Search(context.Background(), q)
	if err != nil || !p.Meta.Stale || !p.Meta.Cached {
		t.Fatalf("network failure serves the stale entry: %+v %v", p, err)
	}
	c.failWith = &mothdex.Error{Kind: mothdex.KindNotFound, Op: "search"}
	if _, err := s.Search(context.Background(), q); code(err) != CodeNotFound {
		t.Fatalf("only network and rate limit fall back to stale: %v", err)
	}
	clk.t = clk.t.Add(8 * 24 * time.Hour) // older than 7 days
	c.failWith = &mothdex.Error{Kind: mothdex.KindRateLimited, Op: "search"}
	if _, err := s.Search(context.Background(), q); code(err) != CodeRateLimited {
		t.Fatalf("beyond 7 days there is no stale fallback: %v", err)
	}
}

func TestRandomDedupsAndNeverFallsBackToStale(t *testing.T) {
	e := entries("a", "b", "c")
	c := &fakeClient{randoms: []mothdex.Entry{e[0], e[0], e[1], e[1], e[2]}}
	s := newSvc(t, c)
	res, err := s.Random(context.Background(), RandomQuery{Count: 3})
	if err != nil || len(res.Value) != 3 || c.nRandom != 5 {
		t.Fatalf("%d entries after %d calls: %v", len(res.Value), c.nRandom, err)
	}
	c2 := &fakeClient{randoms: []mothdex.Entry{e[0], e[0], e[0], e[0]}}
	if res, _ := newSvc(t, c2).Random(context.Background(), RandomQuery{Count: 2}); len(res.Value) != 1 || c2.nRandom != 4 {
		t.Fatalf("at most 2N attempts: %d entries, %d calls", len(res.Value), c2.nRandom)
	}
	if _, err := newSvc(t, &fakeClient{}).Random(context.Background(), RandomQuery{Count: 1}); code(err) != CodeNetwork {
		t.Fatalf("random fails with the network error: %v", err)
	}
	for _, n := range []int{0, 21} {
		if _, err := s.Random(context.Background(), RandomQuery{Count: n}); code(err) != CodeUsage {
			t.Fatalf("count %d", n)
		}
	}
}

func TestPlatformsRegionsInfo(t *testing.T) {
	s := newSvc(t, &fakeClient{})
	p, _ := s.Platforms(context.Background(), PlatformFilter{})
	if len(p.Value) != 3 || p.Value[0].ID != "gba" || p.Value[2].ID != "snes" {
		t.Fatalf("sorted by id: %+v", p.Value)
	}
	if n, _ := s.Platforms(context.Background(), PlatformFilter{Brand: "nintendo", Class: "console"}); len(n.Value) != 1 || n.Value[0].ID != "snes" {
		t.Fatalf("filter: %+v", n.Value)
	}
	if r, _ := s.Regions(context.Background()); r.Value[0].ID != "eu" {
		t.Fatalf("regions: %+v", r.Value)
	}
	if i, _ := s.DBInfo(context.Background()); i.Value.TotalEntries != 42 {
		t.Fatal("info")
	}
}

type fakeCovers struct {
	calls atomic.Int32
	store cache.Store
	ok    bool
	gate  chan struct{}
}

func (f *fakeCovers) Fetch(_ context.Context, u string) bool {
	f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	if !f.ok {
		return false
	}
	return f.store.PutBlob(cache.NSCovers, cache.KeyCover(u), strings.NewReader("PNGDATA"), "image/png", 1<<20) == nil
}

func TestCoverFromCacheThenFetchSingleflightAndNegativeCache(t *testing.T) {
	c := &fakeClient{entries: map[string]mothdex.Entry{
		"has":  {Slug: "has", CoverURL: "https://img.test/a.png"},
		"none": {Slug: "none"},
		"bad":  {Slug: "bad", CoverURL: "https://img.test/bad.png"},
	}}
	fc := &fakeCovers{ok: true, gate: make(chan struct{})}
	s := newSvc(t, c, func(o *Options) { o.Covers = fc })
	fc.store = s.o.Cache

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc, ct, err := s.Cover(context.Background(), Ref{Slug: "has"})
			if err != nil || rc == nil || ct != "image/png" {
				t.Errorf("cover: %v %q %v", rc, ct, err)
				return
			}
			if b, _ := io.ReadAll(rc); string(b) != "PNGDATA" {
				t.Errorf("body %q", b)
			}
			_ = rc.Close()
		}()
	}
	for fc.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(fc.gate)
	wg.Wait()
	if fc.calls.Load() != 1 {
		t.Fatalf("%d fetches for one URL", fc.calls.Load())
	}
	if rc, _, _ := s.Cover(context.Background(), Ref{Slug: "has"}); rc == nil || fc.calls.Load() != 1 {
		t.Fatal("a cached cover is served without fetching")
	} else {
		_ = rc.Close()
	}
	if rc, _, err := s.Cover(context.Background(), Ref{Slug: "none"}); rc != nil || err != nil {
		t.Fatal("an entry without a cover is nil, not an error")
	}

	fc.ok = false
	fc.gate = nil
	for range 3 {
		if rc, _, err := s.Cover(context.Background(), Ref{Slug: "bad"}); rc != nil || err != nil {
			t.Fatal("a refused cover is a miss")
		}
	}
	if fc.calls.Load() != 2 {
		t.Fatalf("a failed URL is tried once per session, %d fetches in total", fc.calls.Load())
	}
}

// Case 49 (service half): the whole Error() text is clean, including the cause, which for
// network and TLS errors quotes server data.
func TestCase49ErrorTextHasNoTerminalEscapes(t *testing.T) {
	hostile := "x509: certificate for \x1b]52;c;ZXZpbA==\x07evil\x1b[2J.example is not valid" + bidi + "\n"
	err := wrap(&mothdex.Error{Kind: mothdex.KindNetwork, Op: "search", Message: hostile, Err: errors.New(hostile)})
	msg := err.Error()
	if strings.ContainsAny(msg, "\x1b\x07\n\r"+bidi) {
		t.Fatalf("control characters survived: %q", msg)
	}
	e := &Error{Code: "internal", Message: hostile, Cause: errors.New(hostile)}
	if strings.ContainsAny(e.Error(), "\x1b\x07\n"+bidi) {
		t.Fatal("Message and Cause are cleaned too")
	}
	if len(SanitizeText(strings.Repeat("a", 5000))) > 2100 {
		t.Fatal("the text is bounded")
	}
}

func TestErrorCodesAndExitCodes(t *testing.T) {
	tests := map[error]struct {
		code string
		exit int
	}{
		&mothdex.Error{Kind: mothdex.KindNetwork}:           {CodeNetwork, 4},
		&mothdex.Error{Kind: mothdex.KindRateLimited}:       {CodeRateLimited, 4},
		&mothdex.Error{Kind: mothdex.KindNotFound}:          {CodeNotFound, 3},
		&mothdex.Error{Kind: mothdex.KindInvalidRequest}:    {CodeInvalidRequest, 2},
		&mothdex.Error{Kind: mothdex.KindUntrustedRedirect}: {CodeUntrustedRedirect, 4},
		&mothdex.Error{Kind: mothdex.KindDecode}:            {CodeAPIIncompatible, 4},
		&mothdex.Error{Kind: mothdex.KindCanceled}:          {CodeCanceled, 130},
		errors.New("boom"):                                  {CodeInternal, 1},
		context.Canceled:                                    {CodeCanceled, 130},
	}
	for in, want := range tests {
		var e *Error
		if !errors.As(wrap(in), &e) || e.Code != want.code || e.ExitCode() != want.exit {
			t.Errorf("%v: %+v, want %+v", in, e, want)
		}
	}
	if ExitCodeFor("size_mismatch") != 5 || ExitCodeFor("disk_full") != 6 || ExitCodeFor("exists") != 6 || ExitCodeFor("path_rejected") != 6 || ExitCodeFor("nope") != 1 {
		t.Fatal("download codes map to 5 and 6")
	}
}
