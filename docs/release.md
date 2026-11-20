# Release artifacts and pipeline

Status: pre-alpha. This page is the contract that `install.sh` (T-21b) and later
package manifests (winget, Scoop, AUR) depend on. Renaming anything here after
`v0.1.0` breaks pinned installers.

## Archive names

```text
mothdex_<version>_<os>_<arch>[v<arm>].tar.gz     linux, darwin
mothdex_<version>_<os>_<arch>.zip                windows
```

`<version>` is the tag without the leading `v`. The binary (`mothdex` or
`mothdex.exe`) sits at the archive root next to `LICENSE`, `README.md` and
`CHANGELOG.md`; the executable bit (0755) is kept by tar. Every archive has a
sibling `<archive>.sbom.json`.

| Target | Archive suffix |
|---|---|
| Linux x86-64 | `linux_amd64.tar.gz` |
| Linux ARM 64-bit | `linux_arm64.tar.gz` |
| Linux ARM 32-bit, ARMv7 | `linux_armv7.tar.gz` |
| Linux ARM 32-bit, ARMv6 (Raspberry Pi 1/Zero) | `linux_armv6.tar.gz` |
| Linux x86 32-bit | `linux_386.tar.gz` |
| Windows x86-64 / ARM 64-bit | `windows_amd64.zip` / `windows_arm64.zip` |
| macOS Intel / Apple silicon | `darwin_amd64.tar.gz` / `darwin_arm64.tar.gz` |

Release files: the 9 archives, their 9 SBOMs, `checksums.txt` (SHA-256 of all 18
files) and `checksums.txt.sigstore.json` (cosign bundle). There is no archive for
`windows/386`, `windows/arm`, `darwin/386` or `darwin/arm`.

### `uname -m` mapping for installers

| `uname -m` | Archive arch |
|---|---|
| `x86_64` | `amd64` |
| `aarch64`, `arm64` | `arm64` |
| `armv7l` | `armv7` |
| `armv6l` | `armv6` (never map it to `armv7`: an ARMv7 binary dies with an illegal instruction) |
| `i386`, `i686` | `386` |

Choose `arm64` from the kernel architecture (`uname -m`), not from the userland:
the binaries are static, so a 32-bit userland on a 64-bit kernel (EmuELEC) runs
the `arm64` build fine. A future `mothdex-gui` must ship under its own prefix
(`mothdex-gui_...`), never as extra files inside `mothdex_`.

## Version information

`mothdex` prints `mothdex <version> (commit <hash>, commit date <RFC3339 UTC>)`.
The values come from `internal/buildinfo` (set with `-ldflags -X` by GoReleaser,
falling back to Go's embedded build info for `go install` builds). The date is the
**commit time**, not the build time, so builds are reproducible. Fields that are
unknown are empty (JSON `version`, `commit`, `date`), never placeholder strings.

## Verifying a download

Every release file is listed in `checksums.txt`, which is signed with cosign
(keyless, GitHub OIDC) and attested with GitHub artifact attestations.

```sh
# 1. Checksums (what install.sh does; cosign is not required on devices)
sha256sum --check --ignore-missing checksums.txt

# 2. Signature of checksums.txt and who signed it
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/vX.Y.Z \
  checksums.txt

# 3. Build provenance of a file
gh attestation verify mothdex_X.Y.Z_linux_amd64.tar.gz --repo Or4cu1o/mothdex-cli \
  --signer-workflow Or4cu1o/mothdex-cli/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z
```

## How the pipeline works

- `release.yml` runs only for `v*` tags that are semantic versions and ancestors
  of `main`. Jobs: `build` (GoReleaser, SBOMs, signature, signature verification,
  `scripts/verify-dist.sh`), `attest`, `publish` (re-verifies checksums and
  attestations, then `gh release create --verify-tag`). Each job has its own
  least-privilege permissions and no dependency cache is used.
- GoReleaser, cosign and syft are built from the modules pinned in
  `tools/*/go.mod` (checksums in `go.sum`, updates through Dependabot).
- `release-check.yml` runs the same build on pull requests that touch the
  release config, without signing, and builds twice to compare the archive
  checksums.
- The Go toolchain is pinned by the `toolchain` line of `go.mod`, which is part of
  what makes builds reproducible: bump it deliberately.
- Nothing is published to package managers (decision D3).

## Owner settings the pipeline relies on

These are repository settings, not files, and need the owner's approval:

1. **Environment `release`** with required reviewers. Without it, GitHub creates
   an unprotected environment on first use and `publish` runs without approval.
   Restrict deployments to protected branches/tags.
2. **Tag ruleset for `v*`**: only the owner can create, and tags cannot be moved
   or deleted. The "tag is on `main`" check runs inside the tag's own workflow
   file, so it is only as strong as this ruleset.
3. Do not mark `Release check` as a required status check: it is path-filtered.
