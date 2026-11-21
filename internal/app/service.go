package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
	"github.com/Or4cu1o/mothdex-cli/internal/session"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

const (
	staleMax       = 7 * 24 * time.Hour
	maxPageSize    = 100
	maxRandom      = 20
	defaultPage    = 10
	maxCoverFailed = 256
)

// APIClient is the part of mothdex.Client the service uses; tests pass a fake.
type APIClient interface {
	Search(ctx context.Context, r mothdex.SearchRequest) (mothdex.SearchResponse, error)
	Entry(ctx context.Context, slug string) (mothdex.Entry, error)
	Random(ctx context.Context, f mothdex.Filters) (mothdex.Entry, error)
	Platforms(ctx context.Context) ([]mothdex.Platform, error)
	Regions(ctx context.Context) ([]mothdex.Region, error)
	Info(ctx context.Context) (mothdex.DBInfo, error)
}

// CoverFetcher downloads a cover into the cache (internal/cover.Fetcher).
type CoverFetcher interface {
	Fetch(ctx context.Context, rawURL string) bool
}

// Options builds the catalog half of the service.
type Options struct {
	Client       APIClient
	Cache        cache.Store
	Clock        cache.Clock
	BaseURL      string // part of the cache keys
	Covers       CoverFetcher
	AllowedHosts []string // downloader URL policy (download.allowed_hosts)
	PageSize     int      // default interface.page_size
	Prefer       Preference
	History      *history.Store
	Config       ConfigSource
	Profile      library.ProfileInfo
	Detect       func(context.Context) (library.ProfileInfo, error) // `profile detect`
}

// Service implements the catalog capability; later capabilities are added to the same type.
type Service struct {
	o       Options
	session *session.Store
	dl      *downloads
	hist    *history.Store
	cfg     ConfigSource
	profMu  sync.RWMutex // guards profile
	profile library.ProfileInfo
	detect  func(context.Context) (library.ProfileInfo, error)

	coverMu     sync.Mutex
	coverFlight map[string]*coverCall
	coverFailed map[string]bool
}

type coverCall struct {
	done    chan struct{}
	ok      bool
	waiters atomic.Int32 // callers that joined this fetch; read by tests to know all are inside
}

// New returns a Service. Client, Cache and Clock are required.
func New(o Options) (*Service, error) {
	if o.Client == nil || o.Cache == nil || o.Clock == nil {
		return nil, errors.New("app: Client, Cache and Clock are required")
	}
	if o.PageSize < 1 || o.PageSize > maxPageSize {
		o.PageSize = defaultPage
	}
	return &Service{o: o, hist: o.History, cfg: o.Config, profile: o.Profile, detect: o.Detect, session: session.New(o.Cache), coverFlight: map[string]*coverCall{}, coverFailed: map[string]bool{}}, nil
}

// readThrough serves key from the cache when fresh, otherwise calls fetch and stores the answer;
// on a network or rate-limit failure it serves an entry expired up to 7 days ago, marked Stale.
func readThrough[T any](s *Service, ns, key string, refresh, allowStale bool, fetch func() (T, error)) (Result[T], error) {
	var v T
	if !refresh {
		if e, err := s.o.Cache.GetJSON(ns, key, &v); err == nil && e.Hit {
			return Result[T]{Value: v, Meta: ResultMeta{Cached: true, FetchedAt: e.Meta.CreatedAt}}, nil
		}
	}
	fresh, err := fetch()
	if err == nil {
		_ = s.o.Cache.PutJSON(ns, key, fresh) // best effort: the cache never fails a command
		return Result[T]{Value: fresh, Meta: ResultMeta{FetchedAt: s.o.Clock.Now()}}, nil
	}
	var me *mothdex.Error
	if allowStale && errors.As(err, &me) && (me.Kind == mothdex.KindNetwork || me.Kind == mothdex.KindRateLimited) {
		var old T
		if e, gerr := s.o.Cache.GetJSON(ns, key, &old, cache.AllowStale(staleMax)); gerr == nil && e.Hit {
			return Result[T]{Value: old, Meta: ResultMeta{Cached: true, Stale: true, FetchedAt: e.Meta.CreatedAt}}, nil
		}
	}
	return Result[T]{}, wrap(err)
}

// resolve maps a Ref to a slug.
func (s *Service) resolve(r Ref) (string, error) {
	switch {
	case r.Slug != "" && r.Index != 0:
		return "", newError(CodeInvalidRequest, "give an index or a slug, not both")
	case r.Slug != "":
		return r.Slug, nil
	case r.Index == 0:
		return "", newError(CodeInvalidRequest, "an index or a slug is required")
	}
	slug, err := s.session.Resolve(r.Index)
	if err != nil {
		var se *session.Error
		if errors.As(err, &se) {
			return "", &Error{Code: se.Code(), Message: "no such result in the last search", Cause: err}
		}
		return "", wrap(err)
	}
	return slug, nil
}

// Close cancels running downloads and waits for them to settle (the history is written at each
// terminal state, so there is nothing left to flush).
func (s *Service) Close() error {
	s.closeDownloads()
	return nil
}

var _ io.Closer = (*Service)(nil)
