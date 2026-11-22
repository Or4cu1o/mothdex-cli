### Added

- T-73 (High, PRT-G3 of `ports.md`; Sentinela T-70 IC-10 and IC-13): `mothdex-gui --probe` is the eligibility check the installer runs. It
  loads SDL2 as a normal start does and checks the version; it opens no window, starts no video or input subsystem, reads no
  configuration and writes no file or log (nothing runs before it but flag parsing). It prints `sdl X.Y.Z` and exits 0, or one line of
  reason on stderr (control characters removed, at most 200 bytes) and exits 1; it also gives up by itself after 15 s, so a library that
  blocks does not hang the installer. The old full check of the binding is now `-spike` (an executable named `mothdex-gui-probe` still
  does it with no flag, so the CI legs are unchanged). The start-up log has one `gui: diagnostics` line: the SDL version and the video
  driver, `LD_LIBRARY_PATH`, `SDL_VIDEODRIVER`, the two `SDL_GAMECONTROLLERCONFIG*` variables, `LD_PRELOAD` and `SDL_DYNAMIC_API` as `set` or
  `unset` on every system, the values of `SDL_VIDEODRIVER`, `LANG` and `LC_ALL` only when they are plain words, the mapping lines added from
  each source and the type of each controller; no path and no other value. The spike jobs check `--probe` with and without SDL2 on Linux
  (scrubbed environment, `timeout 20`) and on Windows (the environment `install.ps1` gives it).
