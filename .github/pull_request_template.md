## What and why

<!-- One or two sentences. Link the issue or task ref. -->

## Risk level

- [ ] Low (docs, text, styling, config values)
- [ ] Medium (bug fix or change of existing behavior)
- [ ] High (file writes from server-provided names, CI/release, secrets, supply chain)

## Checklist

- [ ] Branch is named `type/short-name`; commits follow Conventional Commits
- [ ] A changelog fragment `changes/<branch>.md` is added (not an edit of `CHANGELOG.md`), or this PR needs no entry
- [ ] `golangci-lint run`, `go test -race ./...` and `govulncheck ./...` pass locally
- [ ] No secrets, tokens or `.env` files in the diff
- [ ] README/docs updated if behavior or structure changed
