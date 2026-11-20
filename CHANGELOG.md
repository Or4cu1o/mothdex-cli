# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Downloader and related contracts updated with the 14 change requests from the T-16 threat model: proxy-only URL
  policy, `Sanitize` spec and executable deny-list, no-replace finalize, space reservation ledger, zip caps, cover fetch
  SSRF guard, refresh URL policy, platform-id and folder validation, Windows junction-safe confinement. New config key
  `download.max_file_size`, new error code `unsafe_name`, new flag `--allow-executable`.

### Added

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
- Project skeleton: Go module, package layout with placeholders (including `internal/app`), minimal
  `mothdex` binary that prints its version.
- GPL-3.0 license, governance documents, issue and pull request templates.
- CI for pull requests and `main` (golangci-lint, `go test -race` on Linux,
  Windows and macOS, govulncheck, gitleaks) with actions pinned by commit SHA.
- Dependabot for Go modules and GitHub Actions.

[Unreleased]: https://github.com/Or4cu1o/mothdex-cli/commits/main
