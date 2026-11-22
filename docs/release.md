# Release artifacts and pipeline

Status: pre-alpha. The artifact names and the `install.sh` mapping are a stable
interface once `v0.1.0` is published; they are specified in
[contracts/cli/release-artifacts.md](contracts/cli/release-artifacts.md). This page
describes the pipeline that produces them and how to verify a download.

## Artifacts in short

`mothdex_<version>_<os>_<arch>[.tar.gz|.zip]` for 9 targets (armv6 and armv7 are
separate archives; never map `armv6l` to `armv7`), one `<archive>.sbom.json` per
archive, `checksums.txt` and `checksums.txt.sigstore.json`. `checksums.txt` lists
all 18 archives and SBOMs, and the cosign bundle signs it. The names, contents,
targets and the `uname` mapping are in the contract above.

## Version information

`mothdex` prints `mothdex <version> (commit <7-char hash>, <commit date>)`, or
`mothdex dev` for builds without a version. The values come from
`internal/buildinfo` (set with `-ldflags -X` by GoReleaser; `go install
module@version` builds fall back to Go's embedded build info, ignoring pseudo and
dirty versions). The date is the **commit time** (RFC 3339 UTC), not the build
time, so builds are reproducible. Format and JSON fields:
[contracts/cli/cli-output.md](contracts/cli/cli-output.md) section 3.1.

## Installing

Download, read, then run. The installer is never piped to a shell, and it
downloads only from the GitHub release of `Or4cu1o/mothdex-cli` over https.

```sh
curl -fsSLO https://raw.githubusercontent.com/Or4cu1o/mothdex-cli/main/install.sh
less install.sh
sh install.sh --dry-run          # downloads and verifies in a temp dir, installs nothing
sh install.sh                    # or: --version v0.1.0, --profile, --bin-dir, --launcher, --uninstall
```

To install a specific version, fetch the installer from that tag (`.../mothdex-cli/vX.Y.Z/install.sh`) so installer and release match.

On Windows, download `install.ps1`, read it, then run `.\install.ps1` (options
`-Version`, `-RetroBatPath`, `-BinDir`, `-Launcher`, `-DryRun`, `-Uninstall`).
Both installers always check the SHA-256 of the archive against `checksums.txt`
and, when `cosign` (or an authenticated `gh`) is on the machine, the signature
and provenance with the identity below; a failing check aborts before anything
is installed. The binary goes to the profile `bin_dir` (mode 0755; on Windows
`%LocalAppData%\Programs\mothdex` by default), and the launcher (Port), only with `--launcher` (`-Launcher` on Windows; off by default in v0.1 because `mothdex tui` does not exist yet), only to
the profile launcher folder, from a fixed template with a marker line, so an
existing file of yours is never overwritten. `install.sh` needs curl (it forces https, redirects included), or GNU wget together with cosign or a logged-in `gh`:
wget can follow an https to http redirect, so the signature check is what protects that path (busybox wget does not
verify TLS certificates and is refused). On Windows, an existing `-BinDir` outside `%LocalAppData%` is accepted only when its
owner is you or an administrator and no other user can write to it; on a Batocera or Recalbox share partition without file permissions (exFAT, FAT, NTFS) the installer, as
root, accepts the profile's own folders with a warning (see [SECURITY.md](../SECURITY.md)); a RetroBat folder created under `C:\` may let
Authenticated Users write, which is inherent to that tree (the launcher is the only file placed there).

## Verifying a download

The provenance example needs a clone with the tag fetched (`git fetch --tags`); otherwise use the commit SHA of the tag
instead of `git rev-list`.

Every release file is listed in `checksums.txt`, which is signed with cosign
(keyless, GitHub OIDC) and attested with GitHub artifact attestations.

```sh
# 1. Checksums (what install.sh does: it compares the archive hash with its line in
#    checksums.txt; cosign is not required on devices)
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
  --source-ref refs/tags/vX.Y.Z \
  --source-digest "$(git rev-list -n1 vX.Y.Z)" --deny-self-hosted-runners
```

## How the pipeline works

- `release.yml` runs only for `v*` tags that are semantic versions and ancestors
  of `main`. Jobs: `build` (GoReleaser, SBOMs, signature, signature verification,
  `scripts/verify-dist.sh`), `attest`, `publish` (re-verifies checksums and attestations: signer workflow, tag, `--source-digest` of that commit, no self-hosted runner;
  checks that the uploaded files are exactly the ones `checksums.txt` lists plus itself and its bundle; as its last check
  before creating the release, that the tag still resolves to the commit that was built, annotated tags followed; then
  `gh release create --verify-tag`). Each job has its own
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
   file, so it is only as strong as this ruleset, and the publish-time check that the tag still points at the built
   commit leaves a short window before `gh release create` that only the ruleset closes. Residual: a ruleset bypass for the
   repository admin role lets an admin still move a `v*` tag; that check is what catches it inside a run, and immutable
   releases lock the tag once the release is published (a repository setting: owner-checked, Sentinela saw it enabled in the T-24 review). Drop the bypass and lift it only when needed, if that residual matters.
3. `Release check` (job `goreleaser snapshot`) may be a required status check: the workflow runs on every pull request and
   skips its steps itself when no release-relevant path changed (`scripts/ci-scope.sh`), so the check always reports. The
   workflow and the script come from the pull request's own merge commit, so a PR can edit them to skip a required job:
   required checks are not a boundary against the PR author, review is. Residual, owner decision pending (backlog
   `ci-self-edit`): CODEOWNERS with required review on `.github/workflows/` and `scripts/ci-scope.sh` would close it, but it
   cannot be used while every agent works under the owner identity (D28 1c: GitHub does not let an author approve their
   own PR).

The ordered owner steps for the first release (gates, tag, approval, verification of the published assets, rollback) are in
[release-v0.1.0-checklist.md](release-v0.1.0-checklist.md).
