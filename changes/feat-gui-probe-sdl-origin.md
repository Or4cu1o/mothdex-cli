### Added

- `mothdex-gui --probe` and the start-up diagnostics line say where SDL2 was loaded from, as one word and never a path: `system`
  (a standard library directory), `ld_library_path` (a directory named in `LD_LIBRARY_PATH`), `other` or `unknown`; on Windows the
  word is absent (`n/a` in the diagnostics line). `--probe` prints `sdl 2.32.10 system` in one sanitized line of at most 200 bytes.
  On EmuELEC the Port inherits `/emuelec/lib` first while the probe runs scrubbed, so the two can load different libraries; the word
  makes that visible (PRT-G9, B12). The word comes from the process's own memory map, with no file written.
- The diagnostics line logs `LANGUAGE` as its first colon-separated entry only, as a word (PRT-G10, for the locale resolver).

### Fixed

- On Windows the GUI window is shown and raised explicitly after it is created: a process started hidden (RetroBat runs the Port's
  `.bat` with a hidden window style) can have the first show call answered with the start-up show state, so the window never appeared.
  It asks again once if the window is not shown, and logs how many calls it took (`sdl: window shown`, for B11) (PRT-G12).
