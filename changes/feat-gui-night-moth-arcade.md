### Changed

- The GUI has the "Night moth arcade" look of `DESIGN.md` (D30). Colors: the new `bg`, `surface`, `surface.raised`, `frame`, `focus.bg`, `focus.ring`, `text`, `text.dim`, `moth`, `ok`,
  `warn` and `error` tokens. Shapes snap to a pixel cell P (2, 3 or 4 by size): tiles, rows, keys, bars, windows and buttons have a
  2-step notch at each corner, discs and pills a 3-step one, the focus ring is stepped outside the element, windows are `surface.raised`
  with a P frame over the scrim, and the ring width and progress height round to a multiple of P.
  - A menu cursor (a pixel arrow in the focus color) shows on the focused item of the Start menu, the action sheets and the choice lists;
    their text starts at 3u.
  - The progress bar is segmented (blocks of 3P with a P gap); the spinner is eight blocks of 2P around a square of 6P, one step per
    100 ms; the check, cross, exclamation, dash and the arrows of the hint bar are drawn on the pixel grid.
  - The wordmark `mothdex` is in the moth color and the crumbs after it in the text color. The moth sprite (two frames by Aquarela, T-83a,
    embedded and pinned in `internal/ui/gui/assets/PINS.md`; project-owned art, no notice) sits before the wordmark on Home and the
    splash, where its wings flap every 250 ms.
  - Home and the splash have a sky of still specks (one per about 5,200 px², seeded positions, never over a surface or text, inside the
    safe rectangle).
  - No tweening was ever there; nothing moves except the spinner and the splash moth.
- The render harness has the Splash (both wing frames, and in ru_ru) and the exit screen as scenes, for all three sizes.
- The display face is Handjet (SIL OFL 1.1, default instance, subset to Latin and Cyrillic, 54,900 bytes, pinned to a `google/fonts`
  commit in `internal/ui/gui/assets/PINS.md`). It draws the short labels only: the top bar title and wordmark, the Home tile labels, the
  Settings section headers and the calibration percent, at 35, 46 and 68 px (title x 1.35; the large text setting scales it like the
  others). Sentences, rows and game titles stay in Noto Sans, and a label with a glyph Handjet lacks is drawn whole in Noto Sans at the
  title size. The notice is `THIRD_PARTY/Handjet-OFL-1.1.txt`, the 7th file: `install.sh`, `install.ps1` and their tests list it, and the
  contract lines (`ports.md` section 6, `gui.md` section 3) name it. Release note: an `install.sh` or `install.ps1` downloaded before
  that release stops on the unknown notice file by design (fail closed), so fetch both installers from the release tag.
