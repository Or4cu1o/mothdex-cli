# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- API client contract: a refused redirect (another origin, a scheme change or more than 3 hops) is the
  non-retryable kind `KindUntrustedRedirect` (error code `untrusted_redirect`, exit 4), never a network error.
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
