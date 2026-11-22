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
`slug` must match the slug rule below; `MaxResults` 0..100; `Page` ≥ 0; at most 50 platforms or regions.

**Slug rule (both repos).** A slug is `^[a-z0-9][a-z0-9-]{0,255}$`: ASCII lowercase letters, digits and `-`, starting
with a letter or digit, 1 to 256 bytes. It is the only slug shape either client passes on, because slugs end up in
command lines, file names, CSV cells, the session file and terminal output. `pkg/mothdex` exports
`ValidSlug(s string) bool` as the one Go implementation; mothdex-mcp has the same regular expression (`SAFE_SLUG`).
- **Outbound:** `Entry` refuses a slug outside the rule with `KindInvalidRequest` before any request (user input such
  as `--slug`).
- **Inbound:** the client returns API data as answered and does not filter it. The policy for an answer that carries
  an unsafe slug belongs to the consumers, and is the same in both: a **list** answer (search results) drops those
  entries with a warning, and a **single-entry** answer (`/entry`, `/entry/random`) fails with `KindDecode`
  (`api_incompatible`). In mothdex-cli the app service applies it (app-service §3); in mothdex-mcp the tool layer
  (tools.md §1).
- Whether the live catalog has slugs outside the rule is sampled by T-22; a change to the rule is a contract change in
  this file first.
POST bodies are always a JSON object (`{}` when empty). Response bodies are capped at **16 MiB** (`KindDecode` above that).

## 3. Errors

One concrete type; callers use `errors.As` or the `Is*` helpers. Never match on message text outside this package.

```go
type Error struct {
    Kind       Kind          // see the tables below
    Op         string        // "search", "entry", ...
    HTTPStatus int           // 0 for transport errors
    Code       string        // upstream info.error when it is a code (§3.1), "" for the old form and transport errors
    Message    string        // upstream info.message (or info.error in the old form) or transport text; untrusted (§3.3)
    RetryAfter time.Duration // only for KindRateLimited, 0 if unknown
    Err        error         // wrapped cause
}
```

Upstream sends error codes since cavv-dev/mothdex-api `7a87af9` (2026-11-21; API docs "Error codes"): `info.error` is
a machine code and `info.message` the human text, for example
`{"info":{"error":"ENTRY_NOT_FOUND","message":"Entry not found"},"data":{}}`. Application errors such as a missing entry
or a failed validation still come with HTTP 200. Checked live on 2026-11-22: `POST /v1/entry` with an unknown slug
answers HTTP 200 with exactly that body.

### 3.1 Classification order (A6: status, then `info.error`)

Every response goes through these steps, in order. The first one that applies decides.

1. **Transport, redirect and cancellation:** the rows of the last table below, as before.
2. **HTTP status other than 200:** the status decides the kind (table "By HTTP status"). A code in the body is kept
   in `Error.Code` for the log but does not change the kind, with one exception: `400` with code
   `INVALID_API_VERSION` is `KindIncompatible` (our base URL or API version is wrong).
3. **HTTP 200 with `info.error`:**
   - a JSON string matching `^[A-Z][A-Z0-9_]{0,63}$` is a **code**. It is matched **exactly** (case-sensitive, no
     trimming) against the table "By code". A code that is not in the table is `KindAPI`;
   - any other non-empty string is the **old form**, in which `info.error` held the message. It is matched against
     the table "Old form", unchanged from before the codes;
   - `info.error` present but not a non-empty string (number, object, `null`, `""`) is `KindDecode`.
4. **HTTP 200 without `info.error`:** `data` lacks the expected key, or the body is not valid JSON, is `KindDecode`.

By code (HTTP 200):

| Code | Kind | Retried by client |
|---|---|---|
| `ENTRY_NOT_FOUND` | `KindNotFound` | no |
| `INVALID_API_VERSION` | `KindIncompatible` | no |
| `MALFORMED_JSON`, `INVALID_JSON_OBJECT`, `MISSING_REQUIRED_FIELDS`, `INVALID_FIELD_TYPE`, `INVALID_INPUT` | `KindInvalidRequest` | no |
| `DB_OPERATION_ERROR`, `DB_ERROR`, `UNEXPECTED_ERROR` | `KindUpstream` | yes (budget §5) |
| `RATE_LIMIT_ERROR` | `KindRateLimited` | §4, exactly as a 429: `Retry-After` when present, and it counts toward the breach circuit |
| `HTTP_ERROR` | `KindAPI` (upstream sends it with the real HTTP status, so step 2 normally decides; at 200 it is unexplained) | no |
| any other code | `KindAPI` | no |

Old form (HTTP 200, `info.error` is the message; kept for backward compatibility with an older or rolled-back API):

| `info.error` | Kind | Retried by client |
|---|---|---|
| "Entry not found" | `KindNotFound` | no |
| "Slug is required" / "Invalid input provided" | `KindInvalidRequest` | no |
| "Database operation failed" / "A database error occurred" / "An unexpected error occurred" | `KindUpstream` | yes (budget §5) |
| any other string | `KindAPI` | no |

By HTTP status (step 2) and transport (step 1):

| Condition | Kind | Retried by client |
|---|---|---|
| HTTP 400 (any body; with code `INVALID_API_VERSION`: `KindIncompatible`) | `KindInvalidRequest` | no |
| HTTP 404 (route missing: wrong base URL or API version) | `KindIncompatible` | no |
| HTTP 429 | `KindRateLimited` | §4 |
| HTTP 5xx, other unexpected statuses | `KindUpstream` | yes |
| Redirect refused (§5.1): other origin, scheme change, or more than 3 hops | `KindUntrustedRedirect` | no |
| DNS, connect, TLS, reset, timeout | `KindNetwork` | yes |
| `ctx` cancelled or deadline exceeded | `KindCanceled` (wraps `ctx.Err()`) | no |

