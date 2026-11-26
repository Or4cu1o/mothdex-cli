# Release artifacts and pipeline

Status: v0.1.0. The artifact names and the `install.sh` mapping are a stable
interface from `v0.1.0` on; they are specified in
[contracts/cli/release-artifacts.md](contracts/cli/release-artifacts.md). This page
describes the pipeline that produces them and how to verify a download.

## Artifacts in short

`mothdex_<version>_<os>_<arch>[.tar.gz|.zip]` for 9 targets (armv6 and armv7 are
separate archives; never map `armv6l` to `armv7`), one `<archive>.sbom.json` per
archive, `checksums.txt` and `checksums.txt.sigstore.json`. The GUI has its own archives,
`mothdex-gui_<version>_<os>_<arch>` for linux amd64, arm64, armv7, armv6 and windows amd64
(the Windows one carries the pinned `SDL2.dll`, fetched by `scripts/fetch-sdl2-windows.sh` with its
SHA-256 checked, never cached). Every archive carries the licence notices of the Go modules linked into its binary
(`THIRD_PARTY/go-modules-mothdex.txt` in the CLI archives; the GUI archives carry the whole `THIRD_PARTY/` folder, with `go-modules-mothdex-gui.txt` too).
`scripts/gen-go-notices.sh` writes those files from the module list of each binary, and the `Release check` job fails when a file is not current;
`scripts/verify-dist.sh` checks each built binary's `go version -m` against the file in its archive. `checksums.txt` lists all 28 archives and SBOMs, and the cosign
bundle signs it; the attestation covers every listed file. `scripts/verify-dist.sh` checks the
members, the loader and the build flags of every archive, and the snapshot check runs the
installers' own member rules against the GUI archives (`scripts/check-gui-archives.sh`). The
names, contents, targets and the `uname` mapping are in the contract above.

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
sh install.sh                    # or: --version, --profile, --bin-dir, --launcher, --no-launcher, --gui, --no-gui, --alias, --no-alias, --no-modify-path, --uninstall
```

The installer adds the install folder to your PATH without administrator rights: the user `Path` and the current
session on Windows, one line marked `# mothdex` in your shell's startup file elsewhere (only when the folder and that
file cannot be written by other users; for bash on macOS the file is
the login one, `~/.bash_profile` or an existing `~/.profile`). `-NoModifyPath` / `--no-modify-path` prints the command to copy instead, and
uninstall removes only the entry recorded in `.mothdex-install.json` ([contract](contracts/cli/install-state.md)).

It also creates the shortcut `mdex` (`mthd` when `mdex` is taken; none when both are): a hard link `mdex.exe` on
Windows, a relative symbolic link elsewhere. `-Alias <name>` / `--alias <name>` picks another name (2 to 16 characters,
a-z 0-9 and -), `-NoAlias` / `--no-alias` creates none. The installer cannot see aliases or functions defined only in
your interactive shell's startup files.

To install a specific version, fetch the installer from that tag (`.../mothdex-cli/vX.Y.Z/install.sh`) so installer and release match.

