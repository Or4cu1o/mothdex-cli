# Third-party components of `mothdex-gui`

What the GUI binary carries or loads, with the licence of each and where it is pinned. `mothdex` (the CLI) carries none of
the first four rows. Go modules are listed by `go.mod` and the SBOM of each release archive.

| Component | Use | Licence | Pin |
|---|---|---|---|
| Noto Sans Regular, subset to Latin and Cyrillic | the only font, embedded | SIL OFL 1.1, `NotoSans-OFL-1.1.txt` | release `NotoSans-v2.015`, hashes in `internal/ui/gui/assets/PINS.md` |
| SDL_GameControllerDB `gamecontrollerdb.txt` | controller mappings, embedded | zlib, `SDL_GameControllerDB-zlib.txt` | commit `4e87e3c6c257`, hash in `internal/ui/gui/assets/PINS.md` |
| `github.com/ebitengine/purego` | loads SDL2 at run time with no cgo | Apache-2.0 | v0.11.3, exact, in `go.mod` |
| `golang.org/x/image` | font parsing and glyph rasterizing | BSD-3-Clause | v0.48.0 in `go.mod` |
| SDL2 | the system library, loaded by soname (Windows: `SDL2.dll` next to the executable) | zlib | Windows: release 2.32.10, `scripts/sdl2-windows.pin` |

The font stays under the OFL; the rest of the program is under the licence of the repository.
