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
