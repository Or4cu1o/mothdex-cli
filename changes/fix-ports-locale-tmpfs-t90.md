### Fixed

- T-90 (ports.md PRT-G10): the interface language also follows the first entry of `LANGUAGE` when `LC_ALL`, `LC_MESSAGES`
  and `LANG` are all unset, or the one that decides is `C` or `C.UTF-8` (EmuELEC sets `LANG=C.UTF-8` and keeps the
  language only in `LANGUAGE`). A `C` locale with no `LANGUAGE` is still English, and a real language in `LC_ALL`,
  `LC_MESSAGES` or `LANG` still wins.

### Added

- T-90 (ports.md PRT-G11): when the games folder or the data folder is on a RAM disk (Batocera falls back to a 256 MiB
  tmpfs when its storage does not mount), the CLI says so at the start of every command and in `profile show` and
  `profile detect` (text and the `warnings` list of `--json`). The profile result has a new `StorageWarnings` field, kept
  apart from `Warnings` so a RAM disk does not make the profile "not verified"; the GUI reads the field in a separate change.
