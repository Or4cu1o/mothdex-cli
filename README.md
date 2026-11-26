# mothdex-cli

[![CI](https://github.com/Or4cu1o/mothdex-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/Or4cu1o/mothdex-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Or4cu1o/mothdex-cli?include_prereleases)](https://github.com/Or4cu1o/mothdex-cli/releases)
[![License: GPL-3.0](https://img.shields.io/badge/license-GPL--3.0-blue)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/Or4cu1o/mothdex-cli)](go.mod)

Command-line client for the [Mothdex](https://api.mothdex.net) API.

> **Status: v0.1.0.** First release. The command-line client, the interactive shell and the terminal UI are
> implemented; the `--json` output and the exit codes are documented in
> [docs/contracts/cli](docs/contracts/cli). The separate graphical frontend (`mothdex-gui`) and the Ports
> installer (Batocera, Recalbox, RetroBat and similar) ship in the release but are in beta: they have not been
> tested on real devices yet.

## Features

- Search, entry details, random pick, platforms, regions and catalog statistics (API v1)
- Resilient downloader: parallel files, retry with backoff, size validation, atomic move
- Cache with TTL and size limit, request history
- Interactive shell, terminal UI and `mothdex gui` to open the installed gamepad interface
- Grouped help with runnable examples: `mothdex --help`, `mothdex help config`, `mothdex help exit-codes` and [error recovery](docs/errors.md). Help follows `--lang` or `interface.language` in English and Brazilian Portuguese; other locales fall back to English
- Per-system profiles (Batocera, Recalbox, EmuELEC, Lakka, RetroBat, EmuDeck, RetroDECK, generic)
- Localization (`en_us`, `pt_br`, `es_es`, `ru_ru`, `hi_in`, `ja_jp`, `zh_cn`)
- Stable `--json` output and documented exit codes
- Release binaries for Linux, Windows and macOS with checksums, SBOMs and signatures

## Install

One line, no administrator rights:

| System | Command |
|---|---|
| macOS, Linux, WSL | `curl -fsSL https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.sh \| sh` |
| Linux, with options | `curl -fsSL https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.sh \| sh -s -- --no-gui` |
| Windows PowerShell | `irm https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.ps1 \| iex` |
| PowerShell, with options | `& ([scriptblock]::Create((irm https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.ps1))) -NoGui` |
| Windows CMD | `curl -fsSL https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.cmd -o "%TEMP%\mothdex-install.cmd" && "%TEMP%\mothdex-install.cmd" && del "%TEMP%\mothdex-install.cmd"` |

On Windows, look at the prompt: `PS C:\>` is PowerShell; `C:\>` without `PS` is CMD.

These addresses serve the installers from release 0.2.0 on. For an older release, see
[docs/beta-testing.md](docs/beta-testing.md).

The installer checks the SHA-256 of what it downloads (and the signature when `cosign` or `gh` is available), creates
the short name `mdex`, and adds the install folder to your PATH (`--no-modify-path` / `-NoModifyPath` to do it
yourself). Piping a script runs it before anything has checked the script itself:
[ADR-0003](docs/adr/ADR-0003-pipe-install.md).

To read it first, download it, verify it, then run it:

```sh
curl -fsSLO https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.sh
gh attestation verify install.sh --repo Or4cu1o/mothdex-cli   # optional, needs a logged-in gh
less install.sh
sh install.sh --dry-run          # downloads and verifies, installs nothing
sh install.sh
```

```powershell
irm https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.ps1 -OutFile install.ps1
Get-Content install.ps1          # read it first
powershell -ExecutionPolicy Bypass -File .\install.ps1 -DryRun
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

Options and details: [docs/release.md](docs/release.md#installing).

## Test it and report

Testing on a handheld or a retro-gaming system? Read the [beta testing guide](docs/beta-testing.md), then send a
[Ports beta report](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=ports-beta.yml) for the GUI or the Port
on a frontend, or an [install report](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=device_report.yml)
for everything else. Other forms:
[bug report](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=bug.yml) and
[feature request](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=feature_request.yml) here, and the
[MCP server's forms](https://github.com/Or4cu1o/mothdex-mcp/issues/new/choose).

Found an error? Run `mothdex logs bundle`, review the zip and attach it with the error's `ref`
([how to send an error](docs/beta-testing.md#how-to-send-an-error)).

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
