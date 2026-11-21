# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `pkg/mothdex`: `KindUntrustedRedirect` (`untrusted_redirect`, exit 4) and `IsUntrustedRedirect`. A redirect to another
  origin (host, scheme or effective port), a fourth hop, or a 3xx without a usable `Location` is refused, not retried,
  does not open the rate-limit circuit and is never `KindNetwork`; the message names the target host only (T-10c).

### Changed

- Downloader contract (T-17 architecture pass): same-device staging inside the destdir (no double write), the
  batch keeps finalized names so `overwrite` never replaces them, `Retry-After` capped at 15 min, absolute
  `Record.Path`, and what the app service needs from the downloader (ids at submission, per-job cancel).
- Downloader contract: within one batch, a name collision under `on_exists: overwrite` is resolved by rename,
  never by replacing another job's file; the injected ports (`FS.Open`, `FS.SyncDir`, `Journal`, `History`) and the
  history `Record` are documented. `download` JSON gains `raw_filename` and `hash` (additive).
- System profiles map 40 to 48 Mothdex platform ids per frontend (Batocera, EmuELEC, RetroBat, EmuDeck, RetroDECK,
  Recalbox) from the T15b research; arcade ids (D18) and `pc` (D19) stay unmapped on purpose. Contracts document the folder
  rules and that the launcher-folder check is lexical and is repeated after path resolution.
- API client contract: a refused redirect (another origin, a scheme change or more than 3 hops) is the
  non-retryable kind `KindUntrustedRedirect` (error code `untrusted_redirect`, exit 4), never a network error.
- Profiles contract: probe type `files`, the `${MATCH}` variable and the `folders_verified` flag (T-15).
- Cache contract documents the `AllowStale` and `WithTTL` options and the best-effort rename retry on Windows.
- API client and config contracts: `http` base URLs are accepted only for loopback IP literals, not `localhost`.
- Contracts: `version` output shape (`version`, `commit`, commit `date`, `go`, `os`, `arch`; empty in dev builds)
  and the release-artifact contract for `install.sh` (archive names, 9 targets including armv6, `uname` mapping,
  checksum and optional cosign verification).
- Config contract records the T-11 decisions: portable paths apply with any config file, config errors carry no
  `file:line`, `config reset` on a missing file writes only `version: 1`, and `mothdex.ValidateBaseURL` is the only
  rule for `api.base_url`.
- Downloader and related contracts updated with the 14 change requests from the T-16 threat model: proxy-only URL
  policy, `Sanitize` spec and executable deny-list, no-replace finalize, space reservation ledger, zip caps, cover fetch
  SSRF guard, refresh URL policy, platform-id and folder validation, Windows junction-safe confinement. New config key
  `download.max_file_size`, new error code `unsafe_name`, new flag `--allow-executable`.

### Added

- T-18: tests for T-16 cases 14, 15 (loader side), 22 (configuration half), 44 and 45 in `internal/config` and `internal/library`; the catalog marks 14, 44 and 45 as covered.
- `internal/library` and `profiles/*.yaml`: system profiles for Batocera, EmuELEC, Recalbox, Lakka, RetroBat, EmuDeck,
  RetroDECK and generic. Detection is a pure function over an injected file tree (priority, ambiguity and any doubt fall
  back to generic with a warning), `DestDir` with platform-id and folder confinement, folder overrides, unverified-item
  reporting, and the refresh hook restricted to `http://127.0.0.1:1234/reloadgames` or `[::1]` (3 s, no proxy, no
  redirects, non-fatal). Only the 15 platform ids confirmed in the T1e research are mapped to folders; the rest stay
  unmapped until sourced. A `profile.rom_root` (or any `DestDir` result) inside a launcher folder is refused
  (`Profile.IsLauncherPath`, per component and look-alike aware).
- `internal/testutil`: `fakeresolver` (scripted DNS), `zipfix` (zip bomb, overlapping-member, declared-size and member-count
  fixtures) and `realfs` (real-directory helpers: links and Windows junctions, tree snapshot, case-sensitivity probe) for
  the downloader tests (T-18). gosec is not run on `internal/testutil`.
- `internal/proxy`: the downloader (T-17, proxy links only, D16). `Sanitize` for server-supplied names (same rules on every
  OS, executable deny-list, fallback name), URL policy (https, exact allowed host, no user info, port 443, no IP or IDN),
  confinement of the destination directory per component and after the OS resolved links (`GetFinalPathNameByHandle` on
  Windows), a real FS over `os.Root`, a no-replace finalize per OS (Linux `renameat2`, macOS `renameatx_np`, Windows
  `MoveFileExW`) with link and placeholder fallbacks and an EXDEV copy, `on_exists` (skip, rename up to 99, overwrite
  only with `--force`), a per-filesystem space ledger, name reservation, in-flight journal and startup cleanup, retry
  policy with injected clock and sleeper, zip CRC and upstream hash checks with caps, fsync of file and directory before
  `Done`, and one history record per job. Adds `golang.org/x/text` and `golang.org/x/sys`.
