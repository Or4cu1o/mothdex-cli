# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `internal/ui/cli` and `cmd/mothdex` (T-20b): the command line. cobra commands `search`, `info`, `random`, `download`,
  `boxart`, `platforms`, `regions`, `db-info`, `history`, `cache stats|clear`, `config list|get|set|reset`,
  `profile show|detect` and `version` (`shell`, `tui`, `gui` print "not available yet"). Human tables with the 80/60
  column rules, the `--json` envelope v1 (partial data on a batch), the localized error block from
  `errors.<code>.message|hint` (a placeholder with no value falls back to the service text), the exit codes of
  cli-output section 4 (7 for a partial batch, 130 for Ctrl+C; a second Ctrl+C within 2 s exits at once), `--prefer`,
  `--link`, `-o` (becomes the absolute root), `-e/--export` with the CSV formula guard, and the confirmation prompt rules
  (never with `--json`, `-q` or a piped stdin). Every API string passes `SanitizeText` before a terminal (case 49);
  `--json` keeps raw values. `cmd/mothdex/compose.go` is the composition root (config, log, i18n with `LoadDir`, cache,
  API client with the persisted 429 cooldown, cover fetcher, history, profile, app). `app.Service.SaveBoxart` writes
  covers through the confined destination root (O_EXCL, launcher folders refused, an existing file is kept). Progress is
  one line per state change on stderr; a live redrawn bar is not done yet. Review round: `--json` escapes C1 controls
  (CSI/OSC), `error.exit_code` is the process code and `warnings[]` is filled, failure messages carry their named values
  (`path`, `name`, `host`, ...) and never show a raw `{placeholder}`, export write failures are exit 6 with a `--force`
  hint, the cover of a downloaded file is named after the file (`SaveJobBoxart`) and is staged then published, a new
  cover fetch leader re-checks the cache, the persisted 429 cooldown seed is clamped to 10 minutes, `CSVCell` also strips
  U+2028/2029, and the UI imports only `app` (export writers and type aliases moved behind it). Catalog case 49 is
  now covered.
- `docs/contracts/cli/ui.md` (T-40): the E-M2 Shell and TUI contract: packages and imports, `cli.Runner` for the Shell, launchers and an interrupt hand-off in `cli.Deps`, service calls per screen, pinned liner v1.2.3 and Charm v2 versions, test plan, and the app gaps for Forja.
- CI job `release tools build` compiles the pinned release tools (goreleaser, cosign, syft) from `tools/*` on every PR, the
  way `release.yml` installs them, so a dependabot bump there that no longer builds is caught before a tag (PR-31-sec).
- T-21c: installer hardening from the #31 reviews. `install.sh`: no `latest` through wget (a redirect could roll it back; pass `--version`), `dir_safe` also refuses a bin_dir with a world-writable non-sticky ancestor (physical path), the FAT exception resolves symlinked parents and stays inside the profile tree. `install.ps1`: a download whose final address is not https is refused (PowerShell 5.1 follows https to http). Tests: parent, sticky, symlinked parent and wget-latest cases; Pester cases for an inherit-only generic ACE and a real http response.
- `docs/qa/T-22-m1-acceptance.md`: the M1 acceptance checklist (T-22) with the evidence source per item, what waits for
  T-20b, and the plan for the mcp slug charset sample.
- `internal/app` (T-20a): the app service behind the four UIs. Catalog: search with a saved session, entry, random,
  platforms, regions, db-info, link ranking with the downloader URL policy applied first (case 28), covers through
  the hardened fetcher (singleflight, per-session negative cache), stale cache fallback (7 d). Downloads: `Enqueue`
  validates every request before queueing any, a Downloader per destination root (`-o` or `download.dir` becomes the
  absolute root; otherwise the profile ROM root), startup `Cleanup`, journal, `Confiner` with the profile launcher
  paths, `Jobs`/`Events`/`Cancel`/`Wait`, the frontend refresh once per batch and only after a Done job (case 46),
  and the history record with title, link index, expected size and error object. Config read path that works on a
  file holding `on_exists=overwrite`; the overwrite-without-force refusal is `invalid_config` (exit 2). `*app.Error`
  has its whole text cleaned of terminal escapes and maps to the exit codes (case 49, service half).
