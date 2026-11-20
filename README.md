# mothdex-cli

[![CI](https://github.com/Or4cu1o/mothdex-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/Or4cu1o/mothdex-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Or4cu1o/mothdex-cli?include_prereleases)](https://github.com/Or4cu1o/mothdex-cli/releases)
[![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/Or4cu1o/mothdex-cli)](go.mod)

Command-line client for the [Mothdex](https://api.mothdex.net) API.

> **Status: pre-alpha.** Only the project skeleton exists. The binary prints its
> version and nothing else. Everything listed below is **planned**, not
> implemented.

## Planned features

- [ ] Search, entry details, random pick, platforms and regions (API v1)
- [ ] Resilient downloader: parallel files, retry with backoff, size validation, atomic move
- [ ] Cache with TTL and size limit, request history with export
- [ ] Interactive shell, terminal UI and gamepad-driven emulator mode
- [ ] Per-system profiles (Batocera, Recalbox, EmuELEC, Lakka, RetroBat, Bazzite, generic)
- [ ] Localization (`en_us`, `pt_br`, `es_es`, `ru_ru`)
- [ ] Stable `--json` output and documented exit codes
- [ ] Release binaries for Linux, Windows and macOS with checksums

## Build

Requires the Go version declared in [`go.mod`](go.mod).

```sh
go build -o mothdex ./cmd/mothdex
./mothdex
```

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).
Project history is in [CHANGELOG.md](CHANGELOG.md).

## License

[GPL-3.0](LICENSE)