### 3.2 The code is the key

- Only `Error.Kind` and `Error.Code` drive behavior. Callers outside this package use the kind; `Code` is for logs and
  diagnostics. Nothing outside this package matches message text, and nothing inside matches it except the old-form
  table.
- Both tables (by code, old form) live in one place (`errors.go`), with a parametrized test (§7).
- The `info.code` field proposed in U2 is not part of the API: upstream put the code in `info.error`. A response that
  carries `info.code` is classified as if the field were absent.
- A new upstream code needs no client release to stay safe: it is `KindAPI` until it is added to the table.

### 3.3 The message is untrusted

- `Error.Message` comes from `info.message` when it is a string, else from `info.error` in the old form, else empty.
  For a code with no `info.message`, it is empty.
- The client caps it at **512 bytes** on a UTF-8 boundary and replaces invalid UTF-8. It never parses, matches or
  branches on it in the new form. Validation messages may include details such as field names.
- **Display:** the CLI, Shell, TUI and GUI never show it as the error text. They show their own localized message for
  the kind (cli-output §4). The upstream message is written only to the log, through `app.SanitizeText` (control,
  bidi and escape characters removed), capped at **200 bytes**.
- mothdex-mcp applies the same rule to tool output: see its `tools.md` Errors section.

## 4. Rate limiting and 429 (A5)

Server facts: `30 per 1 second` per IP plus `meta_limits = 15 per hour`; repeated breaches lock the IP out
for the rest of the hour. `app.py` does not enable flask-limiter headers, so **`Retry-After` is probably
absent** (not verified against production).

- Client-side token bucket (`golang.org/x/time/rate`): default **5 req/s, burst 5**; `WithRateLimit` clamps to 1..10 req/s.
- On 429: honor `Retry-After` (seconds or HTTP date) when present, else wait **2 s**; at most **1** automatic retry per call.
  Automatic waits are capped at `maxAutoWait` = **30 s**: a longer `Retry-After` is not slept; the call fails at once
  with `KindRateLimited` and `RetryAfter` set, and the caller decides (accepted deviation 1 from T-10).
- **Breach circuit:** the client counts 429s. On the 3rd within 10 minutes it opens a cooldown of 10 minutes;
  every call fails fast with `KindRateLimited` and `RetryAfter` set, with no network I/O. This keeps one
  process well under the 15/hour meta-limit.
- Cross-process: `WithCooldown(until time.Time)` seeds the circuit, and `WithBreachHook(func(until time.Time))`
  reports when it opens. The app service persists `until` in the `state` cache namespace, so the next CLI
  invocation stays quiet too.

## 5. Retries (reads are idempotent) and transport security

Retryable kinds (`KindUpstream`, `KindNetwork`): up to **2** retries, exponential backoff 500 ms × 2ⁿ with full
jitter, capped at 8 s. Every attempt takes a rate token. Backoff sleeps go through an injected `Sleeper`
(the default uses `time.Timer` and respects `ctx`), so tests never sleep.

TLS (T-16 CR-12): system roots, with `golang.org/x/crypto/x509roots/fallback` embedded for systems without a CA
bundle (some emulation distros). There is **no** option, flag or env var that disables certificate verification.

### 5.1 Redirects (T-30-S2)

The client follows at most **3** redirects (301, 302, 303, 307, 308), and only when the target keeps the same
**origin** as the original request: the same scheme, host (case-insensitive) and effective port. Anything else is
refused **before** a request is sent to the target: another host, a scheme change (`https` → `http` downgrade, or
`http` → `https` on loopback), another port, a missing or unparsable `Location`, or a 4th hop.

A refused redirect is **`KindUntrustedRedirect`** (TS `"untrusted_redirect"`; CLI error code `untrusted_redirect`,
exit 4). It is **not retryable**: the server answered on purpose, so another attempt would get the same redirect. It
does not open the 429 circuit and is never classified as `KindNetwork`. In Go, `CheckRedirect` returns a sentinel
(`errRedirectRefused`) that the classifier checks with `errors.Is` **before** the transport-error branch, because
`net/http` wraps it in a `*url.Error`. `Message` names the refused target host only (not the full URL). A 303, or
a 301/302 after a POST, follows as a body-less `GET` with no `Content-Type`.

## 6. Options (defaults)

| Option | Default | Notes |
|---|---|---|
| `WithBaseURL(string)` | `https://api.mothdex.net/v1` | `https` required; `http` only for loopback **IP literals** (`127.0.0.0/8`, `::1`; tests), never names such as `localhost`, which can be repointed. Rule: `ValidateBaseURL` |
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

Error codes (T-77), one parametrized classification test over:
- each of the 12 codes at HTTP 200;
- the old form (the existing recorded fixtures);
- an unknown code, a lower-case or over-long code-like string (old form);
- a non-200 status with a code, including `400` with `INVALID_API_VERSION`;
- `RATE_LIMIT_ERROR` at 200, which must open the circuit like a 429;
- `info.error` as a number, `null` and `""`;
- `info.code` present.
New fixtures are built from the documented shape, so no live request is needed. A message over 512 bytes and one with
control characters check the cap and the log sanitization.

## Resolved questions (Regente, T-2 phase 1)

- OQ-API-1: the T-10 fixture-recording script records whether `Retry-After` is present; the policy above works either way.
- OQ-API-2: resolved by upstream `7a87af9` (T-77): `info.error` carries a stable code and `info.message` the text (§3.1-§3.3). The old-form message table stays for backward compatibility.
