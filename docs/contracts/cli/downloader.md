# Contract: downloader (`internal/proxy`)

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-17) · Risk: **High** (writes files named by the server)
Sources: [`mothdex-proxy/src/main.cpp`](https://github.com/cavv-dev/mothdex-proxy/blob/main/src/main.cpp) (`streamFile`, `getFileDetails`, `pieceWaitTimeout`) · Covers A1–A4. See [ADR-0002](../../adr/ADR-0002-downloader-without-range.md).

Sentinela's threat model (T-16) must be accepted before T-17 starts; it may tighten §6 (paths) and §2 (hosts).

## 1. Proxy facts the design depends on

| Fact (main.cpp) | Consequence |
|---|---|
| `Range` is ignored; it always answers `200` with the whole file and `Content-Length` | no resume, no segments; every retry starts from byte 0 |
| headers are written **before** the first piece is available | "warming" = headers received, 0 body bytes |
| each piece may wait up to `pieceWaitTimeout` = 15 min; on timeout the stream just ends | short stream: detect `bytes < expected` and retry |
| `404 "not found"` both for unknown names and when `makeRoom` cannot free disk | 404 is **possibly transient** |
| `Content-Disposition` filename comes from the torrent | ignored; the name comes from `link.filename`, sanitized |

## 2. Request

`GET link.url`, no `Range`, `Accept-Encoding: identity`, same User-Agent as the API client.
Before connecting: the URL must be `https` and its host must be in `download.allowed_hosts`
(default `["proxy.mothdex.net"]`). Redirects: at most 3, each one re-checked against the allowlist.
Violation: `Failed(untrusted_url)`, not retryable.

## 3. State machine

```
Queued ─▶ Preflight ─▶ Connecting ─▶ Warming ─▶ Transferring ─▶ Verifying ─▶ Finalizing ─▶ Done
              │             │           │            │              │             │
              ▼             └───────────┴─────┬──────┴──────────────┘             │
          Skipped / Failed                    ▼                                   ▼
                                       RetryWait ─▶ Connecting           Failed (no retry)
any state ── Cancel / ctx ─▶ Canceled (.part removed)
```

| State | Entered when | Leaves to |
|---|---|---|
| Queued | accepted by `Enqueue` | Preflight when a worker is free |
| Preflight | sanitize name, resolve destination, check host, check free space ≥ `size × 1.05`, apply `on_exists` | Connecting · Skipped · Failed |
| Connecting | request sent; `connect_timeout` 30 s, `header_timeout` 60 s | Warming · RetryWait · Failed |
| Warming | `200` + headers received, 0 body bytes; `first_byte_timeout` (default 10 m) | Transferring · RetryWait |
| Transferring | ≥ 1 byte; streaming to `.part` with SHA-256 computed on the fly; `idle_timeout` (default 5 m) between reads | Verifying · RetryWait · Failed |
| RetryWait | retryable failure, budget left (§5) | Connecting (`.part` truncated to 0) · Failed |
| Verifying | EOF with `bytes == expected` | Finalizing · RetryWait (one integrity retry) · Failed |
| Finalizing | atomic move (§6) and history record | Done · Failed |

Expected size = `link.size`. If `Content-Length` is present and differs from `link.size`: `Failed(size_mismatch)`,
not retryable (the catalog and the proxy disagree, so re-downloading cannot fix it). Missing `Content-Length`:
proceed using `link.size`. More bytes than expected: abort immediately, `Failed(size_mismatch)`.

## 4. Verification (A4)

Order, all on the `.part`:
1. **Size**: `bytes == link.size` (always).
2. **Zip CRC**: if the first bytes are `PK\x03\x04` (magic bytes, not the extension), read every member to EOF with
   `archive/zip`, which checks CRC32. Bomb guard: stop if uncompressed bytes exceed `100 × size` or 64 GiB.
   7z/rar: not checked (backlog), and the history records `crc: "skipped"`.
3. **Upstream hashes** (future, `link.hashes != nil`): `scope=file` compares against the `.part`;
   `scope=content` compares against the single archive member, or the file itself if it is not an archive.
   Strongest available algorithm: sha1 > md5 > crc32.
4. **Local SHA-256** of the file as downloaded, stored in history (proof of what was received, not of correctness).

Config `download.verify: false` skips only steps 2 and 3; size is always checked.
An integrity failure gets **one** re-download; a second one ends in `Failed(integrity)`.

## 5. Retry policy

| Failure | Retryable | Budget |
|---|---|---|
| network error, reset, TLS handshake, timeout (connect/header/first-byte/idle) | yes | `max_attempts` total (default 5) |
| truncated stream (`EOF` with `bytes < expected`) | yes | same |
| `5xx`, `429` | yes; `Retry-After` honored when larger | same |
| `404` | yes (A3) | separate: `not_found_retries` (default 3) with fixed delays 30 s, 60 s, 120 s |
| other `4xx`, `size_mismatch`, `untrusted_url`, disk full, permission | no | — |

Backoff: full jitter, `sleep = rand(0, min(60 s, 2 s × 2^(attempt-1)))`, through an injected `Sleeper` and `Clock`.
Each attempt is recorded (state, cause, bytes) for history.

## 6. Files and paths

- `.part` path: `<paths.temp>/downloads/<jobID>.part` (no server-derived text in the temp name).
- Final name: `Sanitize(link.filename)`; this rule is owned by T-16. Minimum: base name only, no `..`, no separators,
  no control characters, no Windows reserved names or characters, ≤ 200 bytes UTF-8, never empty
  (fallback `<slug>.<format>`).
- The destination must resolve (after `EvalSymlinks` on the directory) inside the configured root. The final file is
  opened with `O_EXCL` semantics, so it never follows a pre-existing symlink.
- Atomic move: `os.Rename(.part, final)`; on `EXDEV` (different filesystem), copy to `<destdir>/.<jobID>.mothdex-tmp`,
  `fsync`, then rename within `destdir`. A visible final file is always complete.
- `download.on_exists`: `skip` (default; state `Skipped`), `rename` (`name (1).ext`), `overwrite` (only via `--force`).
- At startup, `.part` and `.mothdex-tmp` files older than 24 h are deleted.

## 7. Pool and events

- Worker pool: `download.concurrency` default **2**, clamped 1..6. Parallelism is across files only (A1).
- Events: `{JobID, State, Attempt, Bytes, Expected, RateBps, Err}`; progress events are throttled to 4/s.
- Boxart (`cover_url`) is fetched through the `covers` cache, outside the pool; a failure never fails the job.
- Every job reaches exactly one terminal state (`Done`, `Skipped`, `Failed`, `Canceled`) and writes one history record.

## 8. Test hooks (T-18)

`Downloader` takes `HTTPDoer`, `Clock`, `Sleeper`, `FS` root and `Rand` interfaces. The fake proxy covers: stall before
the first byte, stall mid-stream, truncation, transient 404, 429, `Content-Length` mismatch, extra bytes,
malicious filenames and redirect off the allowlist.

## Resolved questions (Regente, T-2 phase 1)

- OQ-DL-1: accepted. `scope=content` is checked only for single-member archives (or non-archives); otherwise history records `hash: "skipped"`.
- OQ-DL-2: keep first byte 10 m and idle 5 m, hard cap 15 m. Tuning after real measurements is in the backlog.
- OQ-DL-3: `download.allowed_hosts` is a config value defaulting to `["proxy.mothdex.net"]`; the T-10 fixtures confirm the host.
