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
signature when cosign or `gh` is present) before installing, and are meant to be
downloaded and read, never piped to a shell. See
[docs/release.md](docs/release.md#installing).

One documented exception: on a Batocera or Recalbox share partition formatted
as exFAT, FAT or NTFS, which has no file permissions, `install.sh` run as root
accepts a directory that looks world-writable, only inside the profile's own tree
(`/userdata` or `/recalbox/share`), only when the filesystem type is proven, and
it prints a warning. On such a partition any process or SMB client that can write
the folder can replace `mothdex`.
