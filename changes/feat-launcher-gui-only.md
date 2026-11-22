### Changed

- T-88 (PRT-G8, D32): the Port launcher is written only when the GUI is installed, as a `v3` template that runs `mothdex-gui` and nothing else (a Port has no terminal, so the `mothdex tui` fallback is gone). Without the GUI
  (not eligible, a probe error, `--no-gui`, no GUI archive for the machine) no launcher is written, and an existing launcher that carries one of our markers (`v1`, `v2` or `v3`) is removed with a note and the frontend is asked
  to reload; a file without our marker, or a link, is never touched. A `v2` launcher is upgraded to `v3` when the GUI is installed; `--no-launcher` still stops the installer from writing or rewriting it; uninstall removes a marked
  launcher of any version. `--launcher` is kept and means the default. D22 now reads "launcher on when the GUI is installed". `install.sh` and `install.ps1` (RetroBat `Mothdex.bat`) with the tests for every path (fresh, upgrade from
  `v2`, `--no-gui`, a failing probe, no GUI archive, `--no-launcher`, no marker, a link, dry run).
