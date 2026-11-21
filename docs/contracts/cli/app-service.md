# Contract: app service (`internal/app`)

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-20) · Risk: Medium

The **only** API the four UIs (CLI, Shell, TUI, GUI) call. Business rules live here or below it;
UIs format, prompt and render, nothing else. UIs may import `pkg/mothdex` **types** (Entry, Link, …) as DTOs,
but never call `mothdex.Client`, the cache, the downloader or the config loader directly.

> Layout note: this adds `internal/app/` to PLAN §2.2. Announced to Guindaste (T-3).

## 1. Interface

Split by capability so each UI and test depends only on what it uses. `Service` composes them.

```go
type Service interface {
    Catalog; Downloads; History; Cache; Config; Profiles
    Close() error // drains or cancels jobs, flushes history
}

type Catalog interface {
    Search(ctx context.Context, q SearchQuery) (SearchPage, error) // saves the session (last results)
    Entry(ctx context.Context, ref Ref) (Result[mothdex.Entry], error)
    Random(ctx context.Context, q RandomQuery) (Result[[]mothdex.Entry], error)
    Platforms(ctx context.Context, f PlatformFilter) (Result[[]mothdex.Platform], error)
    Regions(ctx context.Context) (Result[[]mothdex.Region], error)
    DBInfo(ctx context.Context) (Result[mothdex.DBInfo], error)
    Links(ctx context.Context, ref Ref, sel LinkSelector) ([]LinkChoice, error) // ranked, pure rule after fetch
    Cover(ctx context.Context, ref Ref) (img io.ReadCloser, contentType string, err error) // covers cache; nil img = no cover
}

type Downloads interface {
    Enqueue(ctx context.Context, reqs []DownloadRequest) ([]JobID, error) // validates everything before queueing any
    Jobs() []JobSnapshot
    Events(ctx context.Context) <-chan Event // state changes + throttled progress (≤ 4/s per job)
    Cancel(id JobID) error
    Wait(ctx context.Context) (BatchResult, error) // returns when the queue is empty
}

type History interface {
    List(ctx context.Context, f HistoryFilter) ([]HistoryRecord, error)
}

type Cache interface {
    Stats() (CacheStats, error)
    Clear(ns ...string) error // none = all except `session` and `state`
}

type Config interface {
    Get(key string) (Setting, error)
    List() []Setting // effective value + source (default|file|env|flag)
    Set(key, value string) error   // validates, writes the YAML file atomically
    Reset(key string) error        // key or "all"
}

type Profiles interface { // schema: profiles.md
    Active() ProfileInfo                             // resolved once at startup (flag > config > detection)
    Detect(ctx context.Context) (ProfileInfo, error) // re-run detection, for `profile detect`
}
```

## 2. Key types

```go
type Ref struct { Index int; Slug string } // exactly one set; Index is 1-based over the last search page

type SearchQuery struct {
    Text string; Platforms, Regions []string; Serial string
    Page, PageSize int        // PageSize default from config `interface.page_size`, max 100
    Sort SortKey              // relevance (API order, default) | title | size (client-side, current page only)
    Refresh bool              // `--refresh`: skip cache reads, still write
}
type SearchPage struct {
    Query SearchQuery; Results []IndexedEntry
    Page, TotalPages, TotalResults int // normalized: no results => Page=1, TotalPages=0
    PastLastPage bool                  // API clamped the requested page
    Meta ResultMeta
}
type Result[T any] struct { Value T; Meta ResultMeta }
type ResultMeta struct { Cached, Stale bool; FetchedAt time.Time }

type RandomQuery struct { Filters mothdex.Filters; Count int } // Count 1..20; N calls, dedup by slug, ≤ 2N attempts

type LinkSelector struct {
    Index int        // 1-based, explicit choice (`--link n`); wins over everything
    Prefer Preference // `--prefer` + config `download.prefer`
}
type Preference struct {
    Languages, Formats, Sources []string // matched case-insensitively against extras/format/source_name
    Size SizePref                       // none (default) | smallest | largest
}
type LinkChoice struct { Index int; Link mothdex.Link; Score int; Available bool; Reasons []string }

type DownloadRequest struct {
    Ref Ref; Link LinkSelector
    DestDir string   // "" = config `download.dir` if set, else active profile DestDir(platform) (profiles.md §4)
    Boxart *bool     // nil = config `download.boxart`
}
```

## 3. Rules

- **UI-owned state.** The Shell command history (`<paths.data>/shell_history`) and similar UI conveniences belong to
  the UI that uses them, not to the service.

- **Session.** `Search` saves `{query, page, slugs}` in cache namespace `session`. `Ref{Index}` resolves against it;
  no session or an index out of range gives `KindNotFound` with code `no_session` / `index_out_of_range`.
- **Downloadable links first (T-16 CR-1).** Before ranking, links whose URL fails the downloader URL policy
  (downloader §2: `https`, host in `download.allowed_hosts`, …) are removed from the candidates and returned as
  `LinkChoice{Available: false, Reasons: ["host not allowed: <host>"]}` after the ranked ones. UIs show them as
  unavailable; `--link n` on one of them fails with `untrusted_url`. The downloader re-checks at use anyway.
- **Link ranking** (pure function, tested): keep `type == "Game"` links if any exist, otherwise all. Then sort
  stably and lexicographically by: (1) position of the first preferred language found in `extras` (tokens split on
  `,` and spaces; earlier in the preference list is better; no match sorts last), (2) position of `format` in the
  preferred formats, (3) `source_name` in the preferred sources, (4) `Size` (smallest or largest `size` first; `none` skips
  this step), (5) API order. Region is not a link attribute (it lives on `Entry`), so it stays a search filter (`-r`). With no preference, the first
  `Game` link wins. The UIs show every link with its Reasons; non-interactive commands take the top link.
- **Platform ids for downloads.** The downloader refuses a job whose platform is not in `/platforms`
  (downloader §6.2), so the service passes the cached `meta` list, stale entries included (≤ 7 d). With no list at
  all, `Enqueue` fails with `network` rather than failing every job as `path_rejected`.
- **Stale fallback.** On `KindNetwork` or `KindRateLimited`, catalog reads may serve an expired cache entry
  (≤ 7 days old) with `Meta.Stale = true`. Never for `Random`.
- **429 cooldown** is persisted in namespace `state` (see api-client §4) and restored on start.
- **Errors** leave the service as `*app.Error{Code, Kind, Message, Retryable, Cause}`. `Code` is a stable string
  (cli-output §4) and the CLI maps it to exit codes; other UIs show the localized message for `Code`.
- **Concurrency.** `Service` is safe for concurrent use (TUI and GUI call it from goroutines). Every method
  respects `ctx`; cancelling a UI action cancels the request, not the queue (use `Cancel`).
- **Composition root** is `cmd/mothdex/main.go`: config → logger → i18n → cache → API client → downloader → history → app.
  Tests build the service with fakes for the client and the downloader.

## Resolved questions (Regente, T-2 phase 1)

- OQ-APP-1: Bússola settled it in T-6: `--prefer key=v1,v2` (repeatable) with keys `lang`, `format`, `source`, `size=smallest|largest`. Parsing lives in the CLI layer; an unknown key is a `usage` error (exit 2).
- OQ-APP-2: yes. `download --search <query>` is `Search` plus `Enqueue` of the first hit, implemented in the CLI layer (no new service method).
