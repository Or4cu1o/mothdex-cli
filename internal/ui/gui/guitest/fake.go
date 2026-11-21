// Package guitest is the fake app service the GUI tests share: a small catalog it can search, links it can
// rank, covers it can serve, and downloads it can queue, with every call recorded. It is not used by the
// program.
package guitest

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Fake implements the service the GUI uses. Set the exported fields before the test starts; the calls it
// receives are recorded and read with Calls.
type Fake struct {
	mu    sync.Mutex
	calls []string
	next  int

	Profile  app.ProfileInfo
	Entries  int64 // the catalog size DBInfo reports
	InfoErr  error
	Settings []app.Setting
	JobList  []app.JobSnapshot
	Batch    app.BatchResult

	Catalog    []mothdex.Entry             // what Search and Random draw from
	Platforms_ []mothdex.Platform          // what Platforms returns
	Regions_   []mothdex.Region            // what Regions returns
	LinkList   map[string][]app.LinkChoice // by slug; a slug with none gets one default link
	Records    []app.HistoryRecord
	Covers     map[string]bool // slugs that have a cover (a generated PNG); others answer not_found
	CoverBlock chan struct{}   // when set, Cover waits on it (or its context)

	SearchErr   error // when set, every Search fails with it
	EnqueueErr  error
	PlatformErr error
	Stale       bool // Search answers say they came from a stale cache
}

// New returns a fake with a Batocera profile and a catalog size.
func New() *Fake {
	return &Fake{
		Profile: app.ProfileInfo{ID: "batocera", Name: "Batocera", RomRoot: "/userdata/roms"},
		Entries: 1284019,
	}
}

func (f *Fake) record(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

// Calls is what the service was asked, in order, such as "search:zelda:1", "enqueue:slug:2", "cancel:j1".
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Count is how many recorded calls start with prefix.
func (f *Fake) Count(prefix string) int {
	n := 0
	for _, c := range f.Calls() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// Game is a catalog entry with one downloadable link.
func Game(slug, title, platform string, regions ...string) mothdex.Entry {
	return mothdex.Entry{
		Slug: slug, Title: title, Platform: platform, Regions: regions,
		Links: []mothdex.Link{{Type: "Game", Filename: slug + ".zip", URL: "https://example.org/" + slug, Size: 1 << 20, SizeStr: "1.0 MB", SourceName: "Myrient"}},
	}
}

// DBInfo reports the catalog size.
func (f *Fake) DBInfo(context.Context) (app.Result[mothdex.DBInfo], error) {
	if f.InfoErr != nil {
		return app.Result[mothdex.DBInfo]{}, f.InfoErr
	}
	return app.Result[mothdex.DBInfo]{Value: mothdex.DBInfo{TotalEntries: f.Entries}}, nil
}

// ConfigList returns the configured settings.
func (f *Fake) ConfigList() ([]app.Setting, error) { return f.Settings, nil }

// ActiveProfile returns the profile.
func (f *Fake) ActiveProfile() app.ProfileInfo { return f.Profile }

// Jobs returns the jobs the fake holds.
func (f *Fake) Jobs() []app.JobSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.JobList)
}

// Cancel records a cancel and marks the job canceled.
func (f *Fake) Cancel(id app.JobID) error {
	f.record("cancel:%s", id)
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.JobList {
		if f.JobList[i].ID == id {
			f.JobList[i].State = app.StateCanceled
		}
	}
	return nil
}

// Wait records the call and returns the configured batch.
func (f *Fake) Wait(context.Context) (app.BatchResult, error) {
	f.record("wait")
	return f.Batch, nil
}

// Close records the call.
func (f *Fake) Close() error { f.record("close"); return nil }

func matches(e mothdex.Entry, text string, platforms, regions []string) bool {
	if text != "" && !strings.Contains(strings.ToLower(e.Title), strings.ToLower(text)) {
		return false
	}
	if len(platforms) > 0 && !slices.Contains(platforms, e.Platform) {
		return false
	}
	if len(regions) > 0 && !slices.ContainsFunc(e.Regions, func(r string) bool { return slices.Contains(regions, r) }) {
		return false
	}
	return true
}

// Search filters the catalog by text, platforms and regions and cuts a page of q.PageSize.
func (f *Fake) Search(_ context.Context, q app.SearchQuery) (app.SearchPage, error) {
	page := max(q.Page, 1)
	f.record("search:%s:%d", q.Text, page)
	if f.SearchErr != nil {
		return app.SearchPage{}, f.SearchErr
	}
	var hits []mothdex.Entry
	for _, e := range f.Catalog {
		if matches(e, q.Text, q.Platforms, q.Regions) {
			hits = append(hits, e)
		}
	}
	size := max(q.PageSize, 1)
	total := (len(hits) + size - 1) / size
	from, to := min((page-1)*size, len(hits)), min(page*size, len(hits))
	out := app.SearchPage{Query: q, Page: page, TotalPages: max(total, 1), TotalResults: len(hits), Meta: app.ResultMeta{Stale: f.Stale, Cached: f.Stale, FetchedAt: time.Now().Add(-3 * time.Hour)}}
	for i, e := range hits[from:to] {
		out.Results = append(out.Results, app.IndexedEntry{Index: from + i + 1, Entry: e})
	}
	return out, nil
}

