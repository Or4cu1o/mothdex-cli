package app

import (
	"context"
	"io"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
)

// Cover returns the cover image of an entry from the covers cache, fetching it on a miss with
// the hardened cover fetcher (cache.md section 4). A nil reader means there is no cover: the
// entry has none, the fetch was refused (any policy violation is a miss) or failed. The caller
// closes the reader. The fetch runs outside the download pool; concurrent calls for one URL share
// one fetch, and a URL that failed is not tried again in this session.
func (s *Service) Cover(ctx context.Context, ref Ref) (io.ReadCloser, string, error) {
	res, err := s.Entry(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	url := res.Value.CoverURL
	if url == "" || s.o.Covers == nil {
		return nil, "", nil
	}
	key := cache.KeyCover(url)
	if rc, e, err := s.o.Cache.GetBlob(cache.NSCovers, key); err == nil && e.Hit {
		return rc, e.Meta.ContentType, nil
	}
	if !s.fetchCover(ctx, url) {
		return nil, "", nil
	}
	rc, e, err := s.o.Cache.GetBlob(cache.NSCovers, key)
	if err != nil || !e.Hit {
		return nil, "", nil // the cache is off or full: no cover, not an error
	}
	return rc, e.Meta.ContentType, nil
}

// fetchCover is a singleflight per URL with a per-session negative cache.
func (s *Service) fetchCover(ctx context.Context, url string) bool {
	s.coverMu.Lock()
	if s.coverFailed[url] {
		s.coverMu.Unlock()
		return false
	}
	if call, ok := s.coverFlight[url]; ok {
		s.coverMu.Unlock()
		select {
		case <-call.done:
			return call.ok
		case <-ctx.Done():
			return false
		}
	}
	call := &coverCall{done: make(chan struct{})}
	s.coverFlight[url] = call
	s.coverMu.Unlock()

	defer func() { // also runs if Fetch panics, so waiters never hang
		s.coverMu.Lock()
		delete(s.coverFlight, url)
		s.coverMu.Unlock()
		close(call.done)
	}()
	call.ok = s.o.Covers.Fetch(ctx, url)

	s.coverMu.Lock()
	if !call.ok && ctx.Err() == nil { // a canceled fetch says nothing about the URL
		if len(s.coverFailed) >= maxCoverFailed {
			clear(s.coverFailed)
		}
		s.coverFailed[url] = true
	}
	s.coverMu.Unlock()
	return call.ok
}
