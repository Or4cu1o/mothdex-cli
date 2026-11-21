# Contract: app service (`internal/app`)

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-20) · Risk: Medium

The **only** API the four UIs (CLI, Shell, TUI, GUI) call. Business rules live here or below it;
UIs format, prompt and render, nothing else. UIs may import `pkg/mothdex` **types** (Entry, Link, …) as DTOs,
but never call `mothdex.Client`, the cache, the downloader or the config loader directly. Every type in the
service's signatures is declared in `app` (aliases of internal types are fine: `HistoryRecord = history.Record`,
`State = proxy.State`), so UI packages import only `app` and `pkg/mothdex` (plus the UI infrastructure `internal/i18n`
and `internal/buildinfo`); only the composition root imports the other internals. File exports go through `app`
(`ExportTarget`, `WriteExport`, `ExportHistory`, `CSVCell`, formats `FormatTXT|JSON|CSV`), never `internal/history`.

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
    SaveBoxart(ctx context.Context, ref Ref, destDir string) (path string, err error)       // standalone `boxart`: <title>.<ext>
}

type Downloads interface {
    Enqueue(ctx context.Context, reqs []DownloadRequest) ([]JobID, error) // validates everything before queueing any
    Jobs() []JobSnapshot
    Events(ctx context.Context) <-chan Event // state changes + throttled progress (≤ 4/s per job); never lossy
    Cancel(id JobID) error
    Wait(ctx context.Context) (BatchResult, error) // returns when the queue is empty
    SaveJobBoxart(ctx context.Context, job JobSnapshot, destDir string) (path string, err error) // cover of a Done job, named <stem of its file>.<ext>
}

// Method names carry the capability (one type implements all, so `List`/`Get` cannot repeat; T-20a).
type History interface {
    History(ctx context.Context, f HistoryFilter) ([]HistoryRecord, error)
}

type Cache interface {
    CacheStats() (CacheStats, error)
    CacheClear(ns ...string) error // none = all except `session` and `state`
}

type Config interface {
    ConfigGet(key string) (Setting, error)
    ConfigList() ([]Setting, error) // effective value + source (default|file|env|flag)
    ConfigSchema() []SettingSpec    // per key: Kind (string|enum|int|float|bool|duration|size|list), Enum, Min, Max, Env; no file read
    ConfigSet(key, value string) error   // validates, writes the YAML file atomically
    ConfigReset(key string) error        // key or "all"
}

