# T1b - SDL2/SDL3 from Go without cgo (purego) for the fullscreen gamepad UI

Ref: T1b. Author: Sonda. Date checked: 2026-11-20. Supports: PLAN-v0 sections 2.2 (`cmd/mothdex-gui`), 2.6 (GUI), 2.7 (distribution). Research only; no production code written.

## 1. Question

Can `ebitengine/purego` load SDL2 or SDL3 without cgo on linux/amd64, linux/arm64, linux/arm (armv7) and windows/amd64? Which bindings exist? Which SDL major do the target frontends ship? Which gamepad API and font path should the GUI use? Single purego binary or separate cgo binary?

## 2. Short answer

- **Yes for linux/amd64, linux/arm64, windows/amd64** (purego Tier 1). **linux/arm (armv7) is Tier 2** (best effort, no struct-by-value, not a release blocker for purego); it builds with CGO_ENABLED=0 but is **not verified on hardware** here.
- A purego Linux binary built with CGO_ENABLED=0 is **not static**: it is a dynamic ELF needing glibc's `ld.so`, `libdl.so.2`, `libc.so.6` (observed in a throwaway build, see 4). Fine on Batocera/Recalbox/EmuELEC/ROCKNIX (glibc), not on musl. On linux/arm the Go linker's default interpreter `/lib/ld-linux.so.3` is wrong for hard-float glibc (`/lib/ld-linux-armhf.so.3`); needs `-ldflags "-I ..."`.
- **No purego binding for SDL2 exists** that I could find. The two maintained purego bindings are **SDL3-only and amd64/arm64-only** (no armv7).
- Frontend reality: **SDL2 is shipped everywhere (Batocera, Recalbox, EmuELEC, ROCKNIX); SDL3 only on Batocera (v44+/master) and ROCKNIX.** Recalbox/EmuELEC show no SDL3.
- **Recommendation:** separate `mothdex-gui` binary, built CGO_ENABLED=0, using a **thin own purego binding of ~40 SDL2 functions** (SDL_GameController + SDL_Renderer), SDL2 loaded from the system; software font raster in Go. Keep `mothdex` (CLI/TUI) a truly static binary. Conditions in 7.

## 3. Evidence

