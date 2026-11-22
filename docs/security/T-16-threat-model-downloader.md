# T-16: Threat model, `mothdex-cli` downloader write path

Status: v1 · Author: Sentinela · Date: 2026-11-20 · Risk: **High** · Consumers: Alicerce (contract), Forja (T-17),
Ensaio (T-18), Guindaste (T-21 installer)
Changed by T-27 (Alicerce, agreed with Sentinela): `Content-Length` is the size authority, `link.size` an estimate;
TH-19, the §9 residual and cases 31, 32b–32d, 35 and 36 below.
Gate: T-17 starts only after the contract changes in §7 are accepted (or explicitly rejected by Regente).

## 0. Scope, method, limits

**In scope:** everything between "the API returned a `Link`" and "a file exists in the user's ROM folder and the
frontend was told to rescan": URL and host checks, redirects, response handling, the `.part` file, verification
(size, zip CRC, hashes), final-name sanitization, destination resolution (profiles, `-o`, `download.dir`), the
finalize move, `on_exists`, orphan cleanup, disk full, the cover cache as a second write path, the post-batch
refresh hook (`127.0.0.1:1234`), and the launcher scripts the installer writes.

**Out of scope:** the MCP `download_file` tool (separate threat model before M3'), release signing and CI
(supply chain, Guindaste), the content of ROMs themselves (we never execute or extract them), the API client's
rate limiting.

**Method:** STRIDE per data flow over the contracts (`downloader.md`, `app-service.md`, `profiles.md`, `config.md`,
`cache.md`, `api-client.md`), PLAN-v0 (A1–A7, §2.4, §9), ADR-0002, research T1e, and source reading of
upstream [`mothdex-proxy/src/main.cpp`](https://github.com/cavv-dev/mothdex-proxy/blob/main/src/main.cpp), [`mothdex-api/api.py`](https://github.com/cavv-dev/mothdex-api/blob/main/api.py), [`mothdex-db/{parsers,sources.py,platforms.py}`](https://github.com/cavv-dev/mothdex-db) (snapshot read on 2026-11-20).
Go stdlib behavior checked against the local Go 1.27.4 toolchain docs (`go doc os.Root`, `doc/godebug.md`,
`archive/zip/reader.go`).

**Tools:** manual review, `grep`, `go doc`. No scanners apply (no implementation yet). **Not verified:** behavior on
real devices (Batocera/EmuELEC/Lakka/RetroBat), `renameat2(RENAME_NOREPLACE)` support on exFAT/vfat/ntfs3/ntfs-3g
(FUSE), Windows ACLs of `C:\RetroBat`, and the live link-host mix in production (inferred from `mothdex-db` source).
This document does not claim the design is free of vulnerabilities.

## 1. Key facts that shape the model

| # | Fact | Evidence | Consequence |
|---|---|---|---|
| F1 | `link.filename`, `link.url`, `link.size`, `link.format`, `entry.slug`, `entry.platform`, `entry.cover_url` are passed through by the API unchanged from scraped sources | `api.py:81-82`; `mothdex-db/build.py:156` | All of them are **untrusted**, including fields used to build paths (`platform`, `slug`, `format`). |
| F2 | **Not every link is a proxy link.** Only `TorrentParser` emits `https://proxy.mothdex.net/file/<key>` (`parsers/torrent.py:59`). Internet Archive (40 sources, `archive.org`), MarioCube (3, `repo.mariocube.com`), NoPayStation (3, PSN CDN URLs taken verbatim from the NPS TSV, often `http://`), and `https://mothdex.net/static/...` (PS3 RAP, PSV zRIF) are emitted as-is | `sources.py` parser counts; `parsers/internet_archive.py:50`, `nopaystation.py:61,70,92,110`, `mariocube.py:36` | With the default `allowed_hosts: [proxy.mothdex.net]`, these links fail with `untrusted_url`. Contract and ranking must handle this (CR-1). A1–A3 (no `Range`, 15-min stalls, transient 404) are proxy facts only. |
| F3 | The proxy never sends a redirect; it answers `200` + `Content-Length` or `404 "not found"` | `main.cpp` `handleHttpConnection`, `streamFile` | For the proxy host, any 3xx is anomalous. |
| F4 | The proxy's `Content-Disposition` filename comes from the torrent | `main.cpp:574` | Ignored by contract (good). |
| F5 | Platform ids are short lowercase tokens (`ps1`, `md`, `pc`, `dos`) but the client receives them from the API | `platforms.py` | In `per_platform_id` layout (Lakka, generic) the platform id becomes a **directory name**. |
| F6 | Launcher dirs live **inside the ROM tree**: Batocera `/userdata/roms/ports` (`.sh`), EmuELEC `/storage/roms/ports_scripts` (`.sh`), RetroBat `roms\windows` (`.exe .bat .cmd .lnk …`) | T1e rows 4, 20, 30 | A downloaded file that lands in one of these folders with a runnable extension appears in the frontend as a launchable entry. Batocera/EmuELEC run ES as **root**. |
| F7 | Go ≥ 1.23: `filepath.EvalSymlinks` does **not** resolve Windows mount points (junctions) | `doc/godebug.md` (`winsymlink`) | The contract's confinement check (§6 "EvalSymlinks on the directory") is blind to junctions on Windows. |
| F8 | `os.Root.Rename` replaces an existing target (same semantics as `os.Rename`); Go's `os.Rename` on Windows uses `MOVEFILE_REPLACE_EXISTING` | `go doc os.Root.Rename` | Neither gives "move without overwrite"; `on_exists: skip` cannot be enforced atomically with them. |
| F9 | `archive/zip` fails a member that yields more than `UncompressedSize64` (`ErrFormat`) and checks CRC32 at EOF | `archive/zip/reader.go:301-308` | Per-member over-read is bounded; overlapping-member bombs still multiply total output, so a cumulative cap is needed (contract has one). |
| F10 | Target filesystems include exFAT/vfat/NTFS USB drives under Linux (Batocera share, SD cards) | T1e row 6 | Windows naming rules and case-insensitivity apply on Linux too; no hard links, no symlinks, exec bit absent. |

## 2. Assets

| ID | Asset | Why it matters |
|---|---|---|
| AS1 | Integrity of the host filesystem outside the ROM root (`/userdata/system`, `/storage/.config`, `%AppData%`, startup folders) | Write outside root = persistence / code execution, as **root** on Batocera/EmuELEC/Lakka |
| AS2 | Launchable entries in the frontend (ports, `windows` system) | Placing a script there = one-click code execution |
| AS3 | Existing user files in the ROM folders (ROMs, saves next to ROMs, `gamelist.xml`, `.data` dirs) | Overwrite or deletion = data loss |
| AS4 | Integrity/completeness of downloaded files | Truncated or corrupt ROMs, silent bad data |
| AS5 | Device availability: disk space, RAM, CPU on small ARM boards | Full `/userdata` breaks saves and ES; OOM kills the frontend |
| AS6 | The user's LAN and loopback services | SSRF via URLs the client fetches (downloads, covers, refresh hook) |
| AS7 | Local cache and history (`paths.temp`, `paths.data`) | Poisoned entries reused later; exported files opened in spreadsheets |
| AS8 | The installed binary and launchers (`bin_dir`, launcher scripts) | Replacing them = code execution every time the user opens the Port |

## 3. Actors and trust boundaries

**Actors:** (A) a compromised or malicious upstream: Mothdex API/DB, a scraped source (IA uploader, DAT
maintainer, torrent creator), or the proxy operator; (B) a network attacker (MITM; relevant only where TLS is absent
or broken); (C) another local user or a LAN user with write access to a shared folder (Batocera SMB share,
multi-user desktop, `C:\RetroBat` inherited ACLs); (D) the user, by mistake (bad config, `--force`). The user's
own config file, `-o`, flags and profile overrides are **trusted input but validated** (fail loud, not silent).

```
 [Mothdex API] --JSON(Link,Entry)--> |B1| --> pkg/mothdex --> cache(api) --> app service --> downloader
 [proxy / other hosts] --HTTP body--> |B2| --------------------------------------------------^   |
 [cover hosts] --image--> |B2'| --> cache(covers) --> TUI/GUI decode                              |
 local FS (ROM tree, temp, cache) <==== |B3| writes: .part, staging, final, cleanup <------------- +
 ES on 127.0.0.1:1234 <---- |B4| GET /reloadgames (once per batch)
 install.sh / installer --> |B5| bin_dir + launcher scripts (ports, ports_scripts, roms\windows)
 frontend (ES, runs as root on Batocera/EmuELEC) reads ROM tree <--- |B6|
```

- **B1** API → client: every field untrusted (F1). Validation happens **at use** in the downloader, not only at decode,
  because data also arrives from the cache (AS7).
- **B2** download host → client: TLS-authenticated for `https` hosts only; the body is unauthenticated content.
- **B3** client → filesystem: the critical boundary; directory symlinks/junctions can redirect writes.
- **B4** client → loopback service.
- **B5** installer → persistent dirs: one-time, privileged on emulation systems.
- **B6** filesystem → frontend: names and extensions decide what the frontend shows and how it launches it.

## 4. Threats

Likelihood (L) and impact (I): Low / Med / High. "Contract" = where the mitigation lands. "Severity" = finding
severity of the **current contract gap** (risk-process scale); "covered" means the contract already handles it.

### 4.1 Name and path (B1 → B3)

| ID | Threat (STRIDE) | L | I | Mitigation | Contract | Severity |
|---|---|---|---|---|---|---|
| TH-01 | Path traversal or absolute path in `filename` (`../../system/custom.sh`, `/etc/x`, `C:\x`, `\\srv\share\x`, `..\\..`) (T, E) | Med | High | `Sanitize` keeps only the last component after splitting on **both** `/` and `\`, then applies §5 rules; result must equal `filepath.Base(result)` and `filepath.IsLocal(result)` | downloader §6 | covered (minimum), tightened by CR-6 |
| TH-02 | Traversal through **`entry.platform`** used as a folder (`per_platform_id` on Lakka/generic) or through `profile.folders` values (T, E) | Low | High | Platform id must match `^[a-z0-9][a-z0-9_-]{0,31}$` **and** be in the `/platforms` list (cached `meta`); folder values from config are single relative components (`filepath.IsLocal`, no separators); confinement re-checked on the final dir | profiles §4, downloader §6 | **Major** (CR-2) |
| TH-03 | Traversal through the fallback name `<slug>.<format>`: `slug`/`format` are API data (T) | Low | High | Fallback goes through the same `Sanitize`; `format` must match `^[a-z0-9]{1,10}$` or is dropped | downloader §6 | **Minor** (CR-6) |
| TH-04 | Windows reserved device names (`CON`, `NUL.zip`, `COM1.bin`, `COM¹`, `LPT²`, `CONIN$`, `CONOUT$`), trailing dots/spaces (`game.zip.` → `game.zip`), ADS (`game.zip:payload`, `x::$DATA`), forbidden chars `<>:"/\|?*` and 0x00–0x1F (T, D) | Med | Med | §5 rules applied on **every OS** (exFAT/NTFS on Linux, F10); checks re-run after truncation | downloader §6 | covered partially; CR-6 |
| TH-05 | Unicode tricks: NFD vs NFC duplicates, bidi overrides (`U+202E` makes `game.hs.zip` render as `game.piz.sh`), zero-width chars, fullwidth `／` `＼` `：` that some layers fold to ASCII (S, T) | Med | Med | Normalize to NFC; drop category `Cf` and `Co`, `U+FFFD`; map fullwidth/halfwidth forms (`U+FF01`–`U+FF5E`) through NFKC for the separator check only, and reject the name if NFKC introduces `/ \ :` | downloader §6 | **Minor** (CR-6) |
| TH-06 | Runnable extension or launcher destination: `filename = "Update.sh"` written to a folder that is (or is mapped to) `ports`, `ports_scripts`, `windows`; or a `.desktop`, `.lnk`, `.bat`, `.exe` anywhere in the ROM tree, then launched from ES as root (E) | Low–Med | High | (a) Profile loader rejects any folder map, `rom_root` subpath or `-o` that equals `launcher.dir` or a known launcher system folder (`ports`, `ports_scripts`, `windows`, `steam`, `apps`); (b) the downloader refuses final names whose **last** extension is in the executable deny-list (§5.4) with `Failed(unsafe_name)`, overridable only by an explicit `--allow-executable` per run, never by config/env | downloader §6, profiles §2/§4 | **Major** (CR-3) |
| TH-07 | Hidden or confusing names: leading `.` (hidden from ES, or `.bashrc` style), leading `-` (option injection into the user's own scripts `for f in *; do tool $f`), names longer than 255 bytes / UTF-16 units, empty after sanitization (T, D) | Med | Low | Strip leading `.`, space and `-`; ≤ 200 bytes UTF-8 cut on a rune boundary preserving the extension; empty → fallback name | downloader §6 | covered partially; CR-6 |
| TH-08 | Case-insensitive or sanitization collisions inside a batch (`Game.zip` vs `game.zip`; `A:B.zip` vs `A_B.zip`) or across processes (CLI + TUI): second job silently overwrites the first or is wrongly `Skipped` (T, R) | Med | Med | In-process reservation set keyed by `casefold(NFC(destdir/name))`, taken in Preflight and released at terminal state; atomic no-replace finalize (TH-12) for cross-process | downloader §6, §7 | **Minor** (CR-4) |

### 4.2 Filesystem operations (B3)

| ID | Threat | L | I | Mitigation | Contract | Severity |
|---|---|---|---|---|---|---|
| TH-09 | Symlink/junction in the destination tree redirects the write outside the root (actor C via SMB share or shared dir) (T, E) | Low | High | Resolve root and destdir (`EvalSymlinks`) and require `destdir` inside `root` (both resolved), **plus on Windows** open the dir handle and compare `GetFinalPathNameByHandle` (resolves junctions, F7). All later ops are relative to an `os.Root` opened on the checked destdir. Error `dest_outside_root` with hint `-o <real path>` (explicit `-o` becomes the root) | downloader §6 | **Major** on Windows (CR-13); covered on Unix |
| TH-10 | Symlink/hard link planted at the final name or at the staging name in destdir (`.<jobID>.mothdex-tmp`) (T) | Low | High | Staging created with `O_CREATE|O_EXCL|O_NOFOLLOW` (`os.Root.OpenFile`, which never follows out of the root); finalize by rename (never opens the target); `overwrite` refuses if the existing target is not a regular file (`Lstat`: symlink, dir, device, irregular/reparse) | downloader §6 | **Minor** (CR-4) |
| TH-11 | TOCTOU on the `.part`: predictable name in a shared temp dir (`paths.temp` configured to `/tmp`), file swapped between write and rename, or retry reopening by path (T) | Low | Med | `jobID` = 128-bit random (crypto/rand, base32), never server text; `downloads/` dir created `0700`, files `0600`; refuse to use a `paths.temp` that is group/world-writable without sticky-owner check; retries `Truncate(0)`+`Seek(0)` on the **same open fd**; verify and hash from that fd | downloader §6, §3 RetryWait | **Minor** (CR-7) |
| TH-12 | Overwriting existing files: `on_exists: skip` checked in Preflight, but another process creates the name before the final `os.Rename`, which then replaces it (F8); `overwrite` from config/env instead of `--force` (T, D) | Med | Med | Finalize with **no-replace** primitives: Linux `renameat2(RENAME_NOREPLACE)` (`x/sys/unix`); Windows `MoveFileExW` **without** `MOVEFILE_REPLACE_EXISTING` (+`MOVEFILE_WRITE_THROUGH`); fallback when unsupported (`EINVAL`/`ENOSYS`, e.g. FUSE): `os.Root.Link` then remove staging, else `O_EXCL` placeholder + rename. `EEXIST` → apply `on_exists` again. `overwrite` only from the `--force` flag (env/config value rejected). `rename` mode: bounded `(1)..(99)`, each try no-replace | downloader §6 | **Major** (CR-4) |
| TH-13 | Visible final file not durable: power loss on a handheld after rename leaves a zero-length or partial ROM with the right name (I) | Med | Med | `fsync` the `.part`/staging before rename and `fsync` the destdir after (Unix); `FlushFileBuffers` + `MOVEFILE_WRITE_THROUGH` (Windows) | downloader §6 | **Minor** (CR-5) |
| TH-14 | Disk full: free-space check is per job, so 2–6 parallel jobs each pass and together fill `/userdata`; `.part` (temp fs) and EXDEV staging (dest fs) are on different filesystems on most Batocera/RetroBat setups, so the dest fs is never checked; `paths.temp` on tmpfs eats RAM (D) | Med | High | In-process **reservation ledger** per filesystem (device id): Preflight reserves `size` on the temp fs and, if different, on the dest fs; require `free − reserved ≥ size + max(5 %, 256 MiB)` margin; release on terminal state. `ENOSPC`/`EDQUOT`/`ERROR_DISK_FULL` → `Failed(disk_full)`, `.part` and staging removed immediately. Warn when `paths.temp` is `tmpfs`/`ramfs` | downloader §3 Preflight, §6 | **Major** (CR-5) |
| TH-15 | Orphan cleanup deletes the wrong thing: broad glob, follows a symlink, deletes a live `.part` of a long download in another process (a 50 GB file at 500 KB/s takes > 24 h), or never finds staging files in destdirs it does not know (T, D) | Low | Med | Delete only regular files (`Lstat`) whose name matches `^[a-z2-7]{26}\.part$` in `<paths.temp>/downloads/` and `^\.[a-z2-7]{26}\.mothdex-tmp$` in destdirs listed in an in-flight journal (`<paths.data>/inflight.jsonl`, written at Preflight, cleared at terminal state); age = **mtime** > 24 h (a live job writes at least every `idle_timeout` ≤ 15 m); never recurse | downloader §6 | **Minor** (CR-7) |

### 4.3 Network (B1, B2, B4)

| ID | Threat | L | I | Mitigation | Contract | Severity |
|---|---|---|---|---|---|---|
| TH-16 | Link URL off the allowlist: SSRF to LAN/loopback (`http://192.168.1.1/...`, `https://127.0.0.1:1234/...`), userinfo trick (`https://proxy.mothdex.net@evil/`), IP literals, odd ports, trailing-dot host, IDN homoglyphs, `http` downgrade (S, I) | Med | Med | Parse with `net/url`; require scheme `https`, empty `User`, `Port()` empty or `443`, host = exact lowercase ASCII match (after stripping one trailing dot) against `allowed_hosts`; reject IP literals and non-ASCII hosts; checked in Preflight on every attempt (cache-sourced links too) | downloader §2 | covered; tightened by CR-1 |
| TH-17 | Redirects: proxy never redirects (F3) but the contract allows 3; each hop could downgrade or leak headers (S, I) | Low | Med | For the proxy host: `CheckRedirect` returns an error on any 3xx → `Failed(untrusted_url)`. Keep "≤ 3, each re-checked, https only" only if non-proxy hosts are enabled later (CR-1) | downloader §2 | **Minor** (CR-1) |
| TH-18 | Non-proxy links (F2) silently selected by ranking and then failing, or, if the allowlist is widened, fetched from hosts with different semantics (IA redirects to `ia*.us.archive.org`; NPS over plain `http`) (S, D) | **High** | Med | v0.1: links whose host is not allowed are **filtered out before ranking** and shown as "unavailable (host not allowed)"; non-interactive commands never pick them. Widening is a separate decision with its own host table (exact hosts + redirect targets), never wildcard by default, and `http` never allowed | app-service §3 ranking, downloader §2, config §2 | **Major** (CR-1, owner decision) |
| TH-19 | Response tampering/abuse: `Content-Encoding: gzip` despite `identity` (Go transport would decompress transparently if not disabled), chunked body with no length, body longer than `Content-Length`, a `Content-Length` far larger than the catalog `link.size` (disk fill, consent bypass; T-27), slowloris (S, D) | Med | Med | `Transport.DisableCompression = true` and reject any non-identity `Content-Encoding`; require a strict `Content-Length` (missing, chunked or `0` → `size_mismatch`); copy with `io.LimitReader(body, cl+1)` and fail on the extra byte; re-run the cap, the space ledger and the 500 MB confirmation on `cl` before the first body byte (downloader §3.2, T-27); idle/first-byte timeouts (covered); no cookie jar | downloader §2–§3.2 | **Minor** (CR-8) |
| TH-20 | Absurd sizes: `link.size` ≤ 0, > disk, near `MaxInt64` so `size × 1.05` overflows and passes the free-space check (D) | Low | Med | Reject `size ≤ 0` (`Failed(size_mismatch)`, except a documented 0-byte case, none known) and `size > download.max_file_size` (default 256 GiB, config); compute margins with overflow-checked integer math, no floats | downloader §3 Preflight, config §2 | **Minor** (CR-5) |
| TH-21 | MITM where TLS is weak: Lakka/EmuELEC may lack an up-to-date CA bundle (T1e: unverified), pushing users toward an "insecure" flag (S, T) | Med | High | Never offer `--insecure`/`InsecureSkipVerify`; import `golang.org/x/crypto/x509roots/fallback` so the binary carries roots when the system has none; TLS ≥ 1.2 | api-client §5, downloader §2 | **Suggestion** (CR-12) |
| TH-22 | Refresh hook abused: loader accepts `http://localhost.evil.com:1234`, `http://127.0.0.1.nip.io`, DNS-resolved `localhost`, redirect to a LAN host, `HTTP_PROXY` applied, large body (S, D) | Low | Low | `url.Host` must be the IP literal `127.0.0.1` or `[::1]` (parsed with `netip`), scheme `http`, path fixed; `Transport.Proxy = nil`; no redirects; 3 s timeout; body discarded through `io.LimitReader(…, 64 KiB)`; once per batch only if ≥ 1 `Done`; no API data in the request | profiles §4 | **Minor** (CR-10) |
| TH-23 | Cover fetch (`cover_url`) is a second untrusted-URL write path: SSRF to LAN (GET-based CSRF on routers/NAS), `http`, SVG/HTML served as image, decompression bomb in image decode in TUI/GUI (a 5 MiB PNG can declare 60 000 × 60 000 px) (S, D) | Med | Med | `https` only; dialer `Control` rejects loopback, private, link-local, CGNAT, multicast and unspecified IPs **after** DNS resolution; ≤ 3 redirects each re-checked; 5 MiB cap (covered); content type **and** magic bytes must agree (jpeg/png/webp/gif); `image.DecodeConfig` before decode with ≤ 4096 × 4096 px; never written outside `cache/covers` with a hash name | cache §4, api-client | **Major** (CR-9) |

### 4.4 Content, cache, records

| ID | Threat | L | I | Mitigation | Contract | Severity |
|---|---|---|---|---|---|---|
| TH-24 | Zip bomb during CRC check: overlapping members or many members, 64 GiB of decompression on an ARM board = tens of minutes of CPU; huge central directory (D) | Low | Med | Cumulative cap `min(64 GiB, max(1 GiB, 20 × size))`; member count ≤ 65 535; checks honor `ctx` (cancel stops them); `io.Copy` into `io.Discard`, never into memory; **never extract**; do not set `GODEBUG=zipinsecurepath=0` (would fail legit archives for a check we do not need) | downloader §4 | **Minor** (CR-8) |
| TH-25 | Integrity ≠ authenticity: size + CRC + local SHA-256 detect corruption, not a malicious substitute supplied by upstream (T) | Med | Med (we never execute content) | Document as residual; upstream `hashes` (U2–U4) from DATs raise the bar; history records which checks ran | downloader §4 | residual |
| TH-26 | Cache poisoning: a tampered `cache/api` entry (shared portable `mothdex-data` on USB, other local user) supplies a malicious `Link` later (T) | Low | High | All B1 validations run at use time in the downloader regardless of source (TH-01/02/16 are not "decode-time only"); cache dirs `0700`, files `0600`; on Windows portable mode warn if the dir is writable by other users (not verified how) | cache §2, downloader §2/§6 | **Minor** (CR-11) |
| TH-27 | Terminal escape injection: titles, filenames, `extras` printed by CLI/Shell/TUI contain ESC/OSC sequences (OSC 52 writes the clipboard, OSC 8 fake links, title spoofing) (S, T) | Med | Med | Strip C0/C1 controls (except none) and `Cf` from every API string before rendering; `--json` output escapes them as JSON does | cli-output | **Minor** (CR-14) |
| TH-28 | CSV/TSV formula injection in `history -e csv` and `search -e csv` (`=HYPERLINK(...)`, `+cmd|...`) (T) | Low | Med | Prefix cells starting with `= + - @ \t \r` with `'`; quote all cells | cli-output / history | **Minor** (CR-14) |
| TH-29 | Log injection: newline or ANSI in filenames written to logs (R) | Low | Low | `slog` handlers quote values; never format untrusted strings into the message key | (implementation) | Suggestion |

### 4.5 Installer and launchers (B5, B6) — owner Guindaste (T-21)

| ID | Threat | L | I | Mitigation | Severity |
|---|---|---|---|---|---|
| TH-30 | Launcher script injection: paths with spaces/metachars unquoted in `.sh`; `%`, `&`, `^`, `!` in `.bat` paths | Low | High | Launcher content is a fixed template: `.sh`: `#!/bin/sh` + `exec "<abs path>" "$@"`, path validated `^[A-Za-z0-9_./+-]+$`; `.bat`: path relative to `%~dp0` and quoted, absolute path validated `^[A-Za-z0-9 _\\.:()+-]+$`, no delayed expansion; no `eval`, no `source` of user-writable files | Major (contract for T-21) |
| TH-31 | Writable `bin_dir`/launcher by other users → binary or `SDL2.dll` replaced (DLL planting) → code execution next time the Port runs (as root on Batocera) | Low (Batocera) / Med (Windows `C:\RetroBat`, inherited ACL from `C:\` grants Authenticated Users modify on new subfolders — not verified on target) | High | Linux: files `0755`, dirs `0755`, owner = installing user, refuse world-writable dirs; Windows: install under `%LocalAppData%\Programs\mothdex` or set an explicit ACL on `<retrobat>\mothdex` (owner + SYSTEM + Administrators); load `SDL2.dll` by absolute path from the exe dir only | Major (T-21) |
| TH-32 | Launcher overwrites a user's file with the same name, or install re-run clobbers edits | Low | Low | Atomic write; marker comment `# mothdex-launcher v1`; overwrite only files carrying the marker | Minor (T-21) |
| TH-33 | `install.sh` integrity: binary verified against `checksums.txt` from the same release only protects transit | Low | High | Download to `mktemp -d`, verify SHA-256 before `chmod +x`; verify cosign signature when `cosign` exists; `set -eu`; document "download, read, run" over `curl | sh` | Minor (T-21, supply chain review separate) |

## 5. `Sanitize` specification (owned by T-16, replaces downloader §6 "Minimum")

`Sanitize(raw string) (name string, err error)` is a **pure** function, same rules on every GOOS (F10). In order:

1. **Last component:** split on `/` and `\`; keep the last non-empty element.
2. **Unicode:** invalid UTF-8 → `U+FFFD` → dropped. Normalize to **NFC** (`golang.org/x/text/unicode/norm`).
   Drop runes in categories `Cc`, `Cf` (bidi controls `U+202A–202E`, `U+2066–2069`, zero-width), `Co`, `Cs`, and
   `U+FFFD`. If NFKC of the result contains `/`, `\` or `:`, those runes are replaced by `_` (fullwidth forms).
3. **Forbidden characters:** `< > : " / \ | ? *` and `0x00–0x1F`, `0x7F` → `_`. Collapse runs of `_` and spaces.
4. **Edges:** strip leading `.`, ` `, `-`; strip trailing `.` and ` ` (repeat until stable).
5. **Reserved names (case-insensitive, on the part before the first `.`, after trimming trailing spaces):**
   `CON PRN AUX NUL COM0–9 LPT0–9 COM¹ COM² COM³ LPT¹ LPT² LPT³ CONIN$ CONOUT$` → prefix `_`.
6. **Length:** ≤ 200 bytes UTF-8. Cut the stem on a rune boundary and keep the extension if the extension is
   ≤ 16 bytes; re-run steps 4–5 after cutting.
7. **Empty or only dots/underscores** → fallback `Sanitize(slug) + "." + format` (format matching
   `^[a-z0-9]{1,10}$`, otherwise no extension); if still empty → `download-<jobID>`.
8. **Executable deny-list** (case-insensitive, on the last extension): `sh bash zsh csh ksh fish command run bin`
   *(see note)*, `desktop appimage py pl rb jar`, `exe com scr pif bat cmd ps1 psm1 psd1 vbs vbe js jse wsf wsh hta
   msi msp msc cpl dll lnk url reg scf inf library-ms settingcontent-ms appref-ms`. Match → `ErrUnsafeName`
   (`Failed(unsafe_name)`, exit code from cli-output), unless the run has `--allow-executable`.
   *Note:* `bin` is a legitimate ROM extension (Mega Drive, PS1 `.bin/.cue`) and is **not** in the list;
   `run`/`command` are. Alicerce/Forja confirm no Mothdex `format` in the DB collides with the list (`.exe` for `pc`/`dos`
   is shipped inside zips in the observed data, not verified on the full DB).
9. **Invariants checked at the end (panic in tests, error at runtime):** `name == filepath.Base(name)`,
   `filepath.IsLocal(name)`, no rune from steps 2–3, `len(name) ≤ 200`, `name != "." && name != ".."`.

Idempotence: `Sanitize(Sanitize(x)) == Sanitize(x)` (property test). Display uses the sanitized name; history stores
both raw (escaped) and sanitized names.

## 6. Mitigations mapped to contract sections (summary for Forja)

| Contract § | Required behavior (MUST) | Threats |
|---|---|---|
| downloader §2 | Host check per §TH-16 on every attempt; proxy host: no redirects; `DisableCompression`; no cookie jar; reject non-identity `Content-Encoding` | 16–19, 26 |
| downloader §3 Preflight | `size` sanity and overflow-safe margin; reservation ledger per filesystem (temp and dest); name reservation (casefold NFC); in-flight journal entry | 08, 14, 15, 20 |
| downloader §3 Transferring | `LimitReader(size+1)`; same fd across retries; `0600`; `ENOSPC` → `disk_full`, immediate cleanup | 11, 14, 19 |
| downloader §4 | zip check caps (cumulative, member count, ctx); never extract | 24 |
| downloader §6 | `Sanitize` (§5); platform/folder validation; resolved-path confinement + Windows final-path check; `os.Root` for staging/finalize; `O_EXCL|O_NOFOLLOW`; no-replace finalize; fsync; `--force` only; cleanup rules | 01–15 |
| profiles §2/§4 | Loader rejects folders equal to launcher dirs; refresh URL IP-literal loopback; no proxy/redirect | 02, 06, 22 |
| app-service §3 | Filter non-allowed-host links before ranking; validation at use for cached data | 18, 26 |
| cache §4 / api-client | Cover SSRF guard, magic bytes, decode dimension cap | 23 |
| cli-output | Control-char stripping; CSV formula guard | 27, 28 |
| T-21 installer | Launcher templates, permissions, ACLs, checksum/cosign | 30–33 |

## 7. Contract change requests (sent to Alicerce, ref T-16)

| CR | Severity | Change | Files |
|---|---|---|---|
| CR-1 | **Major** (owner decision) | Record F2. v0.1 = proxy only: filter non-allowed-host links before ranking and show them as unavailable; proxy host gets **0 redirects**; keep `≤3 re-checked https` only for a future explicit host table. `http` never allowed | downloader §2, app-service §3, config §2 |
| CR-2 | **Major** | Platform id regex + membership in `/platforms` before use as folder; `profile.folders` values are single local components; confinement applied to the final dir in every layout | profiles §2/§4, downloader §6 |
| CR-3 | **Major** | Executable deny-list + `--allow-executable` (flag only); profile loader rejects launcher-dir folders | downloader §6, profiles §2, cli-output (new code `unsafe_name`) |
| CR-4 | **Major** | Replace "final file opened with `O_EXCL`" by: staging via `os.Root.OpenFile(O_EXCL|O_NOFOLLOW)`, finalize with `renameat2(RENAME_NOREPLACE)` / `MoveFileExW` without replace, fallbacks as TH-12; in-process name reservation; `rename` mode bounded to 99 | downloader §6, §7 |
| CR-5 | **Major** | Reservation ledger per filesystem (temp + dest), margin `max(5 %, 256 MiB)`; `size ≤ 0` and `> download.max_file_size` (new key, default 256 GiB) rejected; overflow-safe math; fsync file + dir before reporting `Done` | downloader §3, §6; config §2 |
| CR-6 | Minor | Replace §6 "Minimum" with §5 of this document (`Sanitize` spec), including fallback name rules | downloader §6 |
| CR-7 | Minor | `jobID` = 128-bit random base32 (26 chars); `0700/0600`; retry on same fd; cleanup by exact pattern, `Lstat` regular, mtime > 24 h, in-flight journal for destdirs | downloader §3, §6 |
| CR-8 | Minor | `DisableCompression`, reject non-identity encoding, `LimitReader(size+1)`; zip caps `min(64 GiB, max(1 GiB, 20×size))`, ≤ 65 535 members, ctx-aware | downloader §2, §4 |
| CR-9 | **Major** | Cover fetch: https only, block private/loopback/link-local IPs after DNS, redirects re-checked, magic-byte + content-type match, `DecodeConfig` ≤ 4096×4096 | cache §4, api-client |
| CR-10 | Minor | Refresh URL: IP literal `127.0.0.1`/`[::1]` only, `Proxy=nil`, no redirects, body ≤ 64 KiB | profiles §4 |
| CR-11 | Minor | State that B1 validation happens at use in the downloader, not only in the API client; cache dir/file modes | cache §2, downloader §2 |
| CR-12 | Suggestion | Embedded fallback roots (`x/crypto/x509roots/fallback`), no insecure flag ever | api-client §5 |
| CR-13 | **Major** (Windows) | Confinement must not rely on `EvalSymlinks` alone on Windows (F7): verify with `GetFinalPathNameByHandle` on the opened destdir handle | downloader §6 |
| CR-14 | Minor | Strip control/format chars from all API strings in human output; CSV formula guard in exports | cli-output |

New dependencies implied (Sonda/Forja check license and maintenance per base rules): `golang.org/x/text`
(BSD-3, Go team), `golang.org/x/sys` (BSD-3, Go team), `golang.org/x/crypto/x509roots/fallback` (BSD-3, Go team).

## 8. Test cases for Ensaio (T-18)

Fake proxy (`httptest.NewTLSServer`), fake FS where possible, real temp dirs for OS-specific cases. Tests marked
**[W]** run on the Windows CI runner, **[L]** on Linux. All use the injected `Clock`/`Sleeper` (no real sleeps).
Parametrize the name cases in one table test (testing rule: lean parametrization).

**Sanitize (pure, one table test + one property test)**
1. `../../etc/passwd`, `..\\..\\Windows\\x.zip`, `/abs/x.zip`, `C:\x.zip`, `\\srv\share\x.zip`, `dir/sub/x.zip` → base name only, never escapes.
2. `CON`, `con.zip`, `NUL.tar.gz`, `COM1.bin`, `COM¹.zip`, `LPT9`, `CONIN$`, `aux .zip` → prefixed `_`.
3. `game.zip.`, `game.zip ` , `game.zip. . .` → trailing dots/spaces removed.
4. `game.zip:evil`, `x::$DATA`, `a<b>c|d?e*f"g.zip` → no `:` or forbidden chars remain.
5. Control chars `\x00`, `\x1b[31m`, `\x7f`, `\n` inside the name → removed.
6. Bidi/zero-width: `game\u202Ehs.zip`, `ga\u200Bme.zip` → removed; fullwidth `ｇａｍｅ／ｘ.zip`, `a：b.zip` → no separator or colon semantics.
7. NFD `Pokémon` (e + U+0301) and NFC `Pokémon` → identical output.
8. Leading `.`, `-`, ` ` (`.bashrc`, `-rf.zip`) → stripped.
9. 300-byte ASCII name, 300-byte name of 3-byte runes, 300-byte name with 4-byte emoji → ≤ 200 bytes, valid UTF-8, extension kept.
10. Empty, `.`, `..`, `...`, `   `, `\u202E` only → fallback `<slug>.<format>`; malicious slug `../x` and format `zip/../../x` → still safe; empty slug → `download-<jobID>`.
11. Deny-list: `x.sh`, `x.SH`, `x.zip.sh`, `x.desktop`, `x.AppImage`, `x.exe`, `x.bat`, `x.lnk`, `x.ps1` → `ErrUnsafeName`; `x.bin`, `x.cue`, `x.iso`, `x.chd`, `x.zip`, `x.7z` → allowed; with `--allow-executable` the first group is allowed.
12. Property: for random byte strings (fuzz, `testing.F`), output satisfies §5.9 invariants and `Sanitize` is idempotent.

**Destination and confinement**
13. Platform id `../../system`, `ps1/../..`, `PS1`, `ps1 `, `""`, unknown id `zzz` in `per_platform_id` layout → `Failed` before any file is created.
14. `profile.folders.md = "../x"` or `"a/b"` → config/profile load error (exit 2).
15. Folder map or `-o` equal to the profile's launcher dir (`/userdata/roms/ports`, `/storage/roms/ports_scripts`, `<retrobat>\roms\windows`) → rejected by the loader / Preflight.
16. **[L]** destdir is a symlink to a dir outside the root → `dest_outside_root`; same with `-o <that real path>` → succeeds.
17. **[W]** destdir is a junction (`mklink /J`) to a dir outside the root → `dest_outside_root` (guards F7).
18. Pre-existing symlink at the final name pointing to `/tmp/victim` (on_exists `skip`, `rename`, `overwrite`) → victim unchanged in all three; `overwrite` fails because target is not a regular file.
19. Pre-existing symlink/file at the staging name `.<jobID>.mothdex-tmp` → job fails or picks a fresh jobID; nothing outside destdir is touched.

**Overwrite and collisions**
20. `on_exists: skip` with an existing file → `Skipped`, file byte-identical, no `.part` left.
21. Race: a second writer creates the final name after Preflight and before finalize (hook in fake FS or `FS` interface) → existing file preserved, job re-applies `on_exists`.
22. `on_exists: overwrite` from config file or `MOTHDEX_DOWNLOAD_ON_EXISTS` without `--force` → rejected (exit 2); with `--force` → replaced atomically.
23. `rename` mode with `name.zip` … `name (99).zip` all present → `Failed`, no 100th file.
24. Same batch, names `Game.zip` and `game.zip`, and `A:B.zip` and `A_B.zip` → second job is not silently overwritten (renamed or skipped per `on_exists`), both outcomes in history.
25. **[W]** and **[L] on a case-insensitive fs if available**: existing `GAME.ZIP`, download `game.zip` with `skip` → `Skipped`.

**Network and URL**
26. URLs: `http://proxy.mothdex.net/…`, `https://proxy.mothdex.net:8443/…`, `https://proxy.mothdex.net@evil.test/…`, `https://evil.test/…`, `https://127.0.0.1/…`, `https://[::1]/…`, `https://proxy.mothdex.net./…` (accepted after normalization), `https://PROXY.MOTHDEX.NET/…` (accepted), `https://proxy.mothdex.net.evil.test/…`, `https://xn--…/…` → `untrusted_url` except the two noted; no TCP connection made for rejected ones (fake dialer counts dials).
27. Proxy host answers `302` to an allowed host and to `evil.test` → both `Failed(untrusted_url)`, not retried.
28. Link from a non-allowed host (archive.org fixture) is excluded from ranking; `download --link n` pointing to it → `untrusted_url` with a clear message.
29. Cached `entry` JSON edited to carry a malicious `filename` and `url` → same rejections as from the network (validation at use).
30. Server sends `Content-Encoding: gzip` body → failure, file never finalized; request carried `Accept-Encoding: identity`.
31. (T-27) `Content-Length` missing, chunked, `0`, or malformed (`+5`, ` 5`, `5.0`) → `size_mismatch`, not retried, no body byte read; body of `cl+1` bytes → abort at byte `cl+1`, `.part` removed; short body → truncated, retried. `Content-Length` ≠ `link.size` (half and double) → `Done`, both values in the record, one drift warning.
32. `link.size` = 0, −1, `math.MaxInt64`, `> download.max_file_size` → `Failed` in Preflight, no connection.
32b. (T-27) `Content-Length > download.max_file_size` with a small `link.size` → `size_mismatch` before the first body byte.
32c. (T-27) Attempt 1 truncated with `Content-Length` N, attempt 2 answers N+1 → `size_mismatch`, no third attempt.
32d. (T-27) `confirm_downloads` on, `link.size` 5 MB, `Content-Length` 2 GB → `confirmation_required` before the first body byte; re-queued with `SizeConsent` = 2 GB → `Done`; with `SizeConsent` = 2 GB but `Content-Length` 3 GB → `confirmation_required` again; `-f` (`ConsentAny`) → `Done`.
32e. (T-64) `confirm_downloads` on, `link.size` already above 500 MB, `Content-Length` = 2 × `link.size` → `Done`; 2 × `link.size` + 1 → `confirmation_required` before the first body byte; `-f` (`ConsentAny`) or `SizeConsent` = `Content-Length` → `Done`.
33. TLS with a self-signed cert → fails; no code path disables verification (grep test is **not** allowed; assert via behavior).

**Disk, partials, cleanup**
34. Fake FS returns `ENOSPC` mid-stream → `Failed(disk_full)`, not retried, `.part` gone, reservation released.
35. Two jobs of 60 % of free space each with concurrency 2 → second fails in Preflight (`disk_full`), not mid-stream.
    (T-27) `link.size` 1 % of free space, `Content-Length` 90 %, a second job holding 20 % → `disk_full` before the first body byte; a smaller `Content-Length` shrinks the claim (the second job then fits).
36. Temp and dest on different filesystems (fake device ids) with enough space in temp but not in dest → `disk_full` in Preflight.
    (T-27) The same split reached only through `Content-Length` (small `link.size`) → `disk_full` after the headers, before the body.
37. EXDEV path: rename returns `EXDEV` → copy to staging, fsync called (spy), rename within destdir; a crash injected after copy leaves only the staging file, never a partial final name.
38. Cancel during Transferring and during Verifying → `.part`/staging removed, `Canceled`, one history record.
39. Startup cleanup: `.part` files with mtime 25 h (deleted), 23 h (kept), a symlink named like a `.part` (not followed, not deleted target), `notes.part` (kept), a dir named `x.part` (kept), staging file in a destdir from the in-flight journal (deleted when old).
40. Retry after truncation reuses the same fd: replace the `.part` path with a symlink during RetryWait (fake FS hook) → the symlink target is never written.

**Verification**
41. Zip with a member whose data exceeds its declared size → `integrity`; overlapping-member bomb (e.g. 1 MiB file inflating past the cumulative cap) → stops at the cap, `integrity`, within a bounded number of bytes read; zip with 70 000 empty members → stops at member cap.
42. Cancel `ctx` during the zip CRC check → returns promptly, `Canceled`.
43. Non-zip file starting with `PK\x05\x06` (empty zip) or random bytes named `.zip` → CRC step skipped/handled by magic bytes, history records `crc: "skipped"` or failure as specified.

**Refresh hook and covers**
44. Profile override with refresh URL `http://localhost:1234/…`, `http://127.0.0.1.nip.io:1234/…`, `http://192.168.1.10:1234/…`, `https://127.0.0.1:1234/…` → profile load rejected; `http://127.0.0.1:1234/reloadgames` and `http://[::1]:1234/reloadgames` accepted.
45. Refresh server replies `302` to another host, or a 10 MiB body, or hangs → no redirect followed, ≤ 64 KiB read, returns within 3 s (injected clock), warning only; `HTTP_PROXY` set → not used.
46. Refresh called once per batch only when ≥ 1 `Done`; never when all `Skipped`/`Failed`.
47. Cover URL `http://…`, `https://192.168.0.1/x.png`, a public host whose DNS resolves to `127.0.0.1` (fake resolver), redirect to private IP → not fetched; job still `Done`.
48. Cover served as `image/png` with HTML/SVG bytes, or a PNG header declaring 60 000 × 60 000 → rejected, not stored.

**Output**
49. Entry title/filename containing `\x1b]52;c;…\x07` and `\x1b[2J` → human output contains no ESC byte; `--json` output escapes it.
50. History/search CSV export with a filename `=HYPERLINK("http://x")` → cell begins with `'`.

**Installer (Guindaste T-21, listed here for traceability)**
51. Launcher generated for a `bin_dir` with a space and with `$(id)` in the path → refused (validation) or quoted so that `sh -n` passes and the literal path is executed.
52. Launcher files are mode `0755`, owned by the installer user, dir not world-writable; re-install keeps a user-modified launcher without the marker.

## 9. Residual risk (after all CRs)

- **Malicious ROM content** (TH-25): we verify transport integrity, not authenticity; emulators parse the files.
  Rated Med likelihood / Med impact. Upstream hashes (U3/U4) are parked by the maintainer
  (cavv-dev/mothdex-api#1), so no reduction is expected. Since T-27 the exact length is checked against the proxy's
  own `Content-Length`, not the catalog, so a substitute of a different size is no longer caught either.
- **Local attacker with write access to the ROM tree** (SMB share, shared desktop): can race directory components
  between the confinement check and `os.Root` open; with share write access they already have equal power on the
  device. Low.
- **Filesystems without no-replace rename and without hard links** (some FUSE mounts): fallback leaves a narrow
  race that only a local attacker can win. Not verified on devices.
- **Windows ACLs and junctions**: depend on install location and CR-13; not verified on a real Windows/RetroBat box.
- **Upstream host mix may change** (F2 inferred from source, not from live API responses); T-10 fixtures should sample
  real links per source type before v0.1.

## 10. Verdict

**Blocking findings in scope for T-17:** CR-1, CR-2, CR-3, CR-4, CR-5, CR-9, CR-13 (Major). Minor CRs block too,
because the item is High risk (risk-process). T-17 may start once Alicerce updates the contracts or Regente records a
decision for each CR.
