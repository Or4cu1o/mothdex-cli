// Package app is the app service called by all four interfaces (app-service.md): business rules
// live here or below; UIs format, prompt and render.
package app

import (
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/cache"
	"github.com/Or4cu1o/mothdex-cli/internal/config"
	"github.com/Or4cu1o/mothdex-cli/internal/history"
	"github.com/Or4cu1o/mothdex-cli/internal/library"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Aliases keep the internal packages out of the signatures UIs program against: a UI imports app
// (and the pkg/mothdex DTOs), never the packages below it.
type (
	// State is a job state.
	State = proxy.State
	// HistoryFilter selects history records (Failed for --failed, Limit).
	HistoryFilter = history.Filter
	// HistoryRecord is one history entry.
	HistoryRecord = history.Record
	// Setting is one effective configuration value with its source.
	Setting = config.Setting
	// ProfileInfo is the resolved frontend profile.
	ProfileInfo = library.ProfileInfo
	// CacheStats reports the cache per namespace.
	CacheStats = cache.Stats
)

// Job states; Done, Skipped, Failed and Canceled are terminal.
const (
	StateQueued       = proxy.StateQueued
	StatePreflight    = proxy.StatePreflight
	StateConnecting   = proxy.StateConnecting
	StateWarming      = proxy.StateWarming
	StateTransferring = proxy.StateTransferring
	StateRetryWait    = proxy.StateRetryWait
	StateVerifying    = proxy.StateVerifying
	StateFinalizing   = proxy.StateFinalizing
	StateDone         = proxy.StateDone
	StateSkipped      = proxy.StateSkipped
	StateFailed       = proxy.StateFailed
	StateCanceled     = proxy.StateCanceled
)

// Ref names one entry: exactly one of Index (1-based over the last search page) or Slug.
type Ref struct {
	Index int
	Slug  string
}

// SortKey orders a search page client-side.
type SortKey string

// Sort keys.
const (
	SortRelevance SortKey = "relevance" // API order
	SortTitle     SortKey = "title"
	SortSize      SortKey = "size"
)

// SearchQuery is a search request.
type SearchQuery struct {
	Text           string
	Platforms      []string
	Regions        []string
	Serial         string
	Page, PageSize int
	Sort           SortKey
	Refresh        bool // skip cache reads, still write
}

// IndexedEntry is a search result with its 1-based index in the session.
type IndexedEntry struct {
	Index int
	mothdex.Entry
}

// SearchPage is a normalized search answer.
type SearchPage struct {
	Query                          SearchQuery
	Results                        []IndexedEntry
	Page, TotalPages, TotalResults int
	PastLastPage                   bool // the API clamped the requested page
	Dropped                        int  // results hidden because their slug fails mothdex.ValidSlug
	Meta                           ResultMeta
}

// ResultMeta says where an answer came from.
type ResultMeta struct {
	Cached, Stale bool
	FetchedAt     time.Time
}

// Result carries a value and its metadata.
type Result[T any] struct {
	Value T
	Meta  ResultMeta
}

// RandomQuery asks for Count random entries (1..20).
type RandomQuery struct {
	Filters mothdex.Filters
	Count   int
}

// PlatformFilter narrows Platforms.
type PlatformFilter struct{ Brand, Class string }

// SizePref is the size tie-break of a Preference.
type SizePref string

// Size preferences.
const (
	SizeNone     SizePref = ""
	SizeSmallest SizePref = "smallest"
	SizeLargest  SizePref = "largest"
)

// Preference ranks links (config download.prefer and --prefer).
type Preference struct {
	Languages, Formats, Sources []string
	Size                        SizePref
}

// LinkSelector picks a link: an explicit 1-based Index wins over every preference.
type LinkSelector struct {
	Index  int
	Prefer Preference
}

// LinkChoice is one link with its rank. Index is the 1-based position in Entry.Links (the value
// of --link). An unavailable link failed the downloader URL policy and comes after the ranked ones.
type LinkChoice struct {
	Index     int
	Link      mothdex.Link
	Score     int
	Available bool
	Reasons   []string
}
