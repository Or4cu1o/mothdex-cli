### Added

- T-101: `mothdex-gui --version` prints the same version line as `mothdex version` (name, version, short commit and
  date, from the ldflags the release build already passes to the GUI) and exits 0, without loading SDL2 or reading the
  configuration.
- T-101: the GUI window shows the moth app icon (T-83a) through `SDL_SetWindowIcon`, on drivers that support window
  icons (X11 and Windows; Wayland and the dummy driver ignore it). With an SDL2 older than 2.0.5 the window keeps the
  default icon, and the minimum SDL2 version does not change.
