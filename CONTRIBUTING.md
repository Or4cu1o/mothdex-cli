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

Install Go (version in `go.mod`), [golangci-lint](https://golangci-lint.run/) v2,
[govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) and
[gitleaks](https://github.com/gitleaks/gitleaks), then run:

```sh
golangci-lint run
go test -race ./...
govulncheck ./...
gitleaks git --pre-commit --staged --redact
```

## Tests

Test behavior that matters: security-sensitive paths, file writes, contracts
with the API, and pure decision rules. Add one regression test per bug. Do not
add snapshot tests or tests that depend on the real clock or on `sleep`.
Tests never call the real API.

## License

By contributing you agree that your contributions are licensed under GPL-3.0.
