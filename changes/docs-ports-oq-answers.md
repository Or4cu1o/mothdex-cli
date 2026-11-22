### Changed

- T-87: `ports.md` §9 answers OQ-PRT-1 to OQ-PRT-9 from Sonda's T-84 research (now `docs/research/T-84-ports-open-questions.md`),
  each with its confidence and source; §2 to §8 follow: a Port has no terminal, EmuELEC prepends `/emuelec/lib:/emuelec/lib32`
  to `LD_LIBRARY_PATH` and exports `SDL_GAMECONTROLLERCONFIG_FILE`, Batocera passes `SDL_GAMECONTROLLERCONFIG` and its locale,
  Batocera runs a Port with `bash`, EmuELEC 4.8 is aarch64-only, and `HOME` falls back to RAM when storage fails. The beta items
  gain tester steps and B11 to B13; §10 proposes PRT-G8 to PRT-G12. Recalbox keeps `launcher: none`; OQ-PRT-10 stays deferred.