| # | Claim | Label | Source | Version | Checked |
|---|---|---|---|---|---|
| 1 | purego Tier 1: Linux amd64/arm64, Windows amd64/arm64, macOS; Tier 2: Linux 386/arm/riscv64..., Windows 386/arm | Confirmed | https://github.com/ebitengine/purego (README "Supported Platforms") | v0.11.3 (2026-11-03) | 2026-11-20 |
| 2 | Tier 2 linux/386 and linux/arm do **not** support passing structs by value as args/returns | Confirmed | same README, note 3 | v0.11.3 | 2026-11-20 |
| 3 | On Windows amd64/arm64 structs by value work for calls to C, but **not in `NewCallback` callbacks** | Confirmed | same README, note 2 | v0.11.3 | 2026-11-20 |
| 4 | purego works with CGO_ENABLED=0 on Linux via an internal "fakecgo"; also works with CGO_ENABLED=1 | Confirmed | README + https://github.com/ebitengine/purego/blob/main/nocgo.go | v0.11.3 | 2026-11-20 |
| 5 | Without cgo purego imports dlopen/dlsym via `//go:cgo_import_dynamic ... "libdl.so.2"`, so the binary has a dynamic loader dependency | Confirmed | https://github.com/ebitengine/purego/blob/main/dlfcn_nocgo_linux.go | v0.11.3 | 2026-11-20 |
| 6 | Built `GOOS=linux CGO_ENABLED=0` binary importing purego: interpreter `/lib64/ld-linux-x86-64.so.2` (amd64), `/lib/ld-linux-aarch64.so.1` (arm64), `/lib/ld-linux.so.3` (arm); NEEDED libdl.so.2 (+libpthread/libc on amd64/arm64) | Confirmed (my own trivial spike, readelf, Go 1.27.4) | local, throwaway in scratchpad | purego v0.11.3 | 2026-11-20 |
| 7 | Go linker default arm interpreter is `/lib/ld-linux.so.3`; `-ldflags "-I <path>"` overrides it (built OK with `/lib/ld-linux-armhf.so.3`) | Confirmed | https://github.com/golang/go/blob/master/src/cmd/link/internal/arm/obj.go ; spike | go 1.27.4 | 2026-11-20 |
| 8 | purego source handles float args on arm (hard-float padding, soft-float path) | Confirmed (source) / Unverified (on hardware) | https://github.com/ebitengine/purego/blob/main/func.go | v0.11.3 | 2026-11-20 |
| 9 | `purego.Dlopen` is **undefined** on windows (build error in spike); Windows needs the syscall/`LoadLibrary` path (README example "supports FreeBSD and Windows" in examples/libc) | Confirmed | spike; README | v0.11.3 | 2026-11-20 |
| 10 | purego license Apache-2.0; active (pushed 2026-11-10), 4.1k stars | Confirmed | https://github.com/ebitengine/purego | v0.11.3 | 2026-11-20 |
| 11 | Zyko0/go-sdl3: pure-Go SDL3 (v3.4.0) + ttf/image/mixer, purego; platforms windows/linux/macos amd64+arm64 only; MIT; v0.1.4 (2026-11-18), pre-1.0 | Confirmed | https://github.com/Zyko0/go-sdl3 | v0.1.4 | 2026-11-20 |
| 12 | JupiterRider/purego-sdl3: cgo-free SDL3 binding, "early stage", needs SDL >= 3.2.0, amd64/arm64 only; Unlicense; last push 2026-05-14, no GitHub releases | Confirmed | https://github.com/JupiterRider/purego-sdl3 | master | 2026-11-20 |
| 13 | veandco/go-sdl2: cgo, BSD-3-Clause, last commit 2025-02-20, tags only v0.5.0-alpha.* (last stable release v0.4.0 2020-01-19); static build via bundled libs needs a C toolchain | Confirmed | https://github.com/veandco/go-sdl2 | master | 2026-11-20 |
| 14 | A purego SDL2 binding: moonfdd/sdl2-go (3 stars, last push 2024-09, no license, tested Win10 only) is the only hit; not usable | Confirmed (search limited to GitHub) | https://github.com/moonfdd/sdl2-go | - | 2026-11-20 |
| 15 | SDL3 is current (release-3.4.20, 2026-11-03); SDL2 2.32.10 (2025-09-01) is the last SDL2 release | Confirmed | https://github.com/libsdl-org/SDL/releases | 3.4.20 / 2.32.10 | 2026-11-20 |
| 16 | sdl2-compat provides SDL2 ABI on top of SDL3 (so "SDL2" on a system may be SDL3 underneath) | Confirmed | https://github.com/libsdl-org/sdl2-compat | main | 2026-11-20 |
| 17 | Batocera master and branch 44 build both SDL2 (2.32.10 via Buildroot fork) and SDL3 3.4.14 (`BATOCERA_SYSTEM_VERSION = 44`); SDL3 selected only if Xorg or Wayland or GBM+EGL+GLES; branch 43 has no `sdl3.mk` | Confirmed | https://github.com/batocera-linux/batocera.linux (package/batocera/libraries/sdl3/sdl3.mk, package/batocera/core/batocera-system/Config.in, batocera-system.mk); https://github.com/batocera-linux/buildroot (package/sdl2/sdl2.mk) | 44 / master(45-dev) | 2026-11-20 |
| 18 | Recalbox 10.1.2 defconfigs (x86_64, rpi3, rpizero2, rpi5_64) reference SDL2 options (`BR2_PACKAGE_SDL2_OPENGLES`), no SDL3 option found; SDL2 exact version | Confirmed (SDL2 present, no SDL3 seen) / Unverified (version) | https://gitlab.com/recalbox/recalbox (configs/*_defconfig @10.1.2; no package/sdl3) | 10.1.2 (2026-11-17) | 2026-11-20 |
| 19 | EmuELEC dev: SDL2 2.32.10 package, `emuelec-emulationstation` depends on SDL2; no SDL3 package; Amlogic patch "mali-fbdev-add-driver" in SDL2 | Confirmed | https://github.com/EmuELEC/EmuELEC (packages/multimedia/SDL2/package.mk) | v4.8 / dev | 2026-11-20 |
| 20 | ROCKNIX (next) packages SDL2 2.32.10 and SDL3 3.4.10 | Confirmed (packages exist) / Inferred (both in image) | https://github.com/ROCKNIX/distribution (projects/ROCKNIX/packages/graphics/SDL2, SDL3) | next | 2026-11-20 |
| 21 | Lakka v6.1: no SDL package found in `packages/multimedia`; RetroArch-only | Inferred | https://github.com/libretro/Lakka-LibreELEC (tree Lakka-v6.1) | v6.1 | 2026-11-20 |
| 22 | RetroBat: Windows 10/11 64-bit only; ships its own EmulationStation; no system SDL to rely on | Confirmed (64-bit only) / Unverified (which SDL DLL it ships) | https://wiki.retrobat.org (get-started/prerequisites) | 8.3.1 | 2026-11-20 |
| 23 | muOS SDL major | Unverified | - | - | - |
| 24 | Batocera, Recalbox x86_64, EmuELEC use glibc toolchain (Batocera/Recalbox `BR2_TOOLCHAIN_BUILDROOT_GLIBC=y`; EmuELEC/Lakka are LibreELEC-based) | Confirmed (Batocera, Recalbox) / Inferred (EmuELEC) | board.common / defconfig files above | master / 10.1.2 | 2026-11-20 |
| 25 | SDL3 gamepad API: SDL_gamecontroller.h renamed SDL_gamepad.h; `SDL_GameControllerAddMappingsFromFile` -> `SDL_AddGamepadMappingsFromFile`; events `SDL_EVENT_GAMEPAD_*` | Confirmed | https://github.com/libsdl-org/SDL/blob/main/docs/README-migration.md | main | 2026-11-20 |
| 26 | SDL_GameControllerDB: community DB for SDL2 **and** SDL3, zlib, active (2026-11-17) | Confirmed | https://github.com/gabomdq/SDL_GameControllerDB | master | 2026-11-20 |
| 27 | Go text options: `golang.org/x/image` (BSD-3, active 2026-11), `go-text/typesetting` (active; license shown as NOASSERTION by API, must be checked before use), `golang/freetype` **archived** 2024-04, `fogleman/gg` no push since 2023-12 | Confirmed (status) / Unverified (go-text license) | GitHub repo pages | - | 2026-11-20 |
| 28 | SDL3 `SDL_RenderDebugText` is a fixed built-in debug font (Zyko0 example uses it); unusable for localized UI | Inferred | https://github.com/Zyko0/go-sdl3 README | v0.1.4 | 2026-11-20 |

