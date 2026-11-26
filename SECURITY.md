# Security policy

## Supported versions

The project is pre-alpha and has no stable release. Fixes go to `main`.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately through GitHub:
<https://github.com/Or4cu1o/mothdex-cli/security/advisories/new>

Include the affected version or commit, what you observed and how to reproduce
it. You can expect an acknowledgement within 7 days. Fixes are coordinated with the
reporter and disclosed within 90 days of the report at the latest.

## Scope

Of particular interest: writing files with server-provided names (path
traversal, reserved names, symlinks, overwriting), handling of downloaded
content, and the integrity of release artifacts and the CI pipeline.

## Verifying releases

Release files are listed in `checksums.txt`, signed with cosign (keyless) and
attested by GitHub. The exact signer identity and the verification commands are in
[docs/release.md](docs/release.md#verifying-a-download): the certificate identity
is the `release.yml` workflow at `refs/tags/vX.Y.Z` of this repository, with issuer
`https://token.actions.githubusercontent.com`.

The installers (`install.sh`, `install.ps1`) follow the same rule: they read
only from this repository's GitHub releases, verify the SHA-256 (and the
signature when cosign or `gh` is present) before installing. They may be run in
one line through a pipe, which runs the script before anything has checked it;
what that leaves to trust is written in
[ADR-0003](docs/adr/ADR-0003-pipe-install.md). Downloading, verifying and reading
the script first stays documented in the [README](README.md#install).

One documented exception: on a Batocera or Recalbox share partition formatted
as exFAT, FAT or NTFS, which has no file permissions, `install.sh` run as root
accepts a directory that looks world-writable, only inside the profile's own tree
(`/userdata` or `/recalbox/share`), only when the filesystem type is proven, and
it prints a warning. On such a partition any process or SMB client that can write
the folder can replace `mothdex`.

Directory checks in `install.sh`: the bin_dir and the launcher folder must not be world-writable and must be owned by
the installing user or root, and no ancestor of their physical path may be world-writable without the sticky bit.
Ancestors that are only owner- or group-writable are not checked. wget does not enforce https on redirects, so
with wget (no curl) a signature tool (cosign or a logged-in gh) is required and `latest` is not resolved.
