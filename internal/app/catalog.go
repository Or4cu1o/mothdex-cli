package app

import (
	"context"
	"slices"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/session"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Search runs a search, normalizes the page numbers, sorts the page client-side and saves the
// session so that Ref{Index} works afterwards.
func (s *Service) Search(ctx context.Context, q SearchQuery) (SearchPage, error) {
	if q.PageSize < 1 || q.PageSize > maxPageSize {
		q.PageSize = s.o.PageSize
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Sort == "" {
		q.Sort = SortRelevance
	}
	if q.Sort != SortRelevance && q.Sort != SortTitle && q.Sort != SortSize {
		return SearchPage{}, newError(CodeUsage, "sort must be relevance, title or size")
	}
	req := mothdex.SearchRequest{
		Filters:    mothdex.Filters{SearchKey: q.Text, Platforms: q.Platforms, Regions: q.Regions, Serial: q.Serial},
		MaxResults: q.PageSize, Page: q.Page,
	}
	res, err := readThrough(s, cache.NSSearch, cache.KeySearch(s.o.BaseURL, req), q.Refresh, true, func() (mothdex.SearchResponse, error) {
		return s.o.Client.Search(ctx, req)
	})
	if err != nil {
		return SearchPage{}, err
	}
	resp := res.Value
	page := SearchPage{Query: q, Meta: res.Meta, TotalResults: resp.TotalResults, Page: resp.CurrentPage, TotalPages: resp.TotalPages}
	if len(resp.Results) == 0 {
		page.Page, page.TotalPages, page.TotalResults = 1, 0, 0
	}
	page.PastLastPage = len(resp.Results) > 0 && resp.CurrentPage > 0 && q.Page > resp.CurrentPage
	entries := slices.Clone(resp.Results)
	sortEntries(entries, q.Sort)
	slugs := make([]string, len(entries))
	for i, e := range entries {
		slugs[i] = e.Slug
		page.Results = append(page.Results, IndexedEntry{Index: i + 1, Entry: e})
	}
	if err := s.session.Save(session.Last{Query: session.Query{Text: q.Text, Platforms: q.Platforms, Regions: q.Regions, Serial: q.Serial, Sort: string(q.Sort)}, Page: page.Page, Slugs: slugs}); err != nil {
		return SearchPage{}, wrap(err)
	}
	return page, nil
}

// sortEntries orders the current page only; the API order is kept for ties and for relevance.
func sortEntries(es []mothdex.Entry, key SortKey) {
	switch key {
	case SortTitle:
		slices.SortStableFunc(es, func(a, b mothdex.Entry) int {
			return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		})
	case SortSize:
		slices.SortStableFunc(es, func(a, b mothdex.Entry) int { return int(topSize(a) - topSize(b)) })
	}
}

func topSize(e mothdex.Entry) int64 {
	if len(e.Links) == 0 {
		return 0
	}
	return e.Links[0].Size
}

// Entry returns one entry by index or slug.
func (s *Service) Entry(ctx context.Context, ref Ref) (Result[mothdex.Entry], error) {
	slug, err := s.resolve(ref)
	if err != nil {
		return Result[mothdex.Entry]{}, err
	}
	return readThrough(s, cache.NSEntry, cache.KeyEntry(s.o.BaseURL, slug), false, true, func() (mothdex.Entry, error) {
		return s.o.Client.Entry(ctx, slug)
	})
}

// Random returns Count (1..20) distinct random entries: at most 2*Count calls, deduplicated by
// slug, never cached and never served stale.
func (s *Service) Random(ctx context.Context, q RandomQuery) (Result[[]mothdex.Entry], error) {
	if q.Count < 1 || q.Count > maxRandom {
		return Result[[]mothdex.Entry]{}, newError(CodeUsage, "count must be between 1 and 20")
	}
	var out []mothdex.Entry
	seen := map[string]bool{}
	for attempt := 0; attempt < 2*q.Count && len(out) < q.Count; attempt++ {
		e, err := s.o.Client.Random(ctx, q.Filters)
		if err != nil {
			if len(out) > 0 {
				break // a partial answer is better than none
			}
			return Result[[]mothdex.Entry]{}, wrap(err)
		}
		if !seen[e.Slug] {
			seen[e.Slug] = true
			out = append(out, e)
		}
	}
	return Result[[]mothdex.Entry]{Value: out, Meta: ResultMeta{FetchedAt: s.o.Clock.Now()}}, nil
}

// Platforms lists platforms sorted by id, optionally filtered by brand or class.
func (s *Service) Platforms(ctx context.Context, f PlatformFilter) (Result[[]mothdex.Platform], error) {
	res, err := readThrough(s, cache.NSMeta, cache.KeyMeta(s.o.BaseURL, "platforms"), false, true, func() ([]mothdex.Platform, error) {
		return s.o.Client.Platforms(ctx)
	})
	if err != nil {
		return res, err
	}
	out := slices.Clone(res.Value)
	out = slices.DeleteFunc(out, func(p mothdex.Platform) bool {
		return (f.Brand != "" && !strings.EqualFold(p.Brand, f.Brand)) || (f.Class != "" && !strings.EqualFold(p.Class, f.Class))
	})
	slices.SortFunc(out, func(a, b mothdex.Platform) int { return strings.Compare(a.ID, b.ID) })
	res.Value = out
	return res, nil
}

// Regions lists regions sorted by id.
func (s *Service) Regions(ctx context.Context) (Result[[]mothdex.Region], error) {
	res, err := readThrough(s, cache.NSMeta, cache.KeyMeta(s.o.BaseURL, "regions"), false, true, func() ([]mothdex.Region, error) {
		return s.o.Client.Regions(ctx)
	})
	if err == nil {
		res.Value = slices.SortedFunc(slices.Values(res.Value), func(a, b mothdex.Region) int { return strings.Compare(a.ID, b.ID) })
	}
	return res, err
}

// DBInfo returns the database size.
func (s *Service) DBInfo(ctx context.Context) (Result[mothdex.DBInfo], error) {
	return readThrough(s, cache.NSInfo, cache.KeyInfo(s.o.BaseURL), false, true, func() (mothdex.DBInfo, error) {
		return s.o.Client.Info(ctx)
	})
}

// Links ranks the links of an entry (rankLinks). An explicit sel.Index returns just that link:
// out of range is invalid_request, a link off the downloader allowlist fails with untrusted_url
// (T-16 case 28).
func (s *Service) Links(ctx context.Context, ref Ref, sel LinkSelector) ([]LinkChoice, error) {
	res, err := s.Entry(ctx, ref)
	if err != nil {
		return nil, err
	}
	pref := sel.Prefer
	if len(pref.Languages)+len(pref.Formats)+len(pref.Sources) == 0 && pref.Size == SizeNone {
		pref = s.o.Prefer
	}
	ranked := rankLinks(res.Value.Links, pref, s.o.AllowedHosts)
	if sel.Index == 0 {
		return ranked, nil
	}
	i := slices.IndexFunc(ranked, func(c LinkChoice) bool { return c.Index == sel.Index })
	switch {
	case i < 0:
		return nil, newError(CodeInvalidRequest, "link number out of range")
	case !ranked[i].Available:
		return nil, newError(CodeUntrustedURL, "the link is not on the allowed download host list")
	}
	return ranked[i : i+1], nil
}