## 4. Notes on the spike (trivial, clearly a throwaway)

Built a 6-line program calling `purego.Dlopen` with `CGO_ENABLED=0` for linux/{amd64,arm64,arm,386}, then `readelf -l -d`. Results are rows 6, 7, 9. Ran the amd64 binary only (ran OK). arm/arm64/windows binaries were **not executed**. Nothing was kept in the repo.

## 5. Options compared

| Option | Pros | Cons | Fit |
|---|---|---|---|
| A. Own thin purego SDL2 binding, separate `mothdex-gui`, CGO_ENABLED=0 | Matches what every target ships (SDL2); cross-compile all 4 targets from one CI box; no C toolchain; small surface (window, renderer, texture, events, GameController) | We own the binding (~40 funcs, struct layouts for SDL_Event, SDL_Rect); armv7 is purego Tier 2 and unverified; interpreter fix needed on arm; on distros where SDL2 is really sdl2-compat the behavior may differ slightly | **Best** |
| B. Zyko0/go-sdl3 or JupiterRider/purego-sdl3 | Ready-made, active (Zyko0) | SDL3 only (not on Recalbox/EmuELEC); no armv7 listed; pre-1.0; would need to ship our own libSDL3 per arch | Fallback for Batocera/ROCKNIX only |
| C. veandco/go-sdl2 (cgo) separate binary | Mature API | Needs C cross toolchains for 4 targets (armv7 glibc matching Batocera); stale (alpha tags, last commit 2025-02); static link of SDL2 on KMSDRM/Wayland is impractical | Not recommended |
| D. Single binary (CLI+GUI) with purego | One file | Forces CLI to be dynamic (loses static guarantee, breaks on musl/odd rootfs, arm interpreter issue hits the CLI too) | Not recommended |
| E. Ebitengine (pure Go, uses purego internally) instead of SDL | No own binding; full 2D engine | Different input stack (own gamepad DB), heavier, GL/Vulkan expectations on KMSDRM/GLES handhelds unverified | Unverified; out of scope here |

