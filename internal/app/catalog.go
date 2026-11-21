package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
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
	entries := slices.DeleteFunc(slices.Clone(resp.Results), func(e mothdex.Entry) bool { return !mothdex.ValidSlug(e.Slug) })
	page.Dropped = len(resp.Results) - len(entries)
	sortEntries(entries, q.Sort)
	slugs := make([]string, len(entries))
	for i, e := range entries {
		slugs[i] = e.Slug
		page.Results = append(page.Results, IndexedEntry{Index: i + 1, Entry: e})
	}
	// The session is a convenience: when it cannot be saved (read-only cache) the page is still
	// returned and a later Ref{Index} reports no_session.
	last := session.Last{Query: session.Query{Text: q.Text, Platforms: q.Platforms, Regions: q.Regions, Serial: q.Serial, Sort: string(q.Sort)}, Page: page.Page, Slugs: slugs}
	if err := s.session.Save(last); err != nil {
		// The older session must not survive: <n> would resolve against a different search.
		s.session.Clear()
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
	res, err := readThrough(s, cache.NSEntry, cache.KeyEntry(s.o.BaseURL, slug), false, true, func() (mothdex.Entry, error) {
		e, err := s.o.Client.Entry(ctx, slug)
		if err == nil && !mothdex.ValidSlug(e.Slug) {
			return mothdex.Entry{}, errUnsafeSlug()
		}
		return e, err
	})
	if err == nil && !mothdex.ValidSlug(res.Value.Slug) { // an entry cached by an older build
		return Result[mothdex.Entry]{}, errUnsafeSlug()
	}
	var ae *Error
	if errors.As(err, &ae) && ae.Code == CodeNotFound && ae.Args == nil {
		ae.Args = map[string]any{"ref": slug} // fills "No entry found for {ref}."
		ae.Message = "no entry found for " + slug
	}
	return res, err
}

// Random returns Count (1..20) distinct random entries: at most 2*Count calls, deduplicated by
// slug, never cached and never served stale.
func (s *Service) Random(ctx context.Context, q RandomQuery) (Result[[]mothdex.Entry], error) {
	if q.Count < 1 || q.Count > maxRandom {
		return Result[[]mothdex.Entry]{}, newError(CodeUsage, "count must be between 1 and 20")
	}
	var out []mothdex.Entry
	seen := map[string]bool{}
	var unsafe bool
	for attempt := 0; attempt < 2*q.Count && len(out) < q.Count; attempt++ {
		e, err := s.o.Client.Random(ctx, q.Filters)
		if err == nil && !mothdex.ValidSlug(e.Slug) {
			unsafe = true // counts as a failed attempt
			continue
		}
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
	if len(out) == 0 && unsafe {
		return Result[[]mothdex.Entry]{}, errUnsafeSlug()
	}
	return Result[[]mothdex.Entry]{Value: out, Meta: ResultMeta{FetchedAt: s.o.Clock.Now()}}, nil
}

// errUnsafeSlug is the single-entry policy for an answer whose slug fails the rule (api-client section 2).
func errUnsafeSlug() error {
	return newError(CodeAPIIncompatible, "the service answered an entry whose slug has an unexpected form")
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
	if sel.Index != 0 {
		return s.explicitLink(res.Value.Links, sel.Index)
	}
	return rankLinks(res.Value.Links, pref, s.o.AllowedHosts), nil
}

// explicitLink resolves --link n against every link of the entry (an explicit index wins over the
// Game-only filter and any preference); the URL policy still applies: an off-allowlist link is
// untrusted_url (T-16 case 28).
func (s *Service) explicitLink(links []mothdex.Link, index int) ([]LinkChoice, error) {
	if index < 1 || index > len(links) {
		return nil, newError(CodeInvalidRequest, "link number out of range")
	}
	l := links[index-1]
	if _, err := proxy.CheckURL(l.URL, s.o.AllowedHosts); err != nil {
		return nil, newError(CodeUntrustedURL, "the link is not on the allowed download host list")
	}
	return []LinkChoice{{Index: index, Link: l, Score: 1, Available: true}}, nil
}
