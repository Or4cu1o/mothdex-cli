# Security policy

## Supported versions

The project is pre-alpha and has no stable release. Fixes go to `main`.

## Reporting a vulnerability

Please **do not open a public issue**. Report privately through GitHub:
<https://github.com/Or4cu1o/mothdex-cli/security/advisories/new>

Include the affected version or commit, what you observed and how to reproduce
it. You can expect an acknowledgement within a few days.

## Scope

Of particular interest: writing files with server-provided names (path
traversal, reserved names, symlinks, overwriting), handling of downloaded
content, and the integrity of release artifacts and the CI pipeline.
