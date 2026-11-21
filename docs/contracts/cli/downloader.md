# Contract: downloader (`internal/proxy`)

Status: draft v3 (T-2 + T-16 change requests; T-27 size authority) · Owner: Alicerce · Implementer: Forja (T-17) · Risk: **High** (writes files named by the server)
Sources: [`mothdex-proxy/src/main.cpp`](https://github.com/cavv-dev/mothdex-proxy/blob/main/src/main.cpp) (`streamFile`, `getFileDetails`, `pieceWaitTimeout`) · Covers A1–A4. See [ADR-0002](../../adr/ADR-0002-downloader-without-range.md).
Threat model: Sentinela, T-16 ([`T-16-threat-model-downloader.md`](../../security/T-16-threat-model-downloader.md); TH-nn and CR-n ids below). Every
**MUST** here maps to at least one Ensaio test (T-18).

Everything the API returns about a link (`url`, `filename`, `size`, `format`) and about its entry (`slug`, `platform`,
`cover_url`) is **untrusted** (T-16 F1). It is validated **at use, in the downloader**, on every attempt, whether it
came from the network or from the cache (CR-11).

## 1. Upstream facts the design depends on

| Fact | Consequence |
|---|---|
| Proxy: `Range` is ignored; it always answers `200` with the whole file and `Content-Length` | no resume, no segments; every retry starts from byte 0 |
| API: `link.size` is scraped from the sources and is only a rough indicator; the real size is the proxy's `Content-Length` (maintainer, [cavv-dev/mothdex-api#1](https://github.com/cavv-dev/mothdex-api/issues/1)) | `Content-Length` is the transfer authority (§3.2); `link.size` is a preflight estimate only (T-27) |
| Proxy: headers are written **before** the first piece is available | "warming" = headers received, 0 body bytes |
| Proxy: each piece may wait up to `pieceWaitTimeout` = 15 min; on timeout the stream just ends | short stream: detect `bytes < expected` and retry |
| Proxy: `404 "not found"` both for unknown names and when `makeRoom` cannot free disk | 404 is **possibly transient** |
| Proxy: never redirects; `Content-Disposition` filename comes from the torrent | any 3xx is anomalous; the name comes from `link.filename`, sanitized |
| **Not every link is a proxy link** (T-16 F2): only torrent-sourced links use `proxy.mothdex.net`. Internet Archive, MarioCube, NoPayStation (often `http://`) and `mothdex.net/static` links are passed through verbatim | v0.1 downloads proxy links only (§2); other links are shown as unavailable (app-service §3) |

## 2. Request and URL policy (CR-1, CR-8, CR-11; TH-16–19, TH-26)

**v0.1 is proxy-only** (CR-1; the product scope needs the owner's confirmation through Regente, OQ-DL-4). A link is
downloadable only if `url` passes every check, in Preflight, on **every** attempt:

- parsed with `net/url`; scheme exactly `https` (`http` is **never** allowed, not even by config); empty `User`;
  `Port()` empty or `443`;
- host = exact lowercase ASCII match, after stripping one trailing dot, against `download.allowed_hosts`
  (default `["proxy.mothdex.net"]`); IP literals and non-ASCII (IDN) hosts are rejected.

Failure: `Failed(untrusted_url)`, not retryable. The app service filters such links out **before ranking**, so they
never become the top choice (app-service §3).

HTTP client: `GET`, no `Range`, `Accept-Encoding: identity`, `Transport.DisableCompression = true`, no cookie jar,
same User-Agent and TLS settings as the API client (api-client §6). **Redirects: 0** for the proxy host (any 3xx is
`Failed(untrusted_url)`). A future explicit host table may allow ≤ 3 redirects, each re-checked against the policy above.
A response with a `Content-Encoding` other than empty or `identity` is `Failed(integrity)`. The body is always read
through `io.LimitReader(body, cl+1)`, where `cl` is the checked `Content-Length` (§3.2).

## 3. State machine

```
Queued ─▶ Preflight ─▶ Connecting ─▶ Warming ─▶ Transferring ─▶ Verifying ─▶ Finalizing ─▶ Done
              │             │           │            │              │             │
              ▼             └───────────┴─────┬──────┴──────────────┘             │
          Skipped / Failed                    ▼                                   ▼
                                       RetryWait ─▶ Connecting           Skipped / Failed (no retry)
any state ── Cancel / ctx ─▶ Canceled (staging removed, reservations released)
```

| State | Entered when | Leaves to |
|---|---|---|
| Queued | accepted by `Enqueue` | Preflight when a worker is free |
| Preflight | §3.1 checks, in order | Connecting · Skipped · Failed |
| Connecting | request sent; `connect_timeout` 30 s, `header_timeout` 60 s | Warming · RetryWait · Failed |
| Warming | `200` + headers received and the §3.2 header checks passed, 0 body bytes; `first_byte_timeout` (default 10 m) | Transferring · RetryWait |
| Transferring | ≥ 1 byte; streaming to the staging file with SHA-256 computed on the fly; `idle_timeout` (default 5 m) between reads | Verifying · RetryWait · Failed |
| RetryWait | retryable failure, budget left (§5) | Connecting (staging file truncated to 0 **on the same fd**) · Failed |
| Verifying | EOF with `bytes == cl` (§3.2) | Finalizing · RetryWait (one integrity retry) · Failed |
| Finalizing | `fsync` file, no-replace finalize (§6.3), `fsync` destdir, history record | Done · Skipped (`on_exists` hit at finalize) · Failed |

`Done` is reported only after both `fsync` calls succeed (CR-5).

### 3.1 Preflight (CR-2, CR-5, CR-7; TH-08, TH-14, TH-15, TH-20)

1. URL policy (§2).
2. **Size estimate:** `link.size ≤ 0` or `link.size > download.max_file_size` (default 256 GiB) → `Failed(size_mismatch)`,
   no connection. `link.size` is a rough catalog value (T-27): it is used only here, in the step 7 estimate and in the
   500 MB confirmation (cli-conventions §6). It never decides whether a transfer is correct.
3. **Name:** `Sanitize(link.filename)` (§6.1); a deny-listed extension → `Failed(unsafe_name)` unless `--allow-executable`.
4. **Destination dir** per §6.2 (platform id validation, confinement).
5. **Name reservation:** an in-process set keyed by `casefold(NFC(destdir + "/" + name))`, taken here and released at
   the terminal state. A collision inside the batch applies `on_exists` against the reserved name.
6. **`on_exists`** against the filesystem (`skip` → `Skipped`; `rename`; `overwrite` only with `--force`).
7. **Space reservation ledger**, one per filesystem (staging and destination may be on different filesystems):
   `need = size + max(size / 20, 256 MiB)`, with `size = link.size` here and `cl` after §3.2, in overflow-checked `int64` math (no floats). The job starts only if
   `free − reserved(fs) ≥ need` on each filesystem it will write; otherwise `Failed(disk_full)`. Released at the
   terminal state. `ENOSPC` while writing → `Failed(disk_full)` and immediate cleanup.
8. **In-flight journal:** append `{jobID, staging paths, destdir, root, started_at}` to `<paths.data>/inflight.jsonl`;
   the entry is removed at the terminal state (§6.4).

### 3.2 Header checks (T-27; TH-19, TH-20)

`Content-Length` is the size authority for the transfer. These checks run on every attempt, after the `200` headers
and **before the first body byte is read**; a failure closes the response unread and removes nothing but the empty
staging file.

1. **Encoding:** `Content-Encoding` empty or `identity` (§2), checked first, because `Content-Length` is now the only
   length.
2. **Length present and strict:** `resp.ContentLength` (Go parses the header; chunked or missing gives `-1`) must be
   `≥ 1`. The raw header must be ASCII digits only and parse with `strconv.ParseInt(v, 10, 64)` to the same value.
   Missing, chunked, `0`, or malformed → `Failed(size_mismatch)`, not retryable. Call the value `cl`.
3. **Cap:** `cl > download.max_file_size` → `Failed(size_mismatch)`, not retryable.
4. **Pin across retries:** the first attempt that passes step 2 pins `cl` for the job. A later attempt with a
   different `Content-Length` → `Failed(size_mismatch)`, not retryable (the file changed upstream).
5. **Space, again:** the job's ledger claim (§3.1 step 7) is recomputed with `cl` and swapped atomically under the
   ledger lock: the job keeps going only if `free − (reserved(fs) − own claim) ≥ need(cl)` on each filesystem it
   writes; otherwise `Failed(disk_full)`, not retryable. A smaller `cl` shrinks the claim.
6. **Consent at the real size:** the job carries `SizeConsent` (app-service §2). When it is the default rule,
   `interface.confirm_downloads` is on, `link.size ≤ 500 MB` and `cl > 500 MB`, the job ends
   `Failed(confirmation_required)`, not retryable, with `cl` in the event. When it is an exact value, `cl` must equal
   it, or the job ends the same way. Interactive UIs offer **Download anyway**, which re-queues that one job with
   `SizeConsent = cl`; non-interactive runs fail unless `-f/--force` (cli-output §1). No body byte is written
   without consent at the real size, and no worker waits on a prompt.
   - **Threshold:** 500 MB is the same constant as the cli-conventions §6 prompt, `524 288 000` bytes (sizes are
     binary-based, §5 there), so the two gates cannot disagree.
   - **`ConsentAny` follows `compose.startupGrants`:** only a per-command `-f/--force` sets it. A `-f` given when the
     Shell, TUI or GUI started never becomes `ConsentAny` for later jobs; in a session, consent comes only from a
     per-command `-f` or from Download anyway.
7. **Drift record:** `link.size` and `cl` are both recorded (§8 `Record`). No tolerance band applies: a catalog
   size off by a unit (MB vs MiB, KB vs MB) is common and is not an error. When `cl > 1.5 × link.size` or
   `cl < link.size / 2`, the job adds a warning (`warnings[]` and stderr) once.

Body: exactly `cl` bytes. The `cl+1` limit reader yielding the extra byte → abort immediately,
`Failed(size_mismatch)`. EOF with `bytes < cl` is a truncated stream (§5, retryable).

**What the checks prove (T-27).** `cl` and the body come from the same TLS-authenticated proxy, so the exact length
proves transport self-consistency only: no truncation, no extra bytes, no transparent decoding. It no longer detects
the proxy serving a different file than the catalog lists (the old `Content-Length == link.size` rule did, by
accident, for files of a different size). The zip CRC (§4) still detects corruption inside zip members, and the
local SHA-256 still records what was received. Upstream hashes (U3/U4) are parked by the maintainer, so T-16 TH-25
stays the accepted residual.

## 4. Verification (A4; CR-8; TH-24)

Order, all on the staging file:
1. **Size**: `bytes == cl` (always; §3.2).
2. **Zip CRC**: if the first bytes are `PK\x03\x04` (magic bytes, not the extension), read every member to EOF with
   `archive/zip`, which checks CRC32, discarding the output (**never extract**). Caps, all enforced: cumulative
   uncompressed bytes ≤ `min(64 GiB, max(1 GiB, 20 × cl))`; ≤ 65 535 members; reading stops on `ctx` cancel.
   A cap hit is `Failed(integrity)`. 7z/rar: not checked (backlog), and history records `crc: "skipped"`.
3. **Upstream hashes** (future, `link.hashes != nil`): `scope=file` compares against the staging file;
   `scope=content` compares against the single archive member, or the file itself if it is not an archive
   (multi-member: `hash: "skipped"`). Strongest available algorithm: sha1 > md5 > crc32.
4. **Local SHA-256** of the file as downloaded, stored in history (proof of what was received, not of correctness).

Config `download.verify: false` skips only steps 2 and 3; size is always checked.
An integrity failure gets **one** re-download; a second one ends in `Failed(integrity)`.

## 5. Retry policy

| Failure | Retryable | Budget |
|---|---|---|
| network error, reset, TLS handshake, timeout (connect/header/first-byte/idle) | yes | `max_attempts` total (default 5) |
| truncated stream (`EOF` with `bytes < expected`) | yes | same |
| `5xx`, `429` | yes; `Retry-After` honored when larger, capped at 15 min (a worker slot is held while waiting) | same |
| `404` | yes (A3) | separate: `not_found_retries` (default 3) with fixed delays 30 s, 60 s, 120 s |
| other `4xx`, any 3xx, `size_mismatch` (including a `Content-Length` change, §3.2), `confirmation_required`, `untrusted_url`, `unsafe_name`, `path_rejected`, disk full, permission | no | — |

Backoff: full jitter, `sleep = rand(0, min(60 s, 2 s × 2^(attempt-1)))`, through an injected `Sleeper` and `Clock`.
Each attempt is recorded (state, cause, bytes) for history.

## 6. Files and paths

### 6.1 `Sanitize` (CR-6, CR-3; spec owned by T-16 §5, reproduced here as binding)

`Sanitize(raw string) (string, error)` is a **pure** function with the **same rules on every GOOS** (exFAT/NTFS
drives on Linux, T-16 F10). Steps, in order:

1. **Last component:** split on `/` and `\`; keep the last non-empty element.
2. **Unicode:** invalid UTF-8 → dropped. Normalize to **NFC** (`golang.org/x/text/unicode/norm`). Drop runes in
   `Cc`, `Cf` (bidi controls, zero-width), `Co`, `Cs`. If NFKC of the result contains `/`, `\` or `:`, those runes
   become `_` (fullwidth look-alikes).
3. **Forbidden characters:** `< > : " / \ | ? *`, `0x00–0x1F`, `0x7F` → `_`; collapse runs of `_` and spaces.
4. **Edges:** strip leading `.`, ` `, `-`; strip trailing `.` and ` `; repeat until stable.
5. **Reserved names** (case-insensitive, on the part before the first `.`, trailing spaces trimmed):
   `CON PRN AUX NUL COM0–9 LPT0–9 COM¹ COM² COM³ LPT¹ LPT² LPT³ CONIN$ CONOUT$` → prefix `_`.
6. **Length:** ≤ 200 bytes UTF-8; cut the stem on a rune boundary, keeping an extension of ≤ 16 bytes; re-run 4–5.
7. **Empty or only dots/underscores** → `Sanitize(slug) + "." + format` when `format` matches `^[a-z0-9]{1,10}$`
   (otherwise no extension); still empty → `download-<jobID>`.
8. **Executable deny-list** (case-insensitive, last extension): `sh bash zsh csh ksh fish command run desktop appimage
   py pl rb jar exe com scr pif bat cmd ps1 psm1 psd1 vbs vbe js jse wsf wsh hta msi msp msc cpl dll lnk url reg scf
   inf library-ms settingcontent-ms appref-ms` → `ErrUnsafeName` (`Failed(unsafe_name)`) unless the run has the
   **flag** `--allow-executable` (flag only; no config or env equivalent). `bin` is a ROM extension and is allowed.
9. **Invariants** (panic in tests, error at runtime): `name == filepath.Base(name)`, `filepath.IsLocal(name)`, no rune
   removed by steps 2–3 remains, `len(name) ≤ 200`, `name` is not `.` or `..`.

Property: `Sanitize(Sanitize(x)) == Sanitize(x)`. UIs display the sanitized name; history stores both the raw name
(escaped) and the sanitized one.

### 6.2 Destination directory and confinement (CR-2, CR-13; TH-01–07)

- The destination dir comes from `-o`, `download.dir` or the profile (profiles §4). Every path component that comes
  from data must be a **single local component**: the platform id must match `^[a-z0-9][a-z0-9_-]{0,31}$` **and** be
  present in `/platforms` (cached `meta`), otherwise `Failed(path_rejected)`; `profile.folders` values must pass
  `filepath.IsLocal` and contain no separator.
- **Confinement applies to the final dir in every layout:** the resolved destdir must be inside the configured root,
  and all file operations go through an `os.Root` opened on that root (no `..`, no symlink escape).
- **"Inside the root" is a per-component check** (T-16 G4), never a string prefix (`C:\roms` vs `C:\roms2`,
  `/userdata/roms` vs `/userdata/roms-old`): `rel, err := filepath.Rel(root, dest)` must succeed, `filepath.IsLocal(rel)`
  must hold (or `rel == "."`), and no element of `rel` may be `..`. On Windows the comparison is case-insensitive and
  both paths must be on the same volume (`filepath.VolumeName`).
- **Never into a launcher folder** (T-16 G3, TH-06): the final destdir (from `-o`, `download.dir`, `profile.rom_root`
  or the profile map) must not be equal to, or inside, the active profile's `launcher.dir` or any folder named
  `ports`, `ports_scripts` or `windows` directly under the ROM root; otherwise `Failed(path_rejected)`, unless the run
  has `--allow-executable`.
  `Profile.IsLauncherPath` (internal/library) is **lexical**: it compares path components, without touching the
  filesystem, and fails closed on relative or drive-relative input. Callers therefore make `-o` (and any relative
  `download.dir`) **absolute first** (`filepath.Abs`). Preflight applies the check **again** on the real path of the
  destdir after it is resolved (`filepath.EvalSymlinks`; on Windows the final path from `GetFinalPathNameByHandle`,
  which also expands 8.3 short names such as `PORTS~1` and follows junctions). A destdir that becomes a launcher
  folder only after resolution is `Failed(path_rejected)`.
- **Windows** (CR-13): Go ≥ 1.23 `EvalSymlinks` does not resolve junctions, so confinement is verified with
  `GetFinalPathNameByHandle` on the opened destdir handle, compared against the same call on the root handle.
- On POSIX systems, directories the downloader creates get mode `0700` and files `0600` (umask may only reduce them) (CR-7).

### 6.3 Staging and finalize without replace (CR-4, CR-7; TH-08–13)

- `jobID` = 128-bit random value, lowercase base32 without padding (26 chars, `[a-z2-7]`). No server text in staging names.
- Staging location (T-17-arch): when `<paths.temp>` and the destdir are on the **same device** (`FS.Free` reports the
  device id), the staging file is `<destdir>/.<jobID>.part` inside the destination root, so finalize is a plain
  no-replace rename and the file is written once. Otherwise it is `<paths.temp>/downloads/<jobID>.part` and finalize
  uses the cross-device copy below. Two `os.Root` handles on the same disk are **not** a reason to copy. The space
  ledger then holds one claim for a same-device job and two for a cross-device one.
- Staging files are opened through `os.Root.OpenFile` with `O_CREATE|O_EXCL|O_NOFOLLOW`; the same fd is reused
  (truncated) across retries. A dot-prefixed `.part` name is not a ROM extension, so frontends do not list it.
- Finalize **never replaces** an existing file (`os.Rename` and `os.Root.Rename` both replace, T-16 F8):
  - Linux `renameat2(RENAME_NOREPLACE)` (`golang.org/x/sys/unix`); Windows `MoveFileExW` **without**
    `MOVEFILE_REPLACE_EXISTING`, with `MOVEFILE_WRITE_THROUGH`; macOS `renameatx_np(RENAME_EXCL)`;
  - unsupported (`EINVAL`/`ENOSYS`, e.g. some FUSE or exFAT drivers): `os.Root.Link` then remove the staging file; if
    hard links are unsupported too, create an `O_EXCL` placeholder with the final name, then rename onto that
    placeholder only;
  - different filesystem (`EXDEV`): copy to `<destdir>/.<jobID>.mothdex-tmp` (`O_EXCL|O_NOFOLLOW`, `0600`), `fsync`,
    then the same no-replace finalize inside `destdir`;
  - `EEXIST` at finalize → apply `on_exists` again (`skip` → `Skipped`, staging removed).
- `download.on_exists`: `skip` (default), `rename` (`name (1).ext` … `name (99).ext`; past 99 → `Failed(exists)`),
  `overwrite`: only with the `--force` flag, never from config or env; it is the one case that uses a replacing rename,
  for that single file. Before replacing, `Lstat` the target through the `os.Root`: anything that is not a regular
  file (symlink, directory, device, FIFO, socket, Windows reparse point) → `Failed(path_rejected)`, and the target is
  left untouched (T-16 G2, TH-10).
- **Collisions inside one batch** (Regente decision, T-17): `overwrite` replaces only a file that existed **before**
  the batch. When two jobs of the same batch reduce to the same reserved name (§3.1 step 5, `casefold(NFC(...))`), the
  later job is resolved by `rename` (`name (1).ext` …) even under `overwrite`, so a job never replaces a file written
  by another job of the same batch. `skip` and `rename` behave as usual. This holds **after** the earlier job is done:
  the downloader keeps, for the whole batch, the set of names its jobs finalized (`casefold(NFC(destdir/name))`), and
  `overwrite` is refused for any name in it. Releasing the per-job reservation at a terminal state is not enough.

### 6.4 Cleanup (CR-7; TH-15)

At startup, delete only **regular** files (`Lstat`, never follow) older than 24 h by **mtime** whose names match exactly:
- `^[a-z2-7]{26}\.part$` in `<paths.temp>/downloads/`;
- `^\.[a-z2-7]{26}\.part$` and `^\.[a-z2-7]{26}\.mothdex-tmp$` in the destdirs listed in `<paths.data>/inflight.jsonl`.

Cleanup is per destination root: a Downloader handles only journal entries whose `root` is its own absolute
destination root (an entry without `root` predates the field and is handled by any root). Entries of a root that is
not opened again stay until it is; the staging sweep in `<paths.temp>/downloads/` does not depend on the journal.
Journal entries whose job files are gone are pruned. The downloader never deletes anything else.

## 7. Pool and events

- Worker pool: `download.concurrency` default **2**, clamped 1..6. Parallelism is across files only (A1).
- Events: `{JobID, State, Attempt, Bytes, Expected, RateBps, Err}`; progress events are throttled to 4/s.
- Boxart (`cover_url`) is fetched through the `covers` cache with its own URL policy (cache §4), outside the pool;
  a failure never fails the job.
- Every job reaches exactly one terminal state (`Done`, `Skipped`, `Failed`, `Canceled`), releases its reservations
  (name, space, journal) and writes one history record.

## 8. Test hooks (T-18)

Everything that touches the outside world is an injected port (`internal/proxy/ports.go`, T-17):

| Port | Contract |
|---|---|
| `HTTPDoer` | `Do(*http.Request)`; the downloader enforces the URL policy (§2) itself and never relies on the doer |
| `Clock` | `Now()` and `NewTimer(d)` (channel plus stop), so timeouts run on a fake clock |
| `Sleeper`, `Rand` | backoff sleep that honours `ctx`; jitter source (`Int63n`). The job id has its own `io.Reader` source |
| `FS` | confined file system with `os.Root` semantics: `OpenExcl` (create-exclusive, no-follow, `0600`), `Open` (read an existing regular file again: verify and the EXDEV copy), `Lstat`, `MkdirAll`, `Remove`, `ReadDir`, `Link` (`fs.ErrExist`, or `errors.ErrUnsupported`), `Free` (free bytes plus device id, for the ledger and the EXDEV decision) and `SyncDir` (the §3 `fsync` of the destdir; it reports success where a directory cannot be synced, e.g. Windows, where `MOVEFILE_WRITE_THROUGH` covers it) |
| `File` | the staging handle: read, write, seek, `Truncate`, `Sync`, `Close`; reused across retries (§3) |
| `Finalizer` | `Rename(fs, from, to, replace)`: no-replace per OS (§6.3); `fs.ErrExist`, `ErrUnsupported` or `EXDEV`; `replace=true` only for `--force` after the `Lstat` check |
| `Journal` | the in-flight journal (§6.4): `Add` and `Entries` plus clearing an entry at the terminal state, over `{job_id, staging, dest_dir, root, started_at}` (`root` is the absolute destination root, used by the per-root cleanup) |
| `History` | `Append(Record)`, exactly once per job at its terminal state |

`Record` (input to history, T-19): `JobID, Slug, Platform, State, Code` (empty unless failed), `RawName` (the received
`link.filename`, escaped before display), `Name` (sanitized; empty if the job failed before naming), `Path`, `Bytes`,
`SHA256`, `CRC` and `Hash` (`ok|skipped|failed`), `CatalogSize` (`link.size`), `ContentLength` (the pinned `cl`; 0 if
no attempt passed §3.2 step 2), `Attempts`, `Started`, `Finished`. Entry and link metadata (`title`,
`source_name`, `link_index`) are added by the history layer. `Record.Path` is the **absolute** final path (the
destination root joined with the name), because history outlives a profile or `-o` change; a root-relative path
would need a migration later.

**Calling the downloader (T-20).** The app service's `Downloads` (app-service §1) needs, from the downloader: a job id
**before** the job starts (returned by `Enqueue` and carried by every event), a `Queued` event when a job is
accepted (not when a worker slot frees up), and cancellation of **one** job without cancelling the batch. The
downloader therefore assigns ids at submission and supports per-job cancel by id; error codes are only those of
cli-output §4 (a destdir outside the root is `path_rejected`).

The fake proxy covers: stall before the first byte, stall mid-stream, truncation, transient 404, 429,
any 3xx, `Content-Length` missing, chunked, malformed, above the cap or changed between attempts, extra bytes, `Content-Encoding: gzip`, malicious filenames, off-policy URLs (http,
userinfo, port, IP literal, IDN, trailing dot) and zip bombs. [T-16 §8](../../security/T-16-threat-model-downloader.md) lists the full test set.

## Resolved questions (Regente, T-2 phase 1)

- OQ-DL-1: accepted. `scope=content` is checked only for single-member archives (or non-archives); otherwise history records `hash: "skipped"`.
- OQ-DL-2: keep first byte 10 m and idle 5 m, hard cap 15 m. Tuning after real measurements is in the backlog.
- OQ-DL-3: `download.allowed_hosts` is a config value defaulting to `["proxy.mothdex.net"]`; the T-10 fixtures confirm the host.

- T-27 (Regente, Sentinela): `Content-Length` is the size authority (§3.2); `link.size` is an estimate. No tolerance
  band; the 500 MB confirmation re-runs on `cl`.

## Open questions

- OQ-DL-4 (CR-1, owner decision via Regente): v0.1 downloads proxy links only. Internet Archive, MarioCube, NoPayStation and `mothdex.net/static` links are listed but unavailable. Confirm, or name hosts for a later version (each needs its own facts: redirects, `Range`, `http`).
- OQ-DL-5: no Mothdex `format` is known to collide with the deny-list (`pc`/`dos` ship zips in the observed data). The T-10 fixture script should count link extensions over a sample to confirm.
