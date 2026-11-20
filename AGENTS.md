# AGENTS.md

Guidance for humans and coding agents working in this repository.

## Project

`mothdex-cli`: Go client for the Mothdex API v1. Status: pre-alpha skeleton.
Architecture rule: every interface (`internal/ui/{cli,shell,tui,gui}`) is a thin
layer over a single app service; no business logic lives in the UI.

## Commands

```sh
go build ./...                                  # build
go run ./cmd/mothdex                            # run (prints version)
golangci-lint run                               # lint (config: .golangci.yml, v2)
go vet -tags gui ./cmd/mothdex-gui              # the gui build tag is skipped by default builds
go test -race ./...                             # tests
govulncheck ./...                               # known vulnerabilities (CI pins v1.10.0; golangci-lint v2.15.0)
gitleaks git --pre-commit --staged --redact     # secrets scan before every commit
goreleaser check && goreleaser release --snapshot --clean --skip=sign   # release dry run (tools pinned in tools/*/go.mod)
scripts/verify-dist.sh dist                     # checks the 9 archives, SBOMs, checksums, binaries
```

## Rules

- Branch `type/short-name`; Conventional Commits; no direct push to `main`.
- Never write, print or commit secrets or `.env`; use `.env.example`.
- CHANGELOG entry under `Unreleased` in every PR; update README when structure changes.
- GitHub Actions are pinned by full commit SHA with a version comment; the
  workflow `GITHUB_TOKEN` stays read-only unless a job needs more.
- Release archive names and the `uname -m` mapping are a contract: see docs/release.md.
- Tests: see CONTRIBUTING.md. Never call the real API from tests.

## Risk map

The riskiest file in a change sets the level of the whole change.

| Surface | Level |
|---|---|
| Docs, locales, styles, README | Low |
| Commands, cache, i18n, TUI/GUI | Medium |
| File writes with a name coming from the server or an LLM (downloader) | **High** |
| CI workflows (`.github/workflows`), Dependabot, release, signing, publishing to package managers, tokens | **High** (supply chain and secrets) |
| Archiving repositories, merges, releases, publications | external action: needs owner approval |
