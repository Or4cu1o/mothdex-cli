# Release checklist: mothdex-cli v0.1.0

Owner: Guindaste (T-23). Status: prepared at main `b45d433`; nothing here has been run against a real tag.
Companion to [release.md](release.md) (how the pipeline works) and
[contracts/cli/release-artifacts.md](contracts/cli/release-artifacts.md) (artifact names, stable from this release on).
Steps marked **OWNER OK** change repository settings, create the tag or publish, and need the owner's explicit approval.
What was and was not checked while preparing this page is at the end.

## 1. Before the tag

1. **Release gates still open in the M1 acceptance** ([qa/T-22-m1-acceptance.md](qa/T-22-m1-acceptance.md)):
   - **D1** every PR since the last review has its required reviews, CI is green on `main`, gitleaks is clean on both
     repos (`gitleaks git --redact` on a fresh clone of each).
   - **D2** the agents' bot identity replaces the owner identity (D14, ROADMAP). **OWNER OK, owner decision open:** which
     GitHub account is the bot, and whether it may create the tag. The tag ruleset (step 2) allows creation only by the
     owner, so the bot must either be a bypass actor of that ruleset or the owner creates the tag. The workflow does not
     depend on who pushes the tag; it only checks that the tag is a semantic version and an ancestor of `main`.
   - **D3** the tag needs the owner's OK; Regente tags after it, never Ensaio.
   - **A2** the release build with real signing (steps 6 to 8). It can only be closed by the tag run, then a manual review
     of the release assets (section 3). Until then A1 (snapshot, signing skipped) is the evidence.
2. **Settings the pipeline relies on** (**OWNER OK**, details in [release.md](release.md)):
   - Environment `release` with the owner as required reviewer, deployments restricted to tags `v*`. Without it GitHub
     creates an unprotected environment on first use and `publish` runs without approval.
   - Tag ruleset for `v*`: creation only by the owner (or the bot, see D2), update and deletion blocked.
   - `Release check` (job `goreleaser snapshot`) may be a required status check: it runs on every pull request and skips its own steps when nothing release-relevant changed.
   - Recommended: immutable releases, so assets cannot be replaced after publishing.
3. **Prepare PR** (docs only, Low): run `scripts/changelog-fold.sh --release 0.1.0` (it folds the `changes/` fragments into
   `CHANGELOG.md`, deletes them, moves the `[Unreleased]` entries under `## [0.1.0] - <date>` and adds a fresh empty `[Unreleased]`); check README and `docs/release.md` say pre-alpha only where that is still true. Merge.
4. **Re-run the T-22 delta on the release commit** (Ensaio): `go test -race -count=1 ./...`, and the CI jobs `goreleaser
   snapshot`, `release-check` and `installer`. Findings F3 (stdin from `/dev/null`) and F5 are product defects owned by
   Forja (T-20c): decide whether they block v0.1.0.
5. **Which commit:** the merge commit of the prepare PR on `main` (it must be an ancestor of `main`, the workflow fails
   otherwise), with CI green on that exact commit. Note the full SHA: `git rev-parse origin/main`.

## 2. Tag and publish

6. **Tag (OWNER OK).** From a clone:

   ```sh
   git fetch origin && git checkout --detach <sha>      # the commit noted in step 5
   git tag -a v0.1.0 -m "mothdex-cli v0.1.0" <sha>
   git push origin v0.1.0                               # never --force; a wrong tag is rolled back, not moved
   ```

7. **What `release.yml` does** (tag push only; three jobs, no dependency cache):
   - `build`: checks the tag is `vX.Y.Z[-pre]` and an ancestor of `main`; builds the pinned goreleaser, cosign and syft
     from `tools/*/go.mod`; `goreleaser release --clean` (9 targets, SBOMs, `checksums.txt`, keyless cosign bundle);
     verifies the bundle against the exact identity of this workflow and tag; runs `scripts/verify-dist.sh dist 0.1.0`
     (formats, static linking, `CGO_ENABLED=0`, `-trimpath`, the version line of every binary that can run there, exactly
     18 files in `checksums.txt`).
   - `attest`: GitHub build provenance for every file in `checksums.txt` and for `checksums.txt` itself.
   - `publish` (**waits for the `release` environment: the owner approves it in the Actions run**): re-checks the
     checksums, runs `gh attestation verify` on every file (signer workflow, `refs/tags/v0.1.0`, `--source-digest` of the commit, no
     self-hosted runner), checks that the files to upload are exactly those in `checksums.txt` plus itself and its bundle,
     checks as its last step that `v0.1.0` still resolves to the built commit, then
     `gh release create v0.1.0 --verify-tag --generate-notes` with the 18 archives and SBOMs, `checksums.txt` and
     `checksums.txt.sigstore.json`. A tag with `-` becomes a pre-release.
8. **Approve** the `release` environment only after `build` and `attest` are green and you have read the `build` log
   (the `checksums.txt` that goreleaser prints, and `verify-dist: ok (version 0.1.0 ..., 9 targets)`).

### Optional rehearsal on a pre-release tag (Ensaio, PR-62-test)