// Random returns the first q.Count entries that match.
func (f *Fake) Random(_ context.Context, q app.RandomQuery) (app.Result[[]mothdex.Entry], error) {
	f.record("random:%d", q.Count)
	var out []mothdex.Entry
	for _, e := range f.Catalog {
		if matches(e, "", q.Filters.Platforms, q.Filters.Regions) && len(out) < q.Count {
			out = append(out, e)
		}
	}
	return app.Result[[]mothdex.Entry]{Value: out}, nil
}

// Entry finds an entry of the catalog by slug.
func (f *Fake) Entry(_ context.Context, ref app.Ref) (app.Result[mothdex.Entry], error) {
	f.record("entry:%s", ref.Slug)
	for _, e := range f.Catalog {
		if e.Slug == ref.Slug {
			return app.Result[mothdex.Entry]{Value: e}, nil
		}
	}
	return app.Result[mothdex.Entry]{}, &app.Error{Code: app.CodeNotFound, Message: "no such entry"}
}

// Platforms returns the configured platforms.
func (f *Fake) Platforms(context.Context, app.PlatformFilter) (app.Result[[]mothdex.Platform], error) {
	f.record("platforms")
	if f.PlatformErr != nil {
		return app.Result[[]mothdex.Platform]{}, f.PlatformErr
	}
	return app.Result[[]mothdex.Platform]{Value: slices.Clone(f.Platforms_)}, nil
}

// Regions returns the configured regions.
func (f *Fake) Regions(context.Context) (app.Result[[]mothdex.Region], error) {
	f.record("regions")
	return app.Result[[]mothdex.Region]{Value: slices.Clone(f.Regions_)}, nil
}

// Links returns the ranked links of an entry: the configured ones, or its own links all marked available.
func (f *Fake) Links(_ context.Context, ref app.Ref, _ app.LinkSelector) ([]app.LinkChoice, error) {
	f.record("links:%s", ref.Slug)
	if l, ok := f.LinkList[ref.Slug]; ok {
		return slices.Clone(l), nil
	}
	for _, e := range f.Catalog {
		if e.Slug == ref.Slug {
			var out []app.LinkChoice
			for i, l := range e.Links {
				out = append(out, app.LinkChoice{Index: i + 1, Link: l, Score: 10 - i, Available: true})
			}
			return out, nil
		}
	}
	return nil, &app.Error{Code: app.CodeNotFound, Message: "no such entry"}
}

// Cover serves a generated PNG for the slugs in Covers and answers not_found for the others.
func (f *Fake) Cover(ctx context.Context, ref app.Ref) (io.ReadCloser, string, error) {
	f.record("cover:%s", ref.Slug)
	if f.CoverBlock != nil {
		select {
		case <-f.CoverBlock:
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	if !f.Covers[ref.Slug] {
		return nil, "", &app.Error{Code: app.CodeNotFound, Message: "no cover"}
	}
	return io.NopCloser(bytes.NewReader(PNG(120, 160, color.RGBA{R: 200, G: 80, B: 40, A: 255}))), "image/png", nil
}

// PNG is a solid picture of w x h pixels, encoded.
func PNG(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// Enqueue records the requests and adds a queued job for each.
func (f *Fake) Enqueue(_ context.Context, reqs []app.DownloadRequest) ([]app.JobID, error) {
	for _, r := range reqs {
		f.record("enqueue:%s:%d", r.Ref.Slug, r.Link.Index)
	}
	if f.EnqueueErr != nil {
		return nil, f.EnqueueErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []app.JobID
	for _, r := range reqs {
		f.next++
		id := app.JobID(fmt.Sprintf("j%d", f.next))
		title := r.Ref.Slug
		for _, e := range f.Catalog {
			if e.Slug == r.Ref.Slug {
				title = e.Title
			}
		}
		f.JobList = append(f.JobList, app.JobSnapshot{ID: id, Slug: r.Ref.Slug, Title: title, LinkIndex: max(r.Link.Index, 1), State: app.StateQueued})
		ids = append(ids, id)
	}
	return ids, nil
}

// SaveJobBoxart records the call.
func (f *Fake) SaveJobBoxart(_ context.Context, job app.JobSnapshot, _ string) (string, error) {
	f.record("boxart:%s", job.ID)
	return "", nil
}

// History returns the configured records.
func (f *Fake) History(context.Context, app.HistoryFilter) ([]app.HistoryRecord, error) {
	f.record("history")
	return slices.Clone(f.Records), nil
}