type Profiles interface { // schema: profiles.md
    ActiveProfile() ProfileInfo                             // resolved once at startup (flag > config > detection)
    DetectProfile(ctx context.Context) (ProfileInfo, error) // re-run detection, for `profile detect`
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
    Dropped int                        // results hidden by the slug rule (§3)
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
}
```

## 3. Rules

- **UI-owned state.** The Shell command history (`<paths.data>/shell_history`) and similar UI conveniences belong to
  the UI that uses them, not to the service.

- **Unsafe slugs (api-client §2, slug rule).** `Search` drops results whose slug fails `mothdex.ValidSlug`
  **before** numbering and before the session is saved, so `<index>` always matches what was shown and the session
  only holds valid slugs. `SearchPage.Dropped` (int, additive) counts them, and the CLI adds the warning
  `"{n} results were hidden because their slug has an unexpected form."` to `warnings[]` (and stderr). `Entry`
  returns `api_incompatible` when the answered slug fails the rule. `Random` counts such an answer as a failed
  attempt (within its 2N budget), and returns `api_incompatible` only when no valid entry was found.
- **Session.** `Search` saves `{query, page, slugs}` in cache namespace `session`. `Ref{Index}` resolves against it;
  no session or an index out of range gives `KindNotFound` with code `no_session` / `index_out_of_range`.
  The session is a convenience: a failed save does not fail `Search`, but the service then deletes the stored
  session (best effort), so a later `Ref{Index}` gets `no_session` and never resolves against an older search.
- **Event delivery.** Each `Events` subscriber has its own queue, so a slow reader never blocks a transfer and never
  loses a state change: only progress ticks of the same job in the same state coalesce into the latest one. The
  channel closes when `ctx` ends or the service closes; events still queued for a reader at that moment are not
  delivered. Events after a job's terminal state are dropped. `Jobs` and `Wait` remain the authoritative final picture.
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
- **Downloaders.** The service builds one downloader per destination root (downloader §6.2 confines each to its
  root). `download.concurrency`, the space ledger and the batch name set therefore apply per root. The CLI uses one
  root per batch; long-lived UIs (E-M3) that mix roots need a shared limiter first.
- **Boxart** (`SaveBoxart`, `SaveJobBoxart`): the service, not the UI, writes covers, so every UI shares the rule. The
  cover comes from the covers cache (validated fetcher, cache §4); the extension comes from the stored content type
  (png, jpg, gif, webp); the name goes through `proxy.Sanitize`. `SaveJobBoxart` names it `<stem of the final file
  name>.<ext>` in the file's folder (a frontend pairs them by stem); `SaveBoxart` (the standalone command) names it
  `<title>.<ext>` in the destination `Enqueue` would use. The write goes through the confined destination root: staged
  and published without replacing (O_EXCL, link or copy), an existing file is kept and its path returned, launcher
  folders are refused (`path_rejected`). An entry without a usable cover is `not_found`. Whether to call it after a
  download (`download.boxart`, `--boxart`, `Done` jobs only, never after an interrupt) is the UI's choice; a failure is a warning and never fails the job.
- **Export** (`ExportTarget`, `WriteExport`, `ExportHistory`, `CSVCell`): the txt/json/csv writers of cli-output §5 live
  in the service so Shell and TUI share one path. A write failure is `exists`, `path_rejected`, `permission_denied` or
  `disk_full` (exit 6) with the target path, never the temporary name.
- **Stale fallback.** On `KindNetwork` or `KindRateLimited`, catalog reads may serve an expired cache entry
  (≤ 7 days old) with `Meta.Stale = true`. Never for `Random`.
- **429 cooldown** is persisted in namespace `state` (see api-client §4) and restored on start.
- **Errors** leave the service as `*app.Error{Code, Kind, Message, Retryable, Cause}`. `Code` is a stable string
  (cli-output §4) and the CLI maps it to exit codes; other UIs show the localized message for `Code`.
- **Concurrency.** `Service` is safe for concurrent use (TUI and GUI call it from goroutines). Every method
  respects `ctx`; cancelling a UI action cancels the request, not the queue (use `Cancel`).
- **Composition root** is `cmd/mothdex/main.go`: config → logger → i18n → cache → API client → downloader → history → app.
  Tests build the service with fakes for the client and the downloader.
- **Cover decoding (gui.md section 8, G2).** `app.DecodeCover(r io.Reader, contentType string, maxSide int) (image.Image, error)`
  decodes a cover that `Cover` returned. It applies the limits of the fetch again (at most `cover.MaxBytes` of input, a type
  from the allowlist that agrees with `contentType`, at most `cover.MaxPixelSide` per side read from the header before any
  pixel is decoded; a GIF's first frame size is checked too) and a pixel budget of `cover.MaxDecodePixels` (4 megapixels,
  also from the header), because the side cap alone still allows 64 to 190 MiB per decode on a handheld. It decodes one
  frame (the first of an animated GIF; PNG, JPEG, GIF and still WebP; an animated WebP is refused) and scales it down to
  `maxSide` on the longer side when `maxSide > 0` (never up, straight from the decoded image). A decoder panic is an error.
  A refusal is an `*Error` (`not_found`) whose Cause is `cover.ErrDecode`. Front ends decode one cover at a time.

## Resolved questions (Regente, T-2 phase 1)

- OQ-APP-1: Bússola settled it in T-6: `--prefer key=v1,v2` (repeatable) with keys `lang`, `format`, `source`, `size=smallest|largest`. Parsing lives in the CLI layer; an unknown key is a `usage` error (exit 2).
- OQ-APP-2: yes. `download --search <query>` is `Search` plus `Enqueue` of the first hit, implemented in the CLI layer (no new service method).
