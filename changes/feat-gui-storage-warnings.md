### Added

- Settings > System shows the storage warnings of the profile (PRT-G11) as lines of their own in the warn color, with no
  "Not verified" prefix: a RAM disk is not a verification problem, and the Verified line does not depend on it. They are the
  plain texts that profile detection gives (`Games folder is on a RAM disk: ...`, `Data folder is on a RAM disk: ...`).
  A render scene (`settings-system-ram`) shows it at the three sizes.

### Fixed

- `mothdex-gui --probe` and the diagnostics line read a relative `LD_LIBRARY_PATH` entry from the current directory, as the
  loader does: `LD_LIBRARY_PATH=lib` with the library in `./lib` is `ld_library_path`, not `other`.
