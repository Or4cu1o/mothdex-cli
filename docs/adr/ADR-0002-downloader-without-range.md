# ADR-0002: The downloader does not depend on HTTP `Range`

Status: **Accepted** (Regente, T-2 phase 1, 2026-11-20) · Author: Alicerce · Findings A1–A3

## Context

`mothdex-proxy` (`src/main.cpp`):
- `streamFile()` always answers `200` with the full `Content-Length` and ignores `Range` (it only lists `Range` in
  CORS allow-headers).
- The file is fetched on demand via BitTorrent. Headers go out before any data; each piece may wait up to
  `pieceWaitTimeout` = 15 min, and on timeout the stream simply ends short.
- `getFileDetails()` returns `404` both for unknown files and when `makeRoom()` cannot free disk space, so a 404 may
  be transient.

Legacy expectations ("multiple connections", resume) cannot be met against this server.

## Options

1. **Segmented, resumable downloads with `Range`.** Impossible today; it would silently degrade to full re-downloads.
2. **Single stream per file, restart from zero on failure, parallelism across files.** Works with the server as it is.
3. Probe for `Range` support and use it when available. Adds complexity for a feature no server offers yet.

## Decision

Option 2, with option 3 deferred until upstream adds `Range` (issue U5):
- One GET per attempt, no `Range` header; a retry truncates the `.part` and starts again.
- "Concurrency" means a worker pool of whole files, default 2 (the proxy is shared by every user).
- No total timeout. Instead: `first_byte_timeout` (default 10 m) for the "warming" phase, and `idle_timeout`
  (default 5 m) between reads, both capped at 15 m.
- Truncation (`bytes < expected`) is retryable; a 404 gets a small, separately budgeted retry with long delays.
- Integrity = exact size + zip CRC + local SHA-256, and upstream `hashes` when they exist (A4).

Details: [`downloader.md`](../contracts/cli/downloader.md).

## Consequences

- Large files over slow torrents can cost several full re-downloads; UIs must show the attempt number and the
  "warming" state, so a stall does not look like a hang.
- The proxy sees no extra load from probing or parallel segments.
- If upstream ships `Range`, a new ADR adds resume behind a capability check. The state machine already separates
  `Connecting`/`Warming`/`Transferring`, so resume only changes how `RetryWait → Connecting` treats the `.part`.
