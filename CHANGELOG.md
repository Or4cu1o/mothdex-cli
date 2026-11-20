# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Project skeleton: Go module, package layout with placeholders, minimal
  `mothdex` binary that prints its version.
- GPL-3.0 license, governance documents, issue and pull request templates.
- CI for pull requests and `main` (golangci-lint, `go test -race` on Linux,
  Windows and macOS, govulncheck, gitleaks) with actions pinned by commit SHA.
- Dependabot for Go modules and GitHub Actions.
