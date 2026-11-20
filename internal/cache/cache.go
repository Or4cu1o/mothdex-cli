// Package cache is the on-disk cache that spares the rate-limited API and keeps repeat
// navigation instant (docs/contracts/cli/cache.md). It is only a cache: losing it never
// loses user data. Several processes may share one root; there are no locks, only atomic
// renames, so the worst outcome is a duplicate fetch or a slightly wrong in-memory size that
// the next Prune corrects.
package cache

import (
	"errors"
	"io"
	"time"
)

// Namespaces of the cache. Evictable ones count toward the size cap.
const (
	NSSearch  = "search"
	NSEntry   = "entry"
	NSMeta    = "meta"
	NSInfo    = "info"
	NSCovers  = "covers"
	NSSession = "session" // last search, so <index> works; never evicted
	NSState   = "state"   // 429 cooldown and similar small flags; never evicted
)

// Errors. A failed Put is never fatal for callers: the cache is best effort.
var (
	ErrUnknownNamespace = errors.New("cache: unknown namespace")
	ErrInvalidKey       = errors.New("cache: invalid key")
	ErrWrongKind        = errors.New("cache: namespace holds a different kind of data")
	ErrTooLarge         = errors.New("cache: item too large to store")
)

// Clock supplies the time; tests inject a fake.
type Clock interface{ Now() time.Time }

// TTL holds the lifetime per namespace; zero disables caching for that namespace.
type TTL struct {
	Search, Entry, Meta, Info, Covers time.Duration
}

// Config is the cache configuration, mapped from cache.* settings by the app service.
type Config struct {
	Enabled  bool  // false: evictable namespaces always miss and are never written
	MaxBytes int64 // size cap over evictable namespaces; <= 0 selects DefaultMaxBytes
	TTL      TTL
}

// DefaultMaxBytes is the default 200 MB cap.
const DefaultMaxBytes = 200 << 20

// Meta is the sidecar stored next to every item.
type Meta struct {
	V           int        `json:"v"`
	NS          string     `json:"ns"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Size        int64      `json:"size"`
	ContentType string     `json:"content_type,omitempty"`
}

// Entry describes the outcome of a read. A miss is Entry{} with a nil error.
type Entry struct {
	Hit   bool
	Stale bool // expired but served under AllowStale
	Meta  Meta
}

// NSStats is the per-namespace row of Stats.
type NSStats struct {
	Name    string
	Items   int
	Bytes   int64
	Expired int // expired or corrupt (both are removed by Prune)
}

// Stats is the result of Store.Stats.
type Stats struct {
	Namespaces []NSStats
	TotalBytes int64 // evictable namespaces only
	MaxBytes   int64
}

// GetOption tunes a read.
type GetOption func(*getOpts)

type getOpts struct{ staleFor time.Duration }

// AllowStale lets a read return an expired item that expired no more than maxAge ago.
func AllowStale(maxAge time.Duration) GetOption { return func(o *getOpts) { o.staleFor = maxAge } }

// PutOption tunes a write.
type PutOption func(*putOpts)

type putOpts struct{ ttl time.Duration }

// WithTTL sets the lifetime of one item (for example a state flag); d <= 0 is ignored.
func WithTTL(d time.Duration) PutOption { return func(o *putOpts) { o.ttl = d } }

// Store is the cache interface. Implementations are safe for concurrent use.
type Store interface {
	GetJSON(ns, key string, v any, opts ...GetOption) (Entry, error)
	PutJSON(ns, key string, v any, opts ...PutOption) error
	GetBlob(ns, key string, opts ...GetOption) (io.ReadCloser, Entry, error)
	PutBlob(ns, key string, r io.Reader, contentType string, maxBytes int64, opts ...PutOption) error
	Delete(ns, key string) error
	Clear(ns ...string) error // none = every evictable namespace
	Stats() (Stats, error)
	Prune() error // remove expired items, then enforce the cap
}