- T-18 audit round: case 39 ages the symlink target (a following cleanup would delete it), case 24 asserts casefold collisions on every file system, a finalize fallback chain test (hard link, O_EXCL placeholder, second writer in each step) and a launcher-behind-a-link test on a real detected profile.
- T-18: `fakefs` counts reads (`OpRead`, `BytesRead`); the case 31, 32, 41 and 42 tests now prove abort at size+1, the exact size limit, bounded verification reads and a stop right after a cancel.
- T-18: tests for 40 of the 52 T-16 threat-model cases against the downloader (`internal/proxy/cases_*_test.go`):
  URL policy, redirects, gzip, sizes, TLS, platform injection, symlink and junction destinations, launcher folders,
  on_exists races, disk full, cross-device copy, cancel, startup cleanup, zip bombs and the Sanitize invariants
  (property and fuzz). The catalog marks each case with its test; the other 12 belong to other packages.
- `internal/testutil/fakefs`: `proxy.FS` and `proxy.Finalizer` doubles over a confined temp root with fault injection
  (ENOSPC mid-write, EXDEV, unsupported no-replace rename and hard link), scripted free space and device ids, hooks
  between steps, a call log and a Sync spy, for the downloader disk, partial-file and race cases (T-18).
- `internal/cache`: on-disk cache (search, entry, meta, info, covers, session, state) with canonical request keys,
  per-namespace TTL, sidecar metadata, stale reads, a 200 MB cap with expiry-first then LRU eviction, atomic writes,
  an injected clock and private file modes (0700/0600).
- `internal/testutil/fakeproxy` and `internal/testutil/fakeclock`: scripted TLS fake of the download proxy (truncate,
  stalls, transient 404, 429, Content-Length mismatch, gzip, redirects, malicious Content-Disposition, oversized body)
  driven by a fake clock, plus a catalog of the 52 downloader test cases from the T-16 threat model, all pending until
  the downloader exists.
- `internal/config`: typed configuration with the precedence defaults < YAML file < `MOTHDEX_*` environment < flags
  (per key, with the source of each value), closed schema v1 validation (types, ranges, `download.max_file_size`,
  `download.prefer.size`, `profile.*`), OS default paths and portable mode, and atomic `set`/`reset` of the YAML file.
- `mothdex` binary embeds fallback CA roots (`x509roots/fallback`) for systems without a CA bundle; `pkg/mothdex` has
  no option to disable certificate verification, and `WithHTTPClient` callers own their TLS policy.
  `info.error` messages lose Unicode control and format characters at parse time (defense in depth).
- `pkg/mothdex`: client for the Mothdex API v1 (search, entry, random, platforms, regions, info) with typed
  errors (HTTP 200 `info.error` included), client-side rate limit (5 req/s), 429 handling, a breach circuit,
  bounded retries with backoff, per-attempt timeouts and context cancellation. Recorded API fixtures live in
  `pkg/mothdex/testdata/api-v1/`; `tools/record-fixtures` re-records them (developer tool, not shipped).
- Design documentation in `docs/`: contracts for the API client, app service, downloader, cache, config,
  system profiles and machine output; ADR-0001 (stack) and ADR-0002 (downloader without `Range`); UX
  conventions (Shell, TUI, `--prefer`); research notes. Adds the config key `download.prefer.size`.
- Release pipeline: GoReleaser OSS configuration for 9 targets (linux
  amd64/arm64/armv7/armv6/386, windows amd64/arm64, darwin amd64/arm64; static,
  trimpath, reproducible timestamps), `checksums.txt`, SBOMs, keyless cosign
  signature, GitHub artifact attestations, a tag-only release workflow
  (semantic-version tags on `main`, per-job permissions, no dependency cache,
  signature and attestation verification before publishing) and a snapshot check
  on pull requests touching it (`scripts/verify-dist.sh`, double build). Release
  tools are pinned in `tools/*/go.mod`. Nothing is published to package managers.
  Artifact naming, verification and owner-side settings are in `docs/release.md`.
- `internal/buildinfo`: version, commit and commit date of the binary
  (ldflags, with a `runtime/debug` fallback for `go install` builds);
  `mothdex` prints them.
- Project skeleton: Go module, package layout with placeholders (including `internal/app`), minimal
  `mothdex` binary that prints its version.
- GPL-3.0 license, governance documents, issue and pull request templates.
- CI for pull requests and `main` (golangci-lint, `go test -race` on Linux,
  Windows and macOS, govulncheck, gitleaks) with actions pinned by commit SHA.
- Dependabot for Go modules and GitHub Actions.

### Fixed

- Config `profile.folders` values reject launcher-folder look-alikes (`portſ`, `wındows`) and the full set of Windows
  device names (`COM0`, `LPT0`, superscript digits); `api.base_url` rejection messages (load and `set`, client validation) never echo the URL userinfo, and a URL that does not parse is
  replaced by a placeholder; the API client
  accepts `http` only for loopback IP literals, not `localhost`.

[Unreleased]: https://github.com/Or4cu1o/mothdex-cli/commits/main
