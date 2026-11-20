# Contract: `pkg/mothdex` (Mothdex API v1 client)

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-10) · Risk: Medium
Sources: [`mothdex-api`](https://github.com/cavv-dev/mothdex-api) `API_DOCUMENTATION.md`, `MIGRATING.md`, `app.py`, `api.py` · Covers findings A5, A6, A7.

Public, reusable Go package. It knows nothing about cache, config files, UI or downloads.
The MCP TypeScript client (T-30) follows the same rules; the shared contract is the fixture set in
`pkg/mothdex/testdata/api-v1/`.

## 1. Types

JSON tags mirror API v1 exactly (snake_case). Unknown fields are ignored (forward compatible).
Nullable strings (`serial`, `cover_url`) decode to `""`; `regions`/`extras`/`links` decode `null` to an empty slice.

```go
type Entry struct {
    Slug     string   `json:"slug"`
    Title    string   `json:"title"`
    Platform string   `json:"platform"`
    Regions  []string `json:"regions"`
    Serial   string   `json:"serial"`    // "" when null
    CoverURL string   `json:"cover_url"` // "" when null; external URL, untrusted
    Links    []Link   `json:"links"`
}

type Link struct {
    Type       string   `json:"type"`     // e.g. "Game"
    Extras     []string `json:"extras"`   // languages, revisions, tags (free text)
    Format     string   `json:"format"`   // e.g. "gba", "bin+cue"
    Filename   string   `json:"filename"` // external input: NEVER used as a path without sanitization
    URL        string   `json:"url"`      // proxy URL; host checked by the downloader, not here
    Size       int64    `json:"size"`     // bytes; authoritative expected size
    SizeStr    string   `json:"size_str"`
    SourceName string   `json:"source_name"`
    SourceURL  string   `json:"source_url"`
    Hashes     *Hashes  `json:"hashes,omitempty"` // FUTURE (PLAN §8 U2); nil today
}

// Hashes is the proposed optional upstream field. Hex strings are lowercased on decode.
// A Hashes value with an unknown Scope is discarded (treated as nil) and logged at debug.
type Hashes struct {
    CRC32 string    `json:"crc32,omitempty"`
    MD5   string    `json:"md5,omitempty"`
    SHA1  string    `json:"sha1,omitempty"`
    Scope HashScope `json:"scope"` // "file" (the downloaded file) | "content" (the ROM inside the archive)
}

type Platform struct { ID, Name, Brand, Class string } // ID filled from the map key
type Region   struct { ID, Name string }
type DBInfo   struct { TotalEntries int64 `json:"total_entries"` }

type Filters struct {
    SearchKey string   `json:"search_key,omitempty"`
    Platforms []string `json:"platforms,omitempty"`
    Regions   []string `json:"regions,omitempty"`
    Serial    string   `json:"serial,omitempty"` // exact match
}

type SearchRequest struct {
    Filters
    MaxResults int `json:"max_results,omitempty"` // 0 = API default (100); valid 1..100
    Page       int `json:"page,omitempty"`        // 0 = 1
}

type SearchResponse struct {
    Results        []Entry `json:"results"`
    CurrentResults int     `json:"current_results"`
    TotalResults   int     `json:"total_results"`
    CurrentPage    int     `json:"current_page"`
    TotalPages     int     `json:"total_pages"`
}
```

`Platforms()` and `Regions()` return slices sorted by `ID` (Go maps have no order). The API may return
`current_page: 0, total_pages: 0` when nothing matches, and silently clamps `page` past the last one
(`api.py:get_search`). The client returns these values **raw**; the app service normalizes them.

## 2. Methods

```go
func New(opts ...Option) (*Client, error) // safe for concurrent use

func (c *Client) Search(ctx context.Context, r SearchRequest) (SearchResponse, error) // POST /search
func (c *Client) Entry(ctx context.Context, slug string) (Entry, error)              // POST /entry
func (c *Client) Random(ctx context.Context, f Filters) (Entry, error)               // POST /entry/random (POST only, A7)
func (c *Client) Platforms(ctx context.Context) ([]Platform, error)                  // GET /platforms
func (c *Client) Regions(ctx context.Context) ([]Region, error)                      // GET /regions
func (c *Client) Info(ctx context.Context) (DBInfo, error)                           // GET /info
```

Local validation runs before any request and returns `KindInvalidRequest` without spending a token:
`slug` must be non-empty and ≤ 256 bytes; `MaxResults` 0..100; `Page` ≥ 0; at most 50 platforms or regions.
POST bodies are always a JSON object (`{}` when empty). Response bodies are capped at **16 MiB** (`KindDecode` above that).

## 3. Errors

One concrete type; callers use `errors.As` or the `Is*` helpers. Never match on message text outside this package.

```go
type Error struct {
    Kind       Kind          // see table
    Op         string        // "search", "entry", ...
    HTTPStatus int           // 0 for transport errors
    Message    string        // info.error or transport text; untrusted, for logs/UI only
    RetryAfter time.Duration // only for KindRateLimited, 0 if unknown
    Err        error         // wrapped cause
}
```

| Condition | Kind | Retried by client |
|---|---|---|
| HTTP 200 + `info.error == "Entry not found"` | `KindNotFound` | no |
| HTTP 200 + `info.error` "Slug is required" / "Invalid input provided" | `KindInvalidRequest` | no |
| HTTP 200 + `info.error` "Database operation failed" / "A database error occurred" / "An unexpected error occurred" | `KindUpstream` | yes (budget §5) |
| HTTP 200 + any other `info.error` | `KindAPI` | no |
| HTTP 200, no `info.error`, but `data` lacks the expected key, or invalid JSON | `KindDecode` | no |
| HTTP 400 (any body) | `KindInvalidRequest` | no |
| HTTP 404 (route missing: wrong base URL or API version) | `KindIncompatible` | no |
| HTTP 429 | `KindRateLimited` | §4 |
| HTTP 5xx, other unexpected statuses | `KindUpstream` | yes |
| DNS, connect, TLS, reset, timeout | `KindNetwork` | yes |
| `ctx` cancelled or deadline exceeded | `KindCanceled` (wraps `ctx.Err()`) | no |

The known-message table lives in one place (`errors.go`) and has a parametrized test against the fixtures.
A6 rule: **every** response checks the HTTP status **and** `info.error`, in that order.
Forward compatibility: if `info.code` appears (requested upstream in proposal U2), it takes
precedence over message matching; unknown codes map to `KindAPI`.

## 4. Rate limiting and 429 (A5)

Server facts: `30 per 1 second` per IP plus `meta_limits = 15 per hour`; repeated breaches lock the IP out
for the rest of the hour. `app.py` does not enable flask-limiter headers, so **`Retry-After` is probably
absent** (not verified against production).

- Client-side token bucket (`golang.org/x/time/rate`): default **5 req/s, burst 5**; `WithRateLimit` clamps to 1..10 req/s.
- On 429: honor `Retry-After` (seconds or HTTP date) when present, else wait **2 s**; at most **1** automatic retry per call.
- **Breach circuit:** the client counts 429s. On the 3rd within 10 minutes it opens a cooldown of 10 minutes;
  every call fails fast with `KindRateLimited` and `RetryAfter` set, with no network I/O. This keeps one
  process well under the 15/hour meta-limit.
- Cross-process: `WithCooldown(until time.Time)` seeds the circuit, and `WithBreachHook(func(until time.Time))`
  reports when it opens. The app service persists `until` in the `state` cache namespace, so the next CLI
  invocation stays quiet too.

## 5. Retries (reads are idempotent)

Retryable kinds (`KindUpstream`, `KindNetwork`): up to **2** retries, exponential backoff 500 ms × 2ⁿ with full
jitter, capped at 8 s. Every attempt takes a rate token. Backoff sleeps go through an injected `Sleeper`
(the default uses `time.Timer` and respects `ctx`), so tests never sleep.

## 6. Options (defaults)

| Option | Default | Notes |
|---|---|---|
| `WithBaseURL(string)` | `https://api.mothdex.net/v1` | `https` required; `http` only for loopback hosts (tests) |
| `WithHTTPClient(*http.Client)` | own client, TLS ≥ 1.2, max 3 redirects within the same host | |
| `WithTimeout(time.Duration)` | 15 s per attempt | |
| `WithRateLimit(rps float64, burst int)` | 5, 5 | clamped 1..10 |
| `WithMaxRetries(int)` | 2 | 0..5 |
| `WithUserAgent(string)` | `mothdex-cli/<version> (+https://github.com/Or4cu1o/mothdex-cli)` | generic; never user data |
| `WithLogger(*slog.Logger)` | discard | logs method, path, status, latency; never bodies |
| `WithSleeper(Sleeper)`, `WithClock(Clock)` | real | test injection |
| `WithCooldown`, `WithBreachHook` | none | §4 |

## 7. Tests (T-10)

Contract tests with `httptest` and the recorded fixtures (search, empty search, entry, entry-not-found with 200,
random, platforms, regions, info, 400, 429, 500, DB error with 200). Pure-rule tests: error classification
(parametrized) and the retry/circuit decision with a fake clock.

## Resolved questions (Regente, T-2 phase 1)

- OQ-API-1: the T-10 fixture-recording script records whether `Retry-After` is present; the policy above works either way.
- OQ-API-2: the U2 upstream proposal (T-8) will also ask for stable error codes; until then the message table in `errors.go` is the only place that matches text.
