# T1f - GoReleaser OSS for Mothdex CLI release pipeline

Researcher: Sonda. Date checked: 2026-11-20. Method: GoReleaser docs source (`www/content/` in goreleaser/goreleaser main, rendered at https://goreleaser.com), `gh api` releases/commits.
Labels: Confirmed / Inferred / Unverified. Docs marked "Pro" come from `g_featpro` / `g_inline_pro` shortcodes or "only available in GoReleaser Pro" comments in the doc source.

## Question
Can GoReleaser OSS produce and publish Mothdex (Go, GPL-3.0, CGO_ENABLED=0; linux amd64/arm64/armv7/386, windows, macOS) to winget, Scoop, Homebrew tap, AUR, deb/rpm, with cosign keyless signing, SBOM, GitHub attestations, checksums, snapshot builds? Which features need Pro? How to harden GitHub Actions?

## Short answer
- Current GoReleaser: **v2.20.3** (2026-11-20). Needs Go 1.27.4 to build from source (go.mod); irrelevant when using the action/binary.
- All requested features exist in **OSS**: winget, Scoop, Homebrew (use `homebrew_casks`; `brews` is deprecated since v2.10), AUR (`aurs` binary, `aur_sources` source), nfpm deb/rpm, cosign signing, SBOM via syft, checksums, snapshot, armv7 (`goarm: 7`).
- **GitHub attestations**: no GoReleaser section; done in workflow with `actions/attest` (docs now recommend it; `actions/attest-build-provenance` v4.2.2 is a maintained wrapper) over `dist/checksums.txt`.
- Pro-only items are sub-options (MSI/NSIS for winget/scoop, `alternative_names`/versioned formulas, nfpm `templated_*`/`if`, `sign.if`, `templated_extra_files`, notarize, DMG `app`). None is required for the baseline.
- Caveats: Homebrew casks traditionally target macOS apps, so the Linux/CLI tap path is unclear (see risks); winget publishes via PR to a fork of microsoft/winget-pkgs; AUR needs an SSH key and Arch guideline compliance.

## Evidence table
| Claim | Label | Source URL | Version | Date |
|---|---|---|---|---|
| Latest release v2.20.3 published 2026-11-20 | Confirmed | https://github.com/goreleaser/goreleaser/releases/tag/v2.20.3 | v2.20.3 | 2026-11-20 |
| GoReleaser Pro vs OSS action input `distribution: goreleaser` (default) or `goreleaser-pro` | Confirmed | https://goreleaser.com/ci/actions/ | v2.20.3 | 2026-11-20 |
| Go builder supports `goos/goarch/goarm/go386`; `goarm: [6,7]`, default `[6]`; `386` in default goarch list; `CGO_ENABLED=0` shown as env | Confirmed | https://goreleaser.com/customization/builds/builders/go/ | v2.20.3 | 2026-11-20 |
| armv7: set `goarm: [7]` (default is 6 only); float ABI selectable `7,hardfloat` since v2.17 | Confirmed | same | v2.20.3 | 2026-11-20 |
| Winget publisher (OSS): generates manifest, opens PR; `use: msi/nsis` and `product_code` are Pro | Confirmed | https://goreleaser.com/customization/publish/winget/ | v2.20.3 | 2026-11-20 |
| Scoop (OSS): manifest into bucket repo; `use: msi/nsis` Pro | Confirmed | https://goreleaser.com/customization/publish/scoop/ | v2.20.3 | 2026-11-20 |
| `homebrew_casks` added v2.10; `brews` (formulas) deprecated v2.10 (soft), "since v2.16" status in deprecations page; casks recommended | Confirmed | https://goreleaser.com/customization/publish/homebrew_casks/ , https://goreleaser.com/resources/deprecations/#brews | v2.20.3 | 2026-11-20 |
| Homebrew `alternative_names` (versioned formulae/casks) and `app` (DMG) are Pro | Confirmed | same two pages | v2.20.3 | 2026-11-20 |
| AUR `aurs` (binary PKGBUILD, enforces `-bin` suffix, SSH key via `private_key`, `git_url` ssh://aur@aur.archlinux.org/...) in OSS, no Pro markers | Confirmed | https://goreleaser.com/customization/publish/aur/ | v2.20.3 | 2026-11-20 |
| `aur_sources` (source PKGBUILD) in OSS, no Pro markers; key must not be password-protected | Confirmed | https://goreleaser.com/customization/publish/aursources/ | v2.20.3 | 2026-11-20 |
| nFPM deb/rpm/apk/etc. in OSS; Pro-only: `if` filter (v2.4), `templated_contents`, `templated_scripts` | Confirmed | https://goreleaser.com/customization/package/nfpm/ | v2.20.3 | 2026-11-20 |
| nfpm library version used: v2.49.0 (released 2026-11-12) | Confirmed | https://github.com/goreleaser/goreleaser/blob/v2.20.3/go.mod | v2.20.3 | 2026-11-20 |
| Signing section (`signs`) OSS; cosign example uses `--bundle=${signature}` and `--yes`; verify with `cosign verify-blob --bundle checksums.txt.sigstore.json`; Pro-only: `if`, installer/diskimage artifact types | Confirmed | https://goreleaser.com/customization/sign/sign/ | v2.20.3 | 2026-11-20 |
| Keyless signing needs workflow `id-token: write` and cosign installed (`sigstore/cosign-installer`) | Inferred (OIDC is cosign behaviour; docs mention OIDC issuer verify) | https://goreleaser.com/customization/sign/sign/ | v2.20.3 | 2026-11-20 |
| SBOM via syft default (`cmd: syft`), `artifacts: archive` etc.; installer/diskimage Pro only; syft must be installed (e.g. `anchore/sbom-action/download-syft`) | Confirmed (syft default) / Inferred (install step) | https://goreleaser.com/customization/sbom/ | v2.20.3 | 2026-11-20 |
| Attestations documented as workflow steps using `actions/attest@v4` with `subject-checksums: ./dist/checksums.txt`; needs `id-token: write`, `attestations: write`; checksum `name_template: checksums.txt` | Confirmed | https://goreleaser.com/customization/publish/attestations/ | v2.20.3 | 2026-11-20 |
| actions/attest-build-provenance v4.2.2 (2026-08-06) not archived; actions/attest v4.2.2 (2026-08-04) | Confirmed | https://github.com/actions/attest , https://github.com/actions/attest-build-provenance | v4.2.2 | 2026-11-20 |
| Verification: `gh attestation verify --owner <org> <file>` | Confirmed | attestations page above | v2.20.3 | 2026-11-20 |
| Checksums OSS; only `templated_extra_files` Pro | Confirmed | https://goreleaser.com/customization/package/checksum/ | v2.20.3 | 2026-11-20 |
| Snapshot: `--snapshot` / `snapshot.version_template`; builds without publishing; version must be nfpm-semver compatible, no `{{.Tag}}` | Confirmed | https://goreleaser.com/customization/publish/snapshots/ | v2.20.3 | 2026-11-20 |
| Notarize macOS: Pro (`g_featpro`) | Confirmed | https://goreleaser.com/customization/sign/notarize/ | v2.20.3 | 2026-11-20 |
| goreleaser-action latest v7.3.0 (2026-10-12) | Confirmed | https://github.com/goreleaser/goreleaser-action/releases | v7.3.0 | 2026-11-20 |

## Pro-only feature list (with docs)
| Feature | Needed for Mothdex? | Doc |
|---|---|---|
| winget/scoop `use: msi|nsis`, `product_code` | No (zip/archive works) | https://goreleaser.com/customization/publish/winget/ , .../scoop/ |
| Homebrew `alternative_names`, `app` | No | https://goreleaser.com/customization/publish/homebrew_casks/ |
| nfpm `if`, `templated_contents`, `templated_scripts` | No | https://goreleaser.com/customization/package/nfpm/ |
| signs `if` (artifact filter), installer/diskimage types | No (use `artifacts: checksum`) | https://goreleaser.com/customization/sign/sign/ |
| checksum `templated_extra_files` | No | https://goreleaser.com/customization/package/checksum/ |
| macOS notarize (`notarize`) | Only if shipping notarized macOS binaries (see risks) | https://goreleaser.com/customization/sign/notarize/ |
| SBOM installer/diskimage artifacts | No | https://goreleaser.com/customization/sbom/ |
Not exhaustive: I scanned the pages above only (Inferred complete for the requested features).

## Options compared
| Concern | Option A | Option B | Note |
|---|---|---|---|
| Signing | cosign keyless on `checksums.txt` (one bundle covers all archives) | sign every artifact | A is cheaper; Pro `if` not needed |
| Provenance | `actions/attest` (documented by GoReleaser) | `actions/attest-build-provenance` (wrapper, v4.2.2) | Same engine; prefer the one the docs cite |
| Homebrew | `homebrew_casks` (non-deprecated) | `brews` (deprecated, still works) | See risk 2 |
| AUR | `aurs` (`-bin`, prebuilt) | `aur_sources` (builds from source) | Arch guidelines prefer source for non-`-bin`; ship both under different names if wanted |
| Windows | winget (PR to winget-pkgs) + scoop bucket | zip only | winget PR needs token with fork access and manual maintainer review |

## Supply-chain hardening for GitHub Actions
| Practice | Label | Source |
|---|---|---|
| Pin third-party actions by full commit SHA (comment tag for humans) | Confirmed (GitHub guidance) | https://docs.github.com/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions |
| Least privilege: `permissions: {}` at workflow level, grant `contents: write`, `id-token: write`, `attestations: write` only on the release job | Confirmed | same + attestations page |
| Trigger release only on tags; protect tags and use an `environment` with required reviewers | Inferred (best practice) | https://docs.github.com/actions/deployment/targeting-different-environments |
| Avoid `pull_request_target` / untrusted checkout in release workflow; `persist-credentials: false` on checkout where possible (GoReleaser then needs token env) | Inferred | - |
| Keep Dependabot/Renovate for action SHA bumps | Inferred | https://docs.github.com/code-security/dependabot |
| Pin GoReleaser version in `with: version:` (not `latest`) and cosign/syft versions | Inferred | https://goreleaser.com/ci/actions/ |
| Run `gitleaks` pre-commit (project rule), secrets only via Actions secrets: tap/bucket PAT, AUR SSH key, winget token | Confirmed (project rules) | repo rules |

SHA pins resolved via `gh api repos/<r>/commits/<tag>` on 2026-11-20 (re-verify before use; tags are mutable):
| Action | Tag | Commit SHA |
|---|---|---|
| goreleaser/goreleaser-action | v7.3.0 | a4f02ab5355a589de60d917a659ce617ed19b3de |
| actions/attest | v4.2.2 | 1e69f48acb82d1966a394da916b4c1698aa569d6 |
| actions/attest-build-provenance | v4.2.2 | 4d101475d8b20a2381f78447822ac1eab6504dd8 |
| sigstore/cosign-installer | v4.1.3 | 756fe2d284c7292c4e9b62c8a28f1e5629c59532 |
| anchore/sbom-action | v0.24.3 | 66cbf4bc1f1c0d2edc94016e65bc221b6bb0ad6c |
| actions/checkout | v7.0.1 | 3d3c42e5aac5ba805825da76410c181273ba90b1 |
| actions/setup-go | v7.1.0 | d8cbb8045d7f43706b9177cceb36c1f91a6644d5 |
| actions/upload-artifact | v7.0.2 | cf430e030ddbb5b0abf93d22962f4752f3646cd9 |
Related versions: cosign v3.1.4 (2026-10-20), syft v1.57.2 (2026-11-16), winget-create v1.28.1.0 (2026-11-11).

## Recommendation
Use GoReleaser OSS v2.20.3 (pin via `version:`; do not use `latest`).
1. Builds: `CGO_ENABLED=0`; goos linux/windows/darwin; goarch amd64/arm64/386/arm with `goarm: [7]`; `ignore` darwin/386, windows/arm (adjust) and darwin/arm 32-bit.
2. Archives (tar.gz; zip for windows), `checksum.name_template: checksums.txt`.
3. nfpm deb+rpm (OSS); verify arch mapping for armv7 (`armhf`/`armv7hl`) in snapshot run.
4. Sign `checksums.txt` with cosign keyless (`--bundle`), SBOM per archive with syft, then `actions/attest` on `dist/checksums.txt`.
5. Publishers: `homebrew_casks` (after verifying the CLI use case, see risks), `scoop`, `winget`, `aurs` (and optionally `aur_sources`).
6. CI: PR job runs `goreleaser release --snapshot --clean` (and `goreleaser check`) with no secrets; tag job does the real release.
Conditions: tap/bucket repos, AUR account+SSH key, winget-pkgs fork, and PATs exist and are stored as secrets; owner approves any publish (project rule).

## Risks
1. Pro gaps are minor, but notarization of macOS binaries is Pro-only; unsigned CLI downloaded via browser triggers Gatekeeper; Homebrew/curl installs avoid quarantine. Alternative (Inferred): sign/notarize outside GoReleaser with `quill` or Apple tooling (`goreleaser/quill` appears in go.mod, not verified as an OSS pipe).
2. `brews` is deprecated since v2.10 in favor of casks; Homebrew casks traditionally target macOS apps, so Linux/Linuxbrew CLI distribution via casks needs verification (Unverified; docs say casks should be used instead).
3. winget: PR review by Microsoft maintainers is asynchronous; package identifier must be registered once manually (Unverified).
4. AUR: account suspended or key mis-set fails the release step; Arch guidelines may reject non-`-bin` prebuilt packages.
5. Tag-mutable actions: SHAs above must be refreshed by Dependabot; compromised-upstream risk remains for the GoReleaser binary itself (action downloads it; consider checksum verification).
6. Attestation docs reference `actions/attest@v4`; if org policy restricts actions, allowlist both attest actions.
7. `--snapshot` versions with `+`/long suffix can break nfpm/rpm; set `snapshot.version_template`.

## Open questions
1. Is macOS notarization a requirement (decides Pro vs external tooling)?
2. Which Homebrew path: cask in a tap, deprecated formula, or homebrew-core submission later?
3. Does the owner already control a tap repo, scoop bucket, AUR account, and a winget-pkgs fork?
4. Do we need armv6 (Raspberry Pi 1/Zero) in addition to armv7?
5. Should provenance level go beyond attestation (SLSA generator reusable workflow) for supply-chain assurance?
