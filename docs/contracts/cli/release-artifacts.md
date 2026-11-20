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
| `checksums.txt` | `sha256  <asset>` for every archive (GNU `sha256sum` format) |
| `checksums.txt.sigstore.json` | keyless cosign bundle for `checksums.txt` |

- `<version>` is the tag **without** the leading `v` (`0.1.0`, `0.2.0-rc.1`). A tag containing `-` is a pre-release.
- Inside each archive: the binary at the **root** (`mothdex`, or `mothdex.exe` on Windows; the tar keeps mode `0755`),
  plus `LICENSE`, `README.md` and `CHANGELOG.md`. No top-level directory.
- A future GUI binary ships in its **own** archives with the prefix `mothdex-gui_` and is never added inside
  `mothdex_` archives. Installers match `mothdex_<version>_` exactly, never a bare `mothdex*` glob.

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
later (additive). All binaries are static (`CGO_ENABLED=0`, `-trimpath`).

## 3. `uname` → archive mapping (`install.sh`)

The binary is static, so only the **kernel** architecture matters, and `uname -m` reports exactly that: a 32-bit
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

Windows does not use `install.sh` (RetroBat and other Windows installs take the zip; package managers come in E-M4).

## 4. Verification by installers

1. Download `checksums.txt` and the archive over `https` from `github.com/Or4cu1o/mothdex-cli/releases/download/<tag>/`.
2. **Required:** the archive's line in `checksums.txt` must match (`sha256sum -c --ignore-missing`, with an
   `openssl dgst -sha256` fallback). A mismatch aborts without installing anything.
3. **Optional, when `cosign` is available:** `cosign verify-blob --bundle checksums.txt.sigstore.json
   --certificate-identity-regexp '^https://github\.com/Or4cu1o/mothdex-cli/\.github/workflows/release\.yml@refs/tags/v'
   --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt`.
   Emulation systems have no cosign, so the checksum is the baseline there. GitHub artifact attestations
   (`gh attestation verify`) are a further optional check.
4. Install into the profile's `bin_dir` ([`profiles.md`](profiles.md)) after the exec-bit check described there.

## Open questions

- OQ-REL-1: should `install.sh` support `--version <tag>` pinning from day one? Proposed: yes; the default is the latest non-pre-release.
