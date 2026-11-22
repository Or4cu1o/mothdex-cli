# Third-party components of `mothdex-gui`

What the GUI binary carries or loads, with the licence of each and where it is pinned. `mothdex` (the CLI) carries none of
the first five rows. Go modules are listed by `go.mod` and the SBOM of each release archive.

| Component | Use | Licence | Pin |
|---|---|---|---|
| Noto Sans Regular, subset to Latin and Cyrillic | the body font and the fallback, embedded | SIL OFL 1.1, `NotoSans-OFL-1.1.txt` | release `NotoSans-v2.015`, hashes in `internal/ui/gui/assets/PINS.md` |
| Handjet Regular (default instance), subset to Latin and Cyrillic | the display face for short labels (D30), embedded | SIL OFL 1.1, `Handjet-OFL-1.1.txt` | `google/fonts` commit `c1eda9233c33`, `ofl/handjet`, hashes in `internal/ui/gui/assets/PINS.md` |
| SDL_GameControllerDB `gamecontrollerdb.txt` | controller mappings, embedded | zlib, `SDL_GameControllerDB-zlib.txt` | commit `4e87e3c6c257`, hash in `internal/ui/gui/assets/PINS.md` |
| `github.com/ebitengine/purego` | loads SDL2 at run time with no cgo | Apache-2.0, `purego-Apache-2.0.txt` | v0.11.3, exact, in `go.mod`; text copied from that module version |
| `golang.org/x/image` | font parsing and glyph rasterizing | BSD-3-Clause, `x-image-BSD-3-Clause.txt` | v0.48.0 in `go.mod`; text copied from that module version |
| SDL2 | the system library, loaded by soname (Windows: `SDL2.dll` next to the executable) | zlib, `SDL2-zlib.txt` (in every GUI archive; it covers the `SDL2.dll` of the Windows archive, while Linux SDL2 is the system's) | Windows: release 2.32.10, `scripts/sdl2-windows.pin`; text from `LICENSE.txt` of tag `release-2.32.10` |

The fonts stay under the OFL; the rest of the program is under the licence of the repository.