The `v*` ruleset forbids moving or deleting tags, so a wrong assumption that fails closed on the real run would burn
`v0.1.0`. A rehearsal on an annotated tag such as `v0.1.0-rc.1` (a tag with `-` is published as a pre-release) proves what
the fake-`gh` tests cannot: (a) `GITHUB_SHA` on an annotated tag push equals the peeled commit, so the tag check passes;
(b) the runner's `gh` accepts `--source-digest` and `--deny-self-hosted-runners` and verifies the real attestations;
(c) the upload-set step passes on a real dist with SBOMs and the bundle; (d) `GITHUB_TOKEN` can read `git/ref/tags` and
`git/tags`; (e) the release is created as a pre-release. It cannot prove the tag-moved failure (needs a throwaway repo or
an admin bypass) or the final non-pre-release path. Creating the tag is **OWNER OK**; the rc tag and its release stay
afterwards (immutable), so the first real release is `v0.1.0`.

## 3. Verify the published release

Run in an empty directory, with `gh`, `cosign` and `sha256sum` installed. `TAG=v0.1.0`.

```sh
gh release download "$TAG" --repo Or4cu1o/mothdex-cli      # everything, including the sigstore bundle
gh release view "$TAG" --repo Or4cu1o/mothdex-cli --json assets --jq '.assets | length'   # expect 20
```

1. **Checksums:** `sha256sum --check --strict checksums.txt` (all 18 lines OK).
2. **Signature and signer:**

   ```sh
   cosign verify-blob --bundle checksums.txt.sigstore.json \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     --certificate-identity "https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/$TAG" \
     checksums.txt
   ```

3. **Provenance** of `checksums.txt` and of two archives (for example `linux_amd64` and `windows_amd64`), also pinning the
   commit: `gh attestation verify <file> --repo Or4cu1o/mothdex-cli --signer-workflow Or4cu1o/mothdex-cli/.github/workflows/release.yml --source-ref "refs/tags/$TAG" --source-digest <full sha of the tag commit> --deny-self-hosted-runners`.
4. **SBOM:** each `<archive>.sbom.json` is valid JSON (`jq -e .`) and lists `golang.org/x/crypto`; it is covered by
   `checksums.txt`, so steps 1 to 3 already bind it to the signed release.
5. **Binary:** extract the `linux_amd64` archive; `./mothdex --version` prints
   `mothdex 0.1.0 (commit <7 chars of the tag commit>, <commit date>)`; the archive holds only the binary, `LICENSE`,
   `README.md` and `CHANGELOG.md` at its root, the binary with mode 0755.
6. **Installer against the published release** (the installer of the tag, never piped to a shell):

   ```sh
   curl -fsSLO "https://raw.githubusercontent.com/Or4cu1o/mothdex-cli/$TAG/install.sh" && less install.sh
   sh install.sh --version "$TAG" --dry-run    # downloads and verifies in a temp dir, installs nothing
   sh install.sh --version "$TAG" --bin-dir "$(mktemp -d)/bin"
   ```

   With `cosign` on the machine this also runs the signature check. On Windows: `install.ps1 -Version $TAG -DryRun`, then a
   real install into a scratch `-BinDir`. Real Batocera, Recalbox and RetroBat hardware is not covered by any automated check.
7. **A2 closes** when 1 to 5 pass on the real assets. **D1 to D3** close with the owner's recorded OK, the review list and
   the bot identity. Tick them in `qa/T-22-m1-acceptance.md` with the run id and the commit.

## 4. Rollback

- **The tag run failed before `publish`:** nothing was published. Fix on a branch, merge, and use a **new** tag
  (`v0.1.1`). Do not move or reuse `v0.1.0`: the ruleset forbids it, and a moved tag breaks everyone who already saw it.
- **The release is published but wrong** (bad asset, a failed check in section 3): mark it as a pre-release or draft
  (`gh release edit v0.1.0 --prerelease`) so `install.sh` stops picking it as `latest`, say so in the release notes, and
  ship `v0.1.1`. Deleting a release or tag is destructive and needs the owner's OK; with immutable releases it is not
  possible, by design.
- **Secrets:** the pipeline has no long-lived secrets (OIDC keyless signing), so nothing to rotate; the Sigstore entry
  stays in the public log.
- **Installed users:** `install.sh --uninstall` removes the binary and the launcher it placed; v0.1.0 has no data migration.

## 5. What was checked while preparing this page, and what was not

Verified: `actionlint` (the pinned image from mothdex-mcp CI) on all cli workflows, clean; `release.yml` read against the
artifact contract (names, 9 targets, `checksums.txt` with 18 files, bundle name, exact signer identity, release assets,
pre-release flag); `.goreleaser.yaml` read against the same contract and `scripts/verify-dist.sh`.

**Not verified:** `goreleaser check` and a snapshot build (goreleaser is not installed on this machine and is not built
here; the CI jobs `release-check` and `goreleaser snapshot` are the evidence); `release.yml` has never run on a tag; cosign
`verify-blob`, `gh attestation verify`, the `environment` approval flow and `--verify-tag` against a real release; the
installer against a real release, macOS, and real Batocera, Recalbox and RetroBat hardware.

**Gap closed by T-24:** `publish` used to verify attestations by signer workflow and tag only; it now also checks that the
tag still resolves to the built commit and passes `--source-digest` and `--deny-self-hosted-runners`, as mothdex-mcp does.
Not verified until the first tag run.
