### Added

- T-114: every run of `mothdex` writes its own log under `logs/sessions/`, and the newest one also as `logs/latest.log`, at the level of the new `log.session_level` setting (`debug` by default during the beta). `log.sessions_keep` (20) sets how many sessions are kept.
- T-114: the core behind error refs, crash files and the diagnostic bundle (`docs/contracts/cli/logs.md`), ready for the command line, the terminal interfaces and the GUI.

### Security

- Every log is redacted as it is written: the API address, credentials in URLs and authentication headers never reach `mothdex.log`, the session files or the console copy. A log folder that is a link or that other users can write to is replaced by the default one, or not used; folders on FAT, exFAT and NTFS, which have no permission bits, are used with a warning. Log files of another user or with a second hard link are refused, and secrets written as `key=value` are removed.
