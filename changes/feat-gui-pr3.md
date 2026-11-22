### Added

- T-78 (Medium, GUI PR 3): the History and Settings screens and the Buttons overlay of `docs/ux/gui.md` 5.9 to 5.11 (copy by Bússola in
  four languages; the other locales fall back to English).
  - **History** (Home tile or Start menu): every record newest first, from `History{}` read on open and after each terminal event; `Y` toggles
    All and Failed only; `A` opens Details with Download again and Close. Download again queues the same entry and link, or says
    `Already queued again.` when it is already in the Queue. A record that stopped on its real size (`confirmation_required`, with the
    Content-Length the history keeps) offers Download anyway instead and opens the T-56 dialog with the size shown, so it asks again
    rather than going out with no consent (backlog `history-retry-consent`); the TUI History does the same with a `[y/N]` question. A reason
    that does not fit the row wraps to a second line, and Details has the full text.
  - **Settings** through the config contract (`ConfigList`, `ConfigSchema`, `ConfigSet`, `ConfigReset`, `CacheStats`, `CacheClear`): every row
    of the table, with Left/Right to change a stepper or a list in place, `A` to open a list or flip a toggle, `X` to reset a value that
    comes from the file, the ordered editor of the preferred languages, the calibration view of the screen margin (corner brackets, live
    percent, `A` saves, `B` cancels), Clear cache with Keep as the default, and read-only System and About. A value set by the environment or a
    flag is read-only with `Set by <variable>` in the hint bar; `overwrite` is shown read-only and never offered or written; a service error
    shows under its row and the old value stays; values that apply on the next start say so. The settings the GUI consumes (confirm button,
    text size, margin, cover art, confirm large downloads, preferred languages and size) apply at once. A test keeps the table in step with
    the schema (keys, ranges, values).
  - **Buttons** overlay: the buttons of the screen underneath with the labels of the pad in hand and the words of that screen's hint bar.
  - The D-pad hints (`Left`/`Right`, the Move row) are drawn as triangles: the font has no arrows.
  - 18 render scenes of these screens in the T-67 harness, each listing the texts it must show.
  - Not in this change: the controller notices of 2.3 and the controller name and GUID in About (they need two more SDL calls).
