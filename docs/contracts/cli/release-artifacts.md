# Contract: release artifacts (the `install.sh` interface)

Status: draft v1 (T-21a-doc) · Owner: Alicerce · Producer: Guindaste (`.goreleaser.yaml`, T-21) · Consumers: `install.sh`
(T-21b), future package manifests (E-M4) · Risk: High surface (supply chain); this file only fixes names and mappings.

Once `v0.1.0` is published, everything below is a **stable interface**: pinned installers and scripts download by
these names. Renaming or removing an artifact is a breaking change and needs a new ADR. Adding a target is additive.

## 1. Release assets

Every GitHub release `vX.Y.Z` (tag with a `v`) contains exactly:

| Asset | Content |
|---|---|
| `mothdex_<version>_<os>_<arch>.tar.gz` | Linux and macOS archives |
| `mothdex_<version>_windows_<arch>.zip` | Windows archives |
| `mothdex_<version>_<os>_<arch>.<ext>.sbom.json` | one SBOM per archive |
| `checksums.txt` | `sha256  <asset>` for every archive and SBOM (GNU `sha256sum` format) |
| `checksums.txt.sigstore.json` | keyless cosign bundle for `checksums.txt` |

- `<version>` is the tag **without** the leading `v` (`0.1.0`, `0.2.0-rc.1`). A tag containing `-` is a pre-release.
- Inside each archive: the binary at the **root** (`mothdex`, or `mothdex.exe` on Windows; the tar keeps mode `0755`),
  plus `LICENSE`, `README.md` and `CHANGELOG.md`. No top-level directory.
- The GUI binary ships in its **own** archives with the prefix `mothdex-gui_` and is never added inside
  `mothdex_` archives. Installers match `mothdex_<version>_` or `mothdex-gui_<version>_` exactly, never a bare `mothdex*` glob.
- GUI archives (`mothdex-gui_<version>_<os>_<arch>.<ext>`, each with its `.sbom.json`, all in `checksums.txt`) exist for `linux_amd64`,
  `linux_arm64`, `linux_armv7`, `linux_armv6` and `windows_amd64`. Members, exactly: `mothdex-gui` (`mothdex-gui.exe`), `LICENSE`,
  `README.md`, `CHANGELOG.md`, the files of `THIRD_PARTY/` under `THIRD_PARTY/`, and on Windows the pinned `SDL2.dll`
  (`scripts/sdl2-windows.pin`). No `386`, `darwin` or `windows_arm64` GUI in this version; a release without a GUI archive for a machine is
  a CLI-only release for it (ports.md 5.1).

## 2. Targets (9)

| `<os>_<arch>` | Go target | Typical systems |
|---|---|---|
| `linux_amd64` | `GOAMD64=v1` | PCs, Batocera/Recalbox x86_64, Bazzite, Lakka x86_64 |
| `linux_386` | `GO386=sse2` | Batocera `x86_wow64` (i686) |
| `linux_arm64` | | Batocera aarch64, EmuELEC, Recalbox rpi4_64/rpi5_64 |
| `linux_armv7` | `GOARM=7` | Batocera cortex-a7/a9/a17, Recalbox rpi3/rpizero2 (32-bit) |
| `linux_armv6` | `GOARM=6` | Batocera bcm2835 and Recalbox rpi1 (Pi 1/Zero) |
| `windows_amd64` | | Windows 10/11, RetroBat (64-bit only) |
| `windows_arm64` | | Windows on Arm |
| `darwin_amd64` | | macOS Intel |
| `darwin_arm64` | | macOS Apple silicon |

Not built: `windows_386`, `windows_arm`, `darwin_386`, `darwin_arm`. `linux_riscv64` (Batocera jh7110) may be added
later (additive). The `mothdex` binaries are static (`CGO_ENABLED=0`, `-trimpath`). `mothdex-gui` is built the same way but on Linux it is dynamic: it needs the system loader to find `libSDL2-2.0.so.0` (gui.md §2, ports.md §3), so the installer proves it starts before installing it.

## 3. `uname` → archive mapping (`install.sh`)

The `mothdex` binary is static, so only the **kernel** architecture matters, and `uname -m` reports exactly that (the `mothdex-gui` archive uses the same mapping; its loader and SDL2 are checked by the installer's probe): a 32-bit
userland on a 64-bit kernel (some EmuELEC builds) reports `aarch64` and takes `arm64`. Do not use `getconf LONG_BIT`,
which reports the userland.

| `uname -s` | `uname -m` | Archive suffix |
|---|---|---|
| `Linux` | `x86_64`, `amd64` | `linux_amd64` |
| `Linux` | `i386`, `i486`, `i586`, `i686` | `linux_386` |
| `Linux` | `aarch64`, `arm64` | `linux_arm64` |
| `Linux` | `armv8l`, `armv7l`, other `armv7*` | `linux_armv7` (`armv8l` = 64-bit CPU under a 32-bit kernel) |
| `Linux` | `armv6l`, other `armv6*` | `linux_armv6` |
| `Darwin` | `x86_64` | `darwin_amd64` |
| `Darwin` | `arm64` | `darwin_arm64` |
| anything else | | stop with `unsupported system: <s>/<m>`; never guess |

Windows does not use `install.sh`. `install.ps1` (T-21b) takes the zip: `PROCESSOR_ARCHITEW6432`, else
`PROCESSOR_ARCHITECTURE`, maps `AMD64` to `windows_amd64` and `ARM64` to `windows_arm64`; anything else stops with
`unsupported system: windows/<arch>`. Only the root entry `mothdex.exe` is extracted (exact, case-sensitive name).
Package managers come in E-M4.

## 4. Verification by installers

1. Download `checksums.txt` and the archive over `https` from `github.com/Or4cu1o/mothdex-cli/releases/download/<tag>/`.
2. **Required:** exactly one line of `checksums.txt` names the archive (exact name, optional `*`), and its SHA-256
   (64 hex) equals the archive's, computed with `sha256sum`, `shasum -a 256` or `openssl dgst -sha256` (busybox has
   no `--ignore-missing`). A mismatch aborts without installing anything.
3. **Optional, when `cosign` is available:** `cosign verify-blob --bundle checksums.txt.sigstore.json
   --certificate-identity-regexp '^https://github\.com/Or4cu1o/mothdex-cli/\.github/workflows/release\.yml@refs/tags/v'
   --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt`.
   Installers may pin the exact identity `…release.yml@refs/tags/<tag>` (stricter; T-21b does). A present tool
   that fails aborts before anything is installed. Emulation systems have no cosign, so the checksum is the baseline there. GitHub artifact attestations
   (`gh attestation verify`) are a further optional check.
4. Install into the profile's `bin_dir` ([`profiles.md`](profiles.md)) after the exec-bit check described there.

## Open questions

- OQ-REL-1: should `install.sh` support `--version <tag>` pinning from day one? Proposed: yes; the default is the latest non-pre-release.
