### Added

- T-114: every error shown by the command line, the shell and the terminal UI carries a 6-character `ref` and names the log file; `--json` has it as `error.ref` and `data.jobs[].error.ref`. A panic writes a crash file under `logs/crash/`.
