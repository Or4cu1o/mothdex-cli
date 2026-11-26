### Added

- T-115: `mothdex-gui --help` lists every flag, the exit codes and the environment variables, in the interface language
  (en_us and pt_br, also es_es and ru_ru).
- T-115: `mothdex-gui --log-level debug` is a shortcut for `MOTHDEX_LOG_LEVEL`.

### Changed

- T-115: **the exit codes of `mothdex-gui` changed**, to line up with the CLI: 1 internal error (was 3), 2 configuration
  error, now also for a bad flag (was 3), 8 SDL2 missing or too old, or no display (was 1). `mothdex-gui --probe` exits 8
  when the machine is not eligible (was 1). A script that tests for 1 or 3 must be updated.
- T-115: `mothdex-gui --help` and `-h` print to stdout, and a bad command line gets a translated message.

### Fixed

- T-115: an internal error (panic) of `mothdex-gui` is logged with its stack trace.
- T-115: when SDL2 cannot open the window (no display, an unknown `--driver`), `mothdex-gui` says why and exits with
  the SDL code instead of crashing.
