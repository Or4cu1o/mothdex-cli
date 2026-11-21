# T-52: SDL2 spike report (`mothdex-gui`, purego binding)

Owner: Guindaste (CI), Vitral (binding). Contract: [gui.md](../contracts/cli/gui.md) section 2, "What the spike must prove".
The jobs below are `.github/workflows/gui-spike.yml` and `gui-spike-qemu.yml`; they stay as the GUI smoke test. The CI result
of the final commit of the PR is the evidence; this page records what each target proved and what it did not, with the
library versions, so that a later regression can be told from a library change.

## What the binding uses

23 SDL2 functions, all available since SDL 2.0.0: `SDL_Init`, `SDL_Quit`, `SDL_GetVersion`, `SDL_GetError`, `SDL_SetHint`,
`SDL_GetCurrentVideoDriver`, `SDL_GameControllerAddMapping`, `SDL_CreateWindow`, `SDL_DestroyWindow`, `SDL_CreateRenderer`,
`SDL_DestroyRenderer`, `SDL_SetRenderDrawColor`, `SDL_RenderClear`, `SDL_RenderFillRect`, `SDL_RenderCopy`,
`SDL_RenderPresent`, `SDL_RenderSetScale`, `SDL_RenderReadPixels`, `SDL_CreateTexture`, `SDL_UpdateTexture`,
`SDL_DestroyTexture`, `SDL_PushEvent`, `SDL_PollEvent`. The binding refuses SDL older than 2.0.0 (hard minimum) and is meant
for 2.0.18 or later. Only pointers, integers and floats cross the boundary.

## Results

| Target | How it ran | SDL2 | Probe (23 functions, 8 steps) | Package tests | ELF check |
|---|---|---|---|---|---|
| linux amd64 (Tier 1) | native, distro libsdl2, dummy driver | 2.32.10 | ok | PASS | interpreter `/lib64/ld-linux-x86-64.so.2`; NEEDED libdl, libpthread, libc only |
| windows amd64 (Tier 1) | `windows-latest`, `SDL2.dll` beside the exe | 2.32.10 | ok | PASS | not applicable (no readelf) |
| linux arm64 (Tier 1) | qemu-user, pinned Debian trixie-slim, `apt` libsdl2 | 2.32.4 | ok | PASS | `/lib/ld-linux-aarch64.so.1` |
| linux armv7 | same, `arm/v7` | 2.32.4 | ok | PASS | `/lib/ld-linux-armhf.so.3` |
| linux armv6 (best effort) | the `GOARM=6` binary on the armhf userland | 2.32.4 | ok | PASS | `/lib/ld-linux-armhf.so.3` |
| linux 386 (optional) | cross-build and `readelf` only | not run | not run | not run | `/lib/ld-linux.so.2` |

Probe steps: load, version check, mappings, window and renderer, atlas texture, draw, pixel read back (proves the float
arguments through `SDL_RenderSetScale`), synthetic `SDL_CONTROLLERBUTTONDOWN` pushed and polled back (event layout).
Package tests also check the struct layouts of `SDL_Rect`, `SDL_version`, `SDL_Event` and `SDL_ControllerButtonEvent`, and that an
event is 8-byte aligned (ARM EABI). `scripts/check-sdl-constants.sh` compiles a C program against the libsdl2-dev 2.32.10
headers and found the 8 binding constants and the struct layouts equal.

Sizes (CI): linux amd64 2,757,806 bytes; linux 386 2,496,537; windows amd64 3,190,272; arm64 2,729,866; armv7 2,590,079;
armv6 2,592,999 (all `-trimpath`, `CGO_ENABLED=0`).

Decision rule (gui.md): no Tier 1 target failed, so no escalation. arm32 passes on the armhf userland.

## Safety checks that run in CI

- `mothdex` stays free of purego and the GUI packages (`scripts/check-gui-isolation.sh`).
- The probe has a program interpreter, no RPATH or RUNPATH, and needs only the libc family.
- Library planting: on Linux a garbage `libSDL2-2.0.so.0` in the working directory and next to the probe is ignored; on Windows
  a garbage `SDL2.dll` in the working directory and first on `PATH` is ignored while the real one beside the exe loads, and a
  real DLL outside the exe folder (cwd, `PATH`) is not loaded (the probe exits 1, SDL2 not found).
- `SDL2.dll` comes from the exact URL pinned in `scripts/sdl2-windows.pin`, checked against its SHA-256 before extraction.
  The pin is trust on first use: libsdl-org signs the source archives only, not the binary zip (checked for 2.32.10).

## Unverified (open)

- **ARMv6 CPU model.** The probe on the `arm1176` model under qemu-user crashed with SIGSEGV (exit 139) at the run recorded in
  the PR. A pure Go `GOARM=6` binary runs fine on that model, and the CI step now also runs `/bin/true` and the dynamic loader
  of the Debian armhf userland on it, so the summary says whether the Debian userland (built for ARMv7) fails by itself.
  Honest status: attempted, crashed, cause not yet pinned, real ARMv6 hardware unknown.
- Real video drivers (KMSDRM, fbdev, Wayland), real game pads, real Batocera, Recalbox, EmuELEC and RetroBat images.
- Any SDL2 other than 2.32.x: no leg runs near the 2.0.18 target, so the refusal of an old SDL (`ErrTooOld`) is not run end to
  end, and the since-version of each function is checked by hand only.
- The no-binfmt qemu path (sysroot) did not run because binfmt works in containers; it also downloads unpinned `apt` packages
  inside the pinned image, which the release path must not copy (gui.md section 6).
- Windows: only the probe and the package tests; no PE check.
