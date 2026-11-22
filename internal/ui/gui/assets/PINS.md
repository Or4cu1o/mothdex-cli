# Pins of the embedded GUI assets

Fonts and images are data, never executed. A change to any of them is a reviewed PR that changes the pin here and
the file together; `assets_test.go` fails if they disagree.

## `NotoSans-Regular-subset.ttf`

- Source: release `NotoSans-v2.015` of `notofonts/latin-greek-cyrillic`, asset `NotoSans-v2.015.zip`
  (SHA-256 `0c34df072a3fa7efbb7cbf34950e1f971a4447cffe365d3a359e2d4089b958f5`), member
  `NotoSans/unhinted/ttf/NotoSans-Regular.ttf` (SHA-256 `f3961a9cde016d41a4879aecda1474d3a36d6bf54fa0e4643de029cc2248b0e8`).
- Licence: SIL OFL 1.1, `THIRD_PARTY/NotoSans-OFL-1.1.txt` (the `OFL.txt` of the same release, SHA-256
  `cee9892f9f0cc8fe882c9e9537ee6a89621d86ee7ceaf70b02e2b2b1c25c061a`). The font stays under OFL; the rest of
  the program is GPL-3.0.
- Subset (fonttools, Latin and Cyrillic, no layout tables, no hinting):

  ```sh
  uv run --with fonttools pyftsubset NotoSans-Regular.ttf \
    --unicodes="U+0020-007E,U+00A0-024F,U+0400-04FF,U+2010-2027,U+2030-203A,U+2190-2193,U+20AC,U+2212,U+00B7" \
    --layout-features="" --no-hinting --drop-tables+=DSIG,GPOS,GSUB,GDEF \
    --output-file=NotoSans-Regular-subset.ttf
  ```

- Result: 59,800 bytes, SHA-256 `ac3fc986ac93d2aeac30103eff750b87611da0f3460d1ddce4a215c1bf5cdf01`.
- The font has no check mark, cross or backspace symbol, and no kerning table (x/image reads `kern`, Noto
  keeps its kerning in GPOS), so the GUI draws the symbols it needs as shapes.

## `gamecontrollerdb.txt`

- Source: `gabomdq/SDL_GameControllerDB`, commit `4e87e3c6c2574f49e1cdf8526b6f829b61e5ccb3` (2026-11-17), file
  `gamecontrollerdb.txt`, unchanged. Licence: zlib, `THIRD_PARTY/SDL_GameControllerDB-zlib.txt` (SHA-256
  `97f35b302b361680ec1e891e95d2d52097bb95abff361434916d99dc1305f127`).
- SHA-256 `a9994e85b0d608cfcb4df0e1424523e488829acb1017b6a2a2adf8df6478cb26`, 614,810 bytes.
- Lines whose `platform:` is not the running one are skipped before SDL sees them.

## `Handjet-Regular-subset.ttf`

- Source: `google/fonts`, commit `c1eda9233c33ad7775b27efd794f931095cf6133` (the last one that touched `ofl/handjet`,
  2026-03-03), file `ofl/handjet/Handjet[ELGR,ELSH,wght].ttf` (SHA-256
  `9262749e8bb0b73ebcae0e20428689c3c59576eebeb6c4e1020300d2d41bdf4d`, 283,912 bytes), a variable font.
- Licence: SIL OFL 1.1, `THIRD_PARTY/Handjet-OFL-1.1.txt` (the `OFL.txt` of the same directory and commit, byte for byte,
  SHA-256 `934a45d3b43276e1f857301e9cb07e00b829cca3d0c13e8e6f8c6790dd6c8a2c`). The notice declares no Reserved Font Name
  (its copyright line is "The Handjet Project Authors"), so a subset may keep the name. The font stays under OFL.
- The default instance (all three axes at their default), then the same subset as Noto:

  ```sh
  uv run --with fonttools fonttools varLib.instancer "Handjet[ELGR,ELSH,wght].ttf" ELGR=drop ELSH=drop wght=drop \
    -o Handjet-static.ttf
  uv run --with fonttools pyftsubset Handjet-static.ttf \
    --unicodes="U+0020-007E,U+00A0-024F,U+0400-04FF,U+2010-2027,U+2030-203A,U+2190-2193,U+20AC,U+2212,U+00B7" \
    --layout-features="" --no-hinting --drop-tables+=DSIG,GPOS,GSUB,GDEF \
    --output-file=Handjet-Regular-subset.ttf
  ```

- Result: 54,900 bytes, SHA-256 `2ba7ebeb0d6cd9a7e962b6cbf608e04a28c799564422144099bec4c306fd057c`.
- Checked glyphs: `ç ã õ ñ ¿ ё й ъ О П › … ·` (pt_br, es_es, en, ru_ru). The em is 8160 units: ascenders reach 0.71 em, accented
  capitals 0.82 em, descenders 0.24 em. The face is used only for short labels (`display`).

## `moth-wings-down.png` and `moth-wings-up.png`

- Source: the T-83a set drawn by Aquarela (approved by Bússola, 2026-11-22): the two frames of the moth sprite of
  DESIGN.md, 16 x 12 cells, RGBA with binary alpha, in `moth`, `moth.deep` and `moth.light`.
- Original project-owned art (D30), shipped under the licence of the repository: no notice in `THIRD_PARTY/`.
- `moth-wings-down.png`: 231 bytes, SHA-256 `a418676ca1027c283f2caa3d08c4879ddbfb3f3667bf7ef568cbf804368cf953`.
  `moth-wings-up.png`: 207 bytes, SHA-256 `396e2413e23030ca4ff7bf093e7147bcbb4bc094cb40c7772c7aa83b1a19e87c`.
- The GUI draws them as cell rectangles (one cell is `P`), never smoothed. The splash alternates the frames every
  250 ms while it shows; Home and the top bar show the wings-down frame.