- `internal/i18n` and `locales/`: embedded locales plus an optional external folder (per-key override), automatic
  detection (`LC_ALL`, `LC_MESSAGES`, `LANG`; the Windows preferred UI languages), `interface.language` resolution
  (exact, same language, en_us), named `{placeholders}`, CLDR plural forms, and a per-key fallback to en_us. `en_us`,
  `pt_br`, `es_es` and `ru_ru` are complete (the `errors.<code>.message` and `.hint` table of cli-output section 4 and
  the UI basics); `zh_cn`, `ja_jp` and `hi_in` translate the UI basics and fall back for the rest. A test fails when a
  locale lacks keys it promises, defines keys en_us lacks, or changes placeholders.
- `internal/log`: the slog logger (T-14). A size-rotated file `mothdex.log` under `paths.logs` (`log.max_size_mb`,
  `log.max_files` in total, created with mode 0700/0600) and an optional stderr copy (`log.console`), from the `log.*`
  settings. Standard library only. A log file that cannot be opened never stops the program: `log.New` returns a
  usable logger plus an error to show once as a warning.
- `internal/history`: JSONL transfer history (`Append`, `List` newest first with a `--failed` filter, damaged lines
  skipped) and export to txt (120 columns), json (the `--json` envelope) and csv (RFC 4180, control and format
  characters stripped, formula guard); `ExportTarget` and `WriteExport` give directory naming, format inference and an
  atomic no-overwrite write. `internal/session`: the last search in the `session` namespace, `Resolve(index)` with
  `no_session` and `index_out_of_range`. Case 50 covered.

- `pkg/mothdex`: `KindUntrustedRedirect` (`untrusted_redirect`, exit 4) and `IsUntrustedRedirect`. A redirect to another
  origin (host, scheme or effective port), a fourth hop, or a 3xx without a usable `Location` is refused, not retried,
  does not open the rate-limit circuit and is never `KindNetwork`; the message names the target host only (T-10c).

### Changed

- App service contract (T-20b): `SaveJobBoxart` (cover named after the downloaded file) and `SaveBoxart` (`boxart`
  command) replace `DownloadRequest.Boxart`; exports go through `app`; UIs may also import `internal/i18n` and
  `internal/buildinfo`.
- Contracts: one slug rule for both repos (`^[a-z0-9][a-z0-9-]{0,255}$`, `mothdex.ValidSlug`). `Entry` refuses other
  slugs locally, search drops entries with an unsafe slug before numbering (`dropped` in `search` JSON, a warning), and a
  single-entry answer with one is `api_incompatible`.
- T-16 catalog: cases 51 and 52 (installer launcher path injection and permissions) are now Covered by
  `tests/installer/cases.sh`, `nonroot.sh` and `install.Tests.ps1`; 51 of 52 cases are Covered, only 49 (terminal
  escapes in the UI output) is Pending.
- Contracts follow the T-20a fixes and the T-21 installer: `Events` is never lossy (per-subscriber queue), the
  journal entry carries its destination root and cleanup is per root, a failed session save deletes the stored
  session, and the installer launcher is opt-in (D22) with the root-only FAT share exception (TH-31).
- App service contract (T-20a review): capability method names as implemented, UI packages import only `app`
  (aliases for internal types), one downloader per destination root, and the service writes boxart.
- Release and profile contracts follow the T-21b installer: `checksums.txt` also lists SBOMs, the exact-line
  checksum rule, the `install.ps1` architecture mapping, and the RetroBat `bin_dir` in `%LocalAppData%`.
- CLI conventions §7: English error strings for `invalid_request`, `unmapped_platform`, `untrusted_redirect` and
  `unsafe_name`; `path_rejected` now describes a refused destination, not a file name (that is `unsafe_name`).
- Contracts: `Service.Cover` returns the cached reader and content type (not a path), `internal/cover` owns the
  cover DNS port (the downloader `Resolver` port is dropped from downloader §8), and cache §4 says how T-20 wires it.