On Windows, download `install.ps1`, read it, then run `.\install.ps1` (options
`-Version`, `-RetroBatPath`, `-BinDir`, `-Launcher`, `-NoLauncher`, `-Gui`, `-NoGui`, `-Alias`, `-NoAlias`, `-NoModifyPath`, `-DryRun`, `-Uninstall`).
Both installers always check the SHA-256 of the archive against `checksums.txt`
and, when `cosign` (or an authenticated `gh`) is on the machine, the signature
and provenance with the identity below; a failing check aborts before anything
is installed. The binary goes to the profile `bin_dir` (mode 0755; on Windows
`%LocalAppData%\Programs\mothdex` by default), and the launcher (Port) only to the profile launcher folder, from a fixed template with a marker line (`v3`, which runs `mothdex-gui` and nothing else; a `v1` or `v2` launcher of an
earlier install is upgraded), so an existing file of yours is never overwritten. When the release ships a `mothdex-gui_`
archive for your machine (decided from the verified `checksums.txt` only), the installer also fetches and verifies it, proves that the GUI starts
(`mothdex-gui --probe`, run from `bin_dir` with a scrubbed environment), installs `mothdex-gui` and the licence notices in
`mothdex-licenses/`, and the launcher is written **whenever the GUI is installed** (`--no-launcher` / `-NoLauncher` turns it off, `--no-gui` / `-NoGui`
installs the CLI only). A Port has no terminal, so there is no `mothdex tui` fallback: without the GUI (not eligible, `--no-gui`, no GUI archive for
the machine) no launcher is written and an existing launcher that carries one of our markers is removed, with a note; a file without our marker is never touched. On Windows the launcher
is written only when `<RetroBat>\roms\windows` cannot be written by other users. After writing or removing a launcher on Batocera, EmuELEC or RetroBat the installer asks the
frontend to reload its game list once (loopback, no proxy, never fatal). A dry run executes the verified probe from a staging name in an existing `bin_dir` and removes it again. On a default `C:\RetroBat` the launcher check will usually skip the launcher, because the root of `C:\` lets Authenticated Users modify new subfolders (not verified on a real machine); restrict the permissions of `roms\windows` to enable it. Release checklist: when a file is added to `THIRD_PARTY/`, add its name to `NOTICE_NAMES` in `install.sh` and `$NoticeNames` in `install.ps1` (a test fails otherwise); an installer downloaded before that release aborts on an unknown notice, by design. Details: [ports.md](contracts/cli/ports.md). `install.sh` needs curl (it forces https, redirects included), or GNU wget together with cosign or a logged-in `gh`:
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

## Update

`mdex upgrade` updates installations created by the official installer and recorded in `.mothdex-install.json`.
Existing installations without that state file should run the installer again. Third-party packages and source builds
should use their own installation method; an unmanaged upgrade exits 9 and prints the installer command.

- `mdex upgrade --check`: discover the latest version without downloading archives.
- `mdex upgrade --channel beta`: include prereleases for this invocation.
- `mdex upgrade --version X.Y.Z`: select an exact release, including a downgrade, with a confirmation.
- `mdex upgrade -f`: skip confirmation, retaining every verification.
- `mdex upgrade --json -f`: use the standard JSON envelope, including the signature result and restart hint.

The updater uses the fixed official GitHub release origin. It verifies SHA-256 for every archive, then verifies origin
with cosign when installed or otherwise `gh attestation` when available. This first implementation reports `none`
(SHA-256 only) when neither is available. A present verifier that fails aborts; `-f` never skips a verification.
The following native Sigstore change (D35) removes SHA-256-only updates.

All components are staged and probed before confirmation. After replacement, the CLI and GUI are checked again.
Failure restores the previous files in reverse order. The Windows alias hardlink is recreated for the new CLI;
locked backup files remain until a later start can remove them. Successful updates require persistent interfaces to restart.
A failure whose `details.restored` is false needs a reinstall using the command in the error hint.

Automatic checks request only public release metadata, sending a fixed `mothdex` User-Agent with no version, identifier,
cookie or authentication token. GitHub sees the connection's IP and timing. Checks are cached under
`paths.data/update-check.json`, limited to once per 24 hours and canceled after 3 seconds. The CLI never waits for a
pending check on exit. A notice appears on interactive stderr only, once per day for each new version. Shell and TUI
show the validated cache hint. No update is installed automatically.

Set `update.check=false`, or `MOTHDEX_NO_UPDATE_CHECK=1`, to make no automatic request. CI, unmanaged installations,
dev builds, non-terminal stderr, `--json`, `-q`, completion, version, logs and upgrade commands also skip automatic
checks. `upgrade --check` is an explicit request and ignores configuration, environment and CI opt-outs. The channel
is `update.channel=stable` by default; `beta` includes prereleases. These settings also have the environment overrides
`MOTHDEX_UPDATE_CHECK` and `MOTHDEX_UPDATE_CHANNEL`.
