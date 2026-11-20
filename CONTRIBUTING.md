# Contributing

Thanks for helping. The project is pre-alpha; open an issue before starting
anything large.

## Branches and commits

- Branch from `main` as `type/short-name` (for example `fix/path-sanitizer`).
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/):
  `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`,
  `chore`, `revert`, with an optional scope (`fix(proxy): ...`).
- Never push directly to `main` and never force-push shared branches. Changes
  land through pull requests.
- One commit per complete unit of work.

## Pull requests

- Add an entry under `Unreleased` in [CHANGELOG.md](CHANGELOG.md) in the same PR.
- Update `README.md` if structure or behavior changes.
- CI must be green. Do not commit secrets, tokens or `.env` files.

## Local checks

Install Go (version in `go.mod`), [golangci-lint](https://golangci-lint.run/) v2.15.0,
[govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) v1.10.0 (the
versions CI uses) and [gitleaks](https://github.com/gitleaks/gitleaks), then run:

```sh
golangci-lint run
go vet -tags gui ./cmd/mothdex-gui
go test -race ./...
govulncheck ./...
gitleaks git --pre-commit --staged --redact
```

## Release dry run

Changes to `.goreleaser.yaml` or the release workflows are checked by CI with a
snapshot build. To run it locally, install the pinned tools (goreleaser, cosign,
syft; versions in `tools/*/go.mod`) and run:

```sh
for tool in goreleaser cosign syft; do GOBIN="$PWD/.bin" go -C "tools/$tool" install tool; done
PATH="$PWD/.bin:$PATH" goreleaser release --snapshot --clean --skip=sign
```

Releases are cut by pushing a `v*` tag from `main`, which needs the owner's approval.

## Tests

Test behavior that matters: security-sensitive paths, file writes, contracts
with the API, and pure decision rules. Add one regression test per bug. Do not
add snapshot tests or tests that depend on the real clock or on `sleep`.
Tests never call the real API.

## License

By contributing you agree that your contributions are licensed under GPL-3.0.