## 6. Gamepad and text

- **SDL2:** `SDL_GameControllerOpen`, `SDL_CONTROLLERBUTTONDOWN`, `SDL_GameControllerAddMappingsFromFile("gamecontrollerdb.txt")`. **SDL3:** `SDL_OpenGamepad`, `SDL_EVENT_GAMEPAD_BUTTON_DOWN`, `SDL_AddGamepadMappingsFromFile`. Both read the same SDL_GameControllerDB format. Embed the DB (zlib) and load it at start; the frontends (EmulationStation) may already feed their own mapping to child processes (Unverified, see open questions).
- **Fonts without SDL_ttf:** embed one OFL TTF, rasterize glyphs with `golang.org/x/image/font/opentype` (+`vector`) into an RGBA atlas, upload as one SDL texture. Cache by (rune, size). Need coverage for pt_br/es/ru at least (Cyrillic); CJK would need a second font. Shaping (`go-text/typesetting`) only if RTL/complex scripts are required; check its license first. Avoid `golang/freetype` (archived).

## 7. Recommendation and conditions

Option A, as a separate `cmd/mothdex-gui` built CGO_ENABLED=0, with the main `mothdex` kept fully static.

Conditions:
1. SDL2 loaded at runtime from the system (`libSDL2-2.0.so.0`, `SDL2.dll`); on failure print a clear message and exit non-zero (CLI/TUI keeps working).
2. linux/arm builds use `-ldflags "-I /lib/ld-linux-armhf.so.3"` (verify the path exists on Batocera/Recalbox armv7 images before release; **not verified**).
3. Real hardware smoke test before promising armv7 (Pi 2/3, H3, S812 boards, Recalbox rpi3/zero2 32-bit): window, renderer, GameController events, float args (`SDL_RenderSetScale`).
4. Avoid APIs that take structs by value or callbacks (keep to pointers/ints/floats): SDL_Event, SDL_Rect passed by pointer.
5. Ship SDL2.dll with the Windows/RetroBat package (RetroBat has no system SDL; which DLL RetroBat bundles is Unverified).
6. If SDL3 becomes the common denominator later, swap the binding, not the architecture (put binding behind a small `internal/ui/gui/sdl` interface).

## 8. Risks and limitations

- purego linux/arm is Tier 2: a bug there will not block a purego release.
- purego pre-1.0 ("beta software ... API breaking changes"): pin the exact version.
- Handheld stacks (KMSDRM, fbdev Mali on EmuELEC Amlogic) may need an SDL video driver hint; untested.
- A GUI run as a "Port" competes with EmulationStation for the display/input (see T1e); not tested.
- Only GitHub-visible bindings were searched; a private or non-GitHub SDL2 purego binding could exist (Unverified).

## 9. Open questions

1. Does the frontend stop ES and release the pad/display when launching a Port (Batocera, EmuELEC), and which SDL env vars (`SDL_GAMECONTROLLERCONFIG`, `SDL_VIDEODRIVER`) are set for the child? Not verified.
2. Which SDL2 version/driver set does Recalbox 10.1.2 and 11.0 ship (not just "present")? Unverified.
3. muOS and Bazzite (Fedora sdl2-compat?) SDL state: Unverified.
4. Locale scope for fonts (CJK?).
