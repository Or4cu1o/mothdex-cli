# Contract: cache (`internal/cache`)

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-12) · Risk: Medium

Its purpose is to spare the rate-limited API (A5) and keep repeat navigation instant. It is a cache: losing
it never loses user data. History is **not** stored here (it lives in `paths.data`).

## 1. Namespaces

Root: `<paths.temp>/cache/<ns>/`.

| Namespace | Content | Kind | Default TTL | Evictable |
|---|---|---|---|---|
| `search` | `/search` responses | JSON | 24 h | yes |
| `entry` | `/entry` responses | JSON | 24 h | yes |
| `meta` | `/platforms`, `/regions` | JSON | 7 d | yes |
| `info` | `/info` | JSON | 1 h | yes |
| `covers` | cover images | binary | 30 d | yes |
| `session` | last search (for `<index>`) | JSON | none | no |
| `state` | 429 cooldown and similar small flags | JSON | per item | no |

`/entry/random` is never cached. Unknown namespaces are a programming error (panic in tests, error at runtime).
TTL overrides: `cache.ttl.<ns>` (config.md). TTL `0` disables caching for that namespace.

## 2. Keys and layout

- Key = lowercase hex SHA-256 of a **canonical** request: JSON with sorted keys, trimmed and lowercased search text,
  platform and region lists lowercased, de-duplicated and sorted, defaults made explicit (`page:1`, `max_results:N`),
  plus the API base URL. The canonicalization is a pure function with a parametrized test.
- Files: `<key>.json` or `<key>.bin`, plus the sidecar `<key>.meta.json`:
  `{"v":1,"ns":"search","created_at":"RFC3339","expires_at":"RFC3339|null","size":123,"content_type":"image/jpeg"}`.
- Last access = data file **mtime**, refreshed with `os.Chtimes` on a hit (at most once per minute per item).
- Modes (T-16 CR-11): cache directories `0700`, files `0600` on POSIX systems. Cached API data is not trusted more
  than network data: consumers (downloader, profiles) validate it at use.
- Writes are atomic: temp file in the same directory, then rename (data first, sidecar last). A missing or corrupt
  sidecar or data file is a miss, and both files are deleted.

## 3. Interface

```go
type Clock interface{ Now() time.Time }

type Store interface {
    GetJSON(ns, key string, v any, opts ...GetOption) (Entry, error)   // Entry{Hit, Stale bool; Meta}
    PutJSON(ns, key string, v any, opts ...PutOption) error
    GetBlob(ns, key string, opts ...GetOption) (io.ReadCloser, Entry, error)
    PutBlob(ns, key string, r io.Reader, contentType string, maxBytes int64, opts ...PutOption) error
    Delete(ns, key string) error
    Clear(ns ...string) error                                 // none = all evictable namespaces
    Stats() (Stats, error)                                    // per ns: items, bytes, expired
    Prune() error                                             // remove expired, then enforce the cap
}

func Open(root string, cfg Config, clock Clock, log *slog.Logger) (Store, error)

func AllowStale(maxAge time.Duration) GetOption // read option
func WithTTL(d time.Duration) PutOption         // write option
```

Options are variadic, so a plain call takes the defaults (T-12, Crivo suggestion 5):

- **`AllowStale(maxAge)`** (`GetJSON`, `GetBlob`): an expired item is returned with `Entry.Stale = true` if it expired
  no more than `maxAge` ago; older items, and every expired item without the option, are a miss. The app service uses
  it for the stale fallback (≤ 7 d). `maxAge ≤ 0` means no stale reads.
- **`WithTTL(d)`** (`PutJSON`, `PutBlob`): sets the lifetime of that one item; `d ≤ 0` is ignored.
  - `session` and `state` have no expiry by default, and `WithTTL` is the only way to give an item one (for
    example the 429 cooldown in `state`).
  - In evictable namespaces it overrides the namespace TTL for that item. When the namespace TTL is `0`, or the
    cache is disabled, the namespace is off: nothing is stored, even with `WithTTL`.

## 4. Size cap and eviction

- Global cap `cache.max_size_mb` (default **200**) over evictable namespaces; `session` and `state` are tiny and exempt.
- Total size is computed by one directory scan on `Open`, then kept in memory.
- On `Put`, when the total exceeds the cap: delete expired items first, then least-recently-used items until the total
  is ≤ 90 % of the cap. A single item larger than 25 % of the cap is not stored.
- **Cover fetch** (T-16 CR-9) is an untrusted-URL path with its own policy: `https` only; the dialer `Control` hook
  rejects loopback, private, link-local, CGNAT, multicast and unspecified IPs **after** DNS resolution; ≤ 3 redirects,
  each re-checked; body ≤ 5 MiB; `Content-Type` **and** magic bytes must agree on `jpeg|png|webp|gif` (SVG and
  anything else are rejected); `image.DecodeConfig` must report ≤ 4096 × 4096 px before any full decode. The blob is
  only stored in the cache, never written elsewhere. Any violation is a cache miss with a debug log, never an error.
  The policy lives in `internal/cover` (`Fetcher.Fetch(ctx, url) bool`), which owns its DNS port (`cover.Resolver`,
  production adapter `cover.SystemResolver`); the downloader has no resolver. `Service.Cover` (app-service §1) reads
  `GetBlob(covers, KeyCover(cover_url))`, and on a miss calls `Fetch` once and reads again, outside the download pool.
  It returns the cached reader and the stored type (taken from the magic bytes), or a nil reader when the entry has no
  usable cover; only the entry lookup can return an error. Concurrent calls for one URL share a single fetch, and a
  refused URL is not fetched again in the same process. Consumers decode one frame only.

## 5. Concurrency

Several processes (for example CLI and TUI) may share the cache. There are no locks: atomic renames keep files whole,
and the worst outcome is a duplicate fetch or a slightly wrong in-memory size, corrected by the next `Prune`.
Inside a process, the `Store` is safe for concurrent use.

**Windows rename retry (best effort).** Windows refuses to replace a file that another handle (a reader) has open,
and `os.Rename` fails with "access denied" for a moment. On Windows only, the replace is retried up to 8 times with a
linearly growing pause (5 ms, 10 ms, …; well under 200 ms in total). If it still fails, the `Put` returns the error,
which callers treat as non-fatal: the cache is best effort and the next fetch rewrites the item. Other OSes do not
retry.

## 6. Switches

`cache.enabled: false`: reads always miss and nothing is written, except `session` and `state`, which still work
because `<index>` and the 429 cooldown depend on them. `--refresh` skips reads for one command but still writes fresh results.

## 7. Tests (T-12)

All with an injected clock (no `sleep`): TTL expiry at the boundary, stale read, LRU order, cap at 90 %, oversized
item, corrupt sidecar, canonical key equivalence (parametrized).

## Resolved questions (Regente, T-2 phase 1)

- OQ-CACHE-1: `search` TTL is 24 h; `--refresh` bypasses the cache when the user wants fresh results.