- `internal/config`: `download.on_exists=overwrite` from the config file or `MOTHDEX_DOWNLOAD_ON_EXISTS` is rejected
  (exit 2) unless `Options.Force` (`--force`) is set; case 22 is covered.
- `internal/proxy`: `finalize` and `Cleanup` split into smaller steps, `FS.Open` returns a `ReadFile` with `Stat`
  (no type assertion), `Result.Path` documented as absolute; test helpers use `bytes.Reader`, the memory-backed
  directory test asserts on `/dev/shm` when it is tmpfs, and stale `dest_outside_root` wording is gone.
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

- T-21b: `install.sh` (POSIX sh; Batocera, EmuELEC, Recalbox, Lakka, generic Linux, macOS) and `install.ps1`
  (Windows, RetroBat). Downloads only from the GitHub release of `Or4cu1o/mothdex-cli` over https, always verifies
  SHA-256 from `checksums.txt`, and also `cosign verify-blob` or `gh attestation verify` when the tool is present
  (a present tool that fails aborts). `--version` pinning, `--dry-run`, `--uninstall`, profile `bin_dir`, a fixed
  launcher template written only to the profile launcher folder (path allow-list, marker, never overwrites a
  user's file). Tests: shellcheck, 193 container checks (Alpine and Debian, no network, simulated Batocera and
  EmuELEC trees, fake release, a second pass as a non-root user) and Pester-style checks for `install.ps1` in CI (`installer.yml`). The launcher is opt-in (`--launcher`, `-Launcher`) in v0.1 because `mothdex tui` does not exist yet;
  wget is used only if it is GNU Wget and a signature tool (cosign or a logged-in gh) is present, because wget can follow an https to http redirect (busybox wget does not verify TLS and is refused); on exFAT/FAT/NTFS share partitions of Batocera
  and Recalbox, root installs get a documented, warned exception to the world-writable refusal.
- `internal/cover`: cover fetch hardening (T-16 CR-9, cache.md section 4): https only, no connection to loopback,
  private, link-local, CGNAT, multicast or special addresses (checked after DNS and again in the dialer Control hook),
  at most 3 redirects each re-checked, 5 MiB cap, Content-Type must agree with the magic bytes (jpeg, png, webp, gif;
  SVG refused), header-only size check of at most 4096 x 4096. Violations are a cache miss. Cases 47 and 48 covered.

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
- T-18 audit round: case 39 ages the symlink target (a following cleanup would delete it), case 24 asserts casefold collisions on every file system, a finalize fallback chain test (hard link, O_EXCL placeholder, second writer in each step) and a launcher-behind-a-link test on a real detected profile. The rename-policy subtest runs in both the hard link and the placeholder step, with titles that name the step.
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

- `internal/cache` on Windows: readers open cache files with `FILE_SHARE_DELETE`, and the replace uses `FileRenameInfoEx` with `FILE_RENAME_POSIX_SEMANTICS` (Windows 10 1809, NTFS; `os.Rename` falls back where unsupported), because `MoveFileEx` still refuses a target with an open handle. A `Put` renaming a new version over an item that a reader holds open no longer fails with "Access is denied". The bounded rename retry could not beat a busy reader (`TestConcurrentGetAndPutKeepTheLatestItem` failed after 59 s on windows-latest); the retry stays as a second line of defence. Regression test `TestPutReplacesAnItemThatAReaderHoldsOpen` holds a reader open across the Put, with no waits.
- `internal/app` test `TestCoverFromCacheThenFetchSingleflightAndNegativeCache` no longer fails about 1 run in 40 under `-race`: the gate opens only after all five callers joined the single fetch (a join counter on the in-flight call replaces the sleep polling).
- Config `profile.folders` values reject launcher-folder look-alikes (`portſ`, `wındows`) and the full set of Windows
  device names (`COM0`, `LPT0`, superscript digits); `api.base_url` rejection messages (load and `set`, client validation) never echo the URL userinfo, and a URL that does not parse is
  replaced by a placeholder; the API client
  accepts `http` only for loopback IP literals, not `localhost`.

[Unreleased]: https://github.com/Or4cu1o/mothdex-cli/commits/main
