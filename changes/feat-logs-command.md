### Added

- T-114: every error shown by the command line, the shell and the terminal UI carries a 6-character `ref` and names the log file; `--json` has it as `error.ref` and `data.jobs[].error.ref`. A panic writes a crash file under `logs/crash/`.
- T-114: `mothdex logs` (`path`, `last`, `list`, `show`, `open`, `bundle`, `clear`) and `mothdex help logs`. The bundle is a zip of at most 10 MB without tokens, the API address or the home folder.
- T-114: `docs/beta-testing.md` explains how to send an error, and the bug report form asks for the ref and the bundle.

### Changed

- The bug report form moved from `bug_report.yml` to `bug.yml`.
