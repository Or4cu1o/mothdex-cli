# Contract: GUI (`mothdex-gui`, E-M3)

Status: draft v1 (T-51) · Owner: Alicerce · Implementer: Vitral (after the spike) · Risk: Medium
(the spike and packaging are High: supply chain).
Sources:
- research: [`T1b-sdl2-purego.md`](../../research/T1b-sdl2-purego.md);
- PLAN-v0: §2.6, plus the §9 errata on 2.1/2.6;
- UX: `docs/ux/gui.md` (Bússola, T-50), whose screens and keys win;
- shared rules for long-lived UIs: [`ui.md`](ui.md).

This document fixes the engineering choices: binary, SDL loading, rendering, input, service use, packaging and tests.

## 1. Binary and packages

The GUI is a separate binary, `cmd/mothdex-gui`, built `CGO_ENABLED=0` with build tag `gui`. `mothdex` stays fully static.
- A CI check fails if `go list -deps ./cmd/mothdex` contains `purego` or `internal/ui/gui`.
- The `gui` tag also keeps the GUI out of the default `go build ./...` and the main test job.

| Package | Role | Imports |
|---|---|---|
| `cmd/mothdex-gui` | composition root: `compose.Open` (gap G1), the log, signals, SDL load, run loop | everything |
| `internal/ui/gui` | pure Go model: screens, focus, actions, layout, draw list. **No SDL, no purego** | `app`, `pkg/mothdex` types, `i18n`, `buildinfo` |
| `internal/ui/gui/input` | SDL-neutral input events → actions (button positions, keyboard, `confirm_button` swap) | none |
| `internal/ui/gui/text` | glyph atlas and measuring with `golang.org/x/image/font/opentype` | `x/image` |
| `internal/ui/gui/sdl` | the thin purego SDL2 binding plus a backend that executes a draw list. **Only package with purego** | `purego` |
| `internal/ui/gui/assets` | `embed` of the font, `gamecontrollerdb.txt` and their licences | none |

**Data flow.** The model reduces actions into state, and from the state builds a draw list of rects, text runs and textures by ID. The backend uploads and draws that list and turns SDL events into `input` events.
- Swapping SDL2 for SDL3 later changes only `sdl` (T1b §7.6).

**Service calls.**
- Service calls run off the render thread and report back as messages, the same pattern as the TUI.
- SDL calls stay on the main OS thread (`runtime.LockOSThread` in `main`).

## 2. SDL2 loading (the spike decides; recommendation A)

| Option | Verdict |
|---|---|
| **A.** Own purego binding of about 40 SDL2 functions; system SDL2 at runtime | **recommended** (T1b §7) |
| B. SDL3 purego bindings (Zyko0, JupiterRider) | no: SDL3 only, and no SDL3 on Recalbox or EmuELEC; no arm32 |
| C. `veandco/go-sdl2` with cgo | no: C cross toolchains for each target, stale, impractical static SDL; only if A fails on a Tier 1 target |
| D. One binary holding CLI and GUI | no: breaks the static CLI |

**Pins.**

| Component | Pin | Licence | Notes |
|---|---|---|---|
| `github.com/ebitengine/purego` | v0.11.3 (latest, rechecked 2026-11-21) | Apache-2.0 | pre-1.0, so pin it exactly |
| SDL2 | runtime minimum: the spike sets it from the functions used; target 2.0.18 or later | | refuse older versions with `SDL_GetVersion` |

**Binding rules.**
- Only pointers, ints and floats cross the boundary: no struct by value, no callbacks. `SDL_Event` and `SDL_Rect` go by pointer, and their layouts are checked per arch in tests.
- Mappings are added line by line with `SDL_GameControllerAddMapping`; there is no `RWops`.

**Library lookup.**
- **Linux:** `dlopen("libSDL2-2.0.so.0")` by soname only.
- **Windows:** `LoadLibraryEx("SDL2.dll", LOAD_LIBRARY_SEARCH_APPLICATION_DIR|LOAD_LIBRARY_SEARCH_SYSTEM32)`, never the current directory (DLL planting). Before that load, `SetDefaultDllDirectories` with the same two flags makes them the default of the whole process, because SDL later loads `vulkan-1`, `opengl32`, XInput and `hid` with a plain `LoadLibrary`, which would search the current directory and `PATH`; if the call fails, SDL2 is not loaded (T-70 IC-8, T-72). `purego.Dlopen` does not exist on Windows (T1b row 9), so use `x/sys/windows`.

**Interpreter on linux/arm.** Build with `-ldflags "-I /lib/ld-linux-armhf.so.3"`; the Go default `/lib/ld-linux.so.3` is wrong for armhf glibc. The path still has to be checked on Batocera and Recalbox armv6 and armv7 images.

**What the spike must prove.** There is no hardware. Each line below is a CI job kept afterwards as the GUI smoke test.
1. Cross-builds `CGO_ENABLED=0` for these targets, each with a `readelf` check of the interpreter and `NEEDED`:

   | Target | Note |
   |---|---|
   | linux amd64 | |
   | linux arm64 | |
   | linux armv7 (`GOARM=7`) | |
   | linux armv6 (`GOARM=6`) | |
   | windows amd64 | |
   | linux 386 | optional, Batocera `x86_wow64` |
2. A headless run on linux amd64 with distro `libsdl2` and `SDL_VIDEODRIVER=dummy` (or `offscreen`) and a software renderer, in this order:
   1. init;
   2. version check;
   3. load the mappings;
   4. window and renderer;
   5. upload an RGBA atlas texture;
   6. draw;
   7. `SDL_RenderReadPixels` checks one known pixel, which proves the float args too (`SDL_RenderSetScale`);
   8. `SDL_PushEvent` a synthetic `SDL_CONTROLLERBUTTONDOWN`, then `SDL_PollEvent` reads it back with the same fields (event layout).
3. The same run on linux arm64 and linux armv7 under qemu-user: `docker/setup-qemu-action` with Debian `arm64v8` and `arm32v7` images and their `libsdl2`.
   - armv6: the armv6 binary under `qemu-arm -cpu arm1176` with an armhf sysroot. Best effort and Unverified as a technique; a failure here is not a stop.
4. The same run on `windows-latest` with the pinned `SDL2.dll` (§6).
5. A report: the functions used, the SDL minimum, binary size, and every Unverified item still open (the real KMSDRM or fbdev drivers, real pads).

**Decision rule.**
- A fails on a Tier 1 target (linux amd64 or arm64, windows amd64): stop and escalate to Regente, with option C as the candidate.
- A fails only on arm32: ship v0.3.0 without `mothdex-gui` for arm32. The launcher there keeps `mothdex tui`, and there is no switch to cgo.
- Real-hardware smoke tests (T1b §7.3) come before arm32 is announced, and are listed in the release notes as Unverified until then.

## 3. Rendering, text and fonts

**Drawing.** Through `SDL_Renderer` only: fills, texture copies and the integer render scale.
- No SDL_ttf or SDL_image: one less system library that the frontends may lack (SDL_ttf on frontends is Unverified).

**Screen size** (agreed with Bússola). Layout uses the real window size:
- Reference 1280x720; supported minimum 640x480.
- Above 1200 px tall, draw the 1080p layout with an integer render scale.
- Below 640x480 (480x320 handhelds), draw the 640x480 layout through `SDL_RenderSetLogicalSize` scaled down; legibility Unverified.
- Size tiers by logical height: `compact` < 600, `standard` 600-899, `large` ≥ 900.
- `gui.safe_area` insets the layout (G2).

**Text.**
- **Fonts:** Noto Sans Regular for body text and as the fallback (one weight: Bússola uses size and colour, not bold), and Handjet for the `display` role (D30), both subset to Latin and Cyrillic.
  - Noto Sans: release `NotoSans-v2.015` from `notofonts/latin-greek-cyrillic`, licence OFL-1.1, embedded with `NotoSans-OFL-1.1.txt` in the archive.
  - Handjet: the default instance of `ofl/handjet` of `google/fonts` at commit `c1eda9233c33`, licence OFL-1.1 (no Reserved Font Name), embedded with `Handjet-OFL-1.1.txt` in the archive. It draws the top bar title and wordmark, the Home tile labels, the Settings section headers and the calibration percent; a label with a glyph it lacks is drawn in Noto Sans at the `title` size.
  - OFL fonts may be bundled with GPL-3.0 software; the fonts stay under OFL.
- **Raster:** `golang.org/x/image` v0.48.0 (BSD-3), `font/opentype` plus `vector`, into an RGBA atlas texture.
  - Cached by (rune, size) in fixed-size pages.
  - A page is rebuilt when it is full, which bounds memory.
  - Missing glyphs draw the font's `.notdef` box.
- **No shaping engine.** `go-text/typesetting` licence reads NOASSERTION on GitHub, so it is not used.
- **Locales:** en_us, es_es, pt_br and ru_ru render fully. zh_cn, ja_jp and hi_in fall back to en_us text with one notice (`ui.gui.locale_fallback`); CJK and Devanagari need a second font and shaping (backlog).
- **Measuring:** goes through the `text` package, so layout tests do not need SDL.

## 4. Input and GameControllerDB

**The database.**
- `gamecontrollerdb.txt` from `gabomdq/SDL_GameControllerDB` (zlib), pinned to commit `4e87e3c6c257` (2026-11-17). It is embedded and updated only by a reviewed PR that changes the pin and the file together.
- Load order, later overrides earlier:
  1. the embedded DB;
  2. `<DataDir>/gamecontrollerdb.txt`, if present (user fixes; a bad line is skipped with a warn log);
  3. the file named by `SDL_GAMECONTROLLERCONFIG_FILE`, then the lines of `SDL_GAMECONTROLLERCONFIG`, so a frontend that sets them for Ports keeps its mapping. SDL applies these two variables itself, but at `SDL_Init`, before any line we add, so lines added afterwards would replace them (found in the PR-71 review): the GUI reads both variables and adds their lines last. Whether the frontends set them is Unverified (T1b OQ1).
- Lines whose `platform:` does not match are skipped before calling SDL.

**Mapping.** Buttons map by position, agreed with Bússola:

| Position | Action |
|---|---|
| south | confirm |
| east | back |
| west | download |
| north | info |
| shoulders L/R | page |
| Start | menu |

- `gui.confirm_button=east` swaps confirm and back only.
- Hints show the label for the detected `SDL_GameControllerGetType`; an unknown type shows Xbox letters.
- The keyboard fallback (arrows, Enter, Esc, `x`, `y`, PgUp/PgDn, Tab, Backspace in text fields) goes through the same `input` reducer.
- Hot-plug is handled with `SDL_CONTROLLERDEVICEADDED` and `SDL_CONTROLLERDEVICEREMOVED`.

## 5. Driving the app service (long-lived UI)

**Composition.** The GUI builds the service with the same composition as the CLI (G1). It opens it once with `Downloads` on and `Force`/`AllowExecutable` false (ui.md §2.1), and closes it on exit.

**One download root (PR-41-arch).** The GUI always enqueues with `DestDir ""`, so all jobs share one root: the profile ROM root, or `download.dir`.
- `download.concurrency`, the space ledger and the batch name set are then global for the session, and **no shared limiter is needed for E-M3**.
- A per-job folder (Bússola G1, backlog) must stay inside that same root (`RomRoot/<folder>`), so this still holds.
- Proposal: close PR-41-arch with that constraint. The limiter returns only if a UI ever offers a second root.

**Calls.** The same as TUI §3: `Search`, `Entry`, `Links`, `Cover`, `Enqueue`, `Jobs`, `Events` (one subscription for the GUI's life), `Cancel`, `History`, `Config*`, `CacheStats`, `CacheClear`, `ActiveProfile`.
- An unmapped platform shows the `unmapped_platform` error with a pad hint.

**Batch boundary and refresh** (Bússola G3, confirmed against `downloads.go`):
- The GUI calls `Wait` only on exit. With no active job, `Wait` returns at once.
- It runs the frontend refresh once if any job reached Done since the last `Wait`, and it prunes finished jobs.
- Until then `Jobs()` keeps every job of the session, which is acceptable for a session. A guard of 1000 finished jobs is gap G4.

**Exit** (Bússola §7).
- The Start-menu Quit or SIGTERM starts a 2 s exit budget:
  1. `Cancel` the active jobs;
  2. `Wait`, with the refresh bounded by the remaining budget;
  3. `Close`, which flushes the history;
  4. exit.
- If the budget runs out, exit anyway; the next start's journal cleanup removes the `.part` files.
- Exit codes ([`cli-output.md`](cli-output.md) §4.1; `--probe` uses 0, 8 and 1):

  | Code | Meaning |
  |---|---|
  | 0 | normal exit, also after SIGTERM or SIGINT; `--help` and `--version` |
  | 1 | internal error, including a panic (the log line carries the stack) |
  | 2 | configuration error: configuration, flags or profile |
  | 8 | SDL missing or too old, or no display |

  **No display** includes SDL choosing its `offscreen` or `dummy` driver by itself, which it does when there is no
  display or the one named does not answer: the GUI would run unseen. A driver asked for with `--driver` or
  `SDL_VIDEODRIVER` (`SDL_VIDEO_DRIVER`) is used as asked. `--probe` starts no video subsystem (ports.md §5.2), so it
  does not check for a display.

  Releases before 0.2.0 used 1 for SDL, 2 for configuration and 3 for internal errors.
- Every failure of a run that opens the GUI, or tries to, is written to the session log, because a Port has no
  visible terminal, and also to stderr with its code, ref and log path.

**Command line.** `--config`, `--profile` and `--lang` mean what they mean in the CLI. `--log-level` is the shortcut
of `MOTHDEX_LOG_LEVEL` (`log.level`, flag over variable over file). `--help` lists the flags, exit codes and variables
in the language of `--lang`, else `interface.language`, else the system's (en_us fills what a locale lacks); it loads
no SDL2 and writes nothing. Flags take one dash or two. The program takes no positional argument: one is a command
line error (exit 2).

**Logs and errors** ([`logs.md`](logs.md)). `main` starts the session before the configuration is read, with mode
`gui`; `--probe`, `--version`, `--spike` and `--help` write no file. The header shows the value of `--profile`,
`--lang`, `--log-level`, `--window` and `--driver` only.
- **Before the service exists** (bad command line, configuration that does not load), the lines go to the session
  file and `latest.log` in the default `paths.logs`, never in a folder the rejected file names.
- **On screen**, every error gets a ref when it happens (`Update`, never while drawing) and one `ERROR` line in the
  log. An error in place shows the message, the hint, `code · ref` and the log path; a toast or a row line puts the
  code and the ref where a cut cannot take them. Each failed download gets its own ref, shown in Details.
- **Panic.** `recover` writes the `ERROR` line with ref, value and stack, and `crash/<session>.txt`, both through the
  redactor; stderr shows the ref and the path, never the value. When the panic is in the run loop, the same window
  shows the **crash screen**: what happened, `code · ref`, the log, and `Export logs` and `Quit`. It calls no
  service. Leaving it exits 1, after giving the service 2 s to close. A panic before the window, or on another
  goroutine (G-LOG-1), has no screen.
- **Export logs** (Settings › About, and the crash screen) asks first, with `Cancel` focused, writes the bundle of
  `mothdex logs bundle` (local files only) off the render thread, and says where the zip is.
- **Testing a crash:** `go build -tags 'gui crashtest'` makes a binary that panics on its second frame. No release is
  built with the tag.

**Version notice and Update** ([`update.md`](update.md) §5). The composition root gives the model an updater only
when `SkipReason` is empty (`update.check`, `MOTHDEX_NO_UPDATE_CHECK`, `CI`, a development build and an unmanaged
installation turn it off: no notice, no action, no request). The model sees a narrow `Updates` interface; every rule
stays in `internal/update`.
- One check per run, started once Home is up, within 3 s. A newer version gives a notice (recorded with
  `MarkNotified` when shown) and, in Settings › About, the version, the release page and `Update`.
- `Update` downloads and verifies (B or a signal cancels, changing nothing), then asks with `Cancel` focused,
  showing the signature result and the notes of the verified archive. Only that yes installs. While it installs
  the screen takes no action, and a program told to leave waits for it (up to 1 minute).
- After it, `Later` or `Restart`; Restart goes through the quit questions, closes SDL and the service, and runs the
  new `mothdex-gui` by its absolute path with the same arguments (TM-40).
- `main` calls `update.CleanupOld` for a run that opens the GUI.

**Console window on Windows.** The release build does not pass `-H=windowsgui`, so Windows opens a console window
along with the GUI. Kept on purpose during the beta (it shows start-up errors to a tester); to review for 1.0.

**Covers.**
- `Catalog.Cover` gives the cached reader, already checked by the fetcher (≤ 5 MiB, ≤ 4096×4096, magic bytes agree).
- The GUI decodes again with the same caps (defense in depth: the cache directory is user-writable) through `app.DecodeCover` (G2).
- One frame only. Decodes run one at a time on a worker, and each result is scaled at once to fit its tile (≤ 512 px side) with `x/image/draw`.
- Textures sit in an LRU of at most 48 tiles (about 48 MiB).
- When there is no cover or the decode fails, the tile shows the placeholder.
- WebP needs `golang.org/x/image/webp`; it comes with the same `x/image` module.

## 6. Packaging (for Guindaste; High, supply chain)

**GoReleaser.** A second build `id: mothdex-gui`:
- `main: ./cmd/mothdex-gui`, `tags: [gui]`, `CGO_ENABLED=0`, `-trimpath`;
- the arm `-I` ldflag of §2;
- targets: linux amd64, arm64, armv7, armv6, and windows amd64 (386 only if the spike passes it); no darwin and no windows arm64 in v0.3.0.

**Archives.** `mothdex-gui_<version>_<os>_<arch>`, never inside `mothdex_` archives (release-artifacts §1).
- Each holds the binary, `LICENSE`, `README.md`, `CHANGELOG.md` and the `THIRD_PARTY/` folder (the repository layout; ports.md PRT-G5): purego Apache-2.0, x/image BSD-3, Noto OFL-1.1, Handjet OFL-1.1, GameControllerDB zlib, SDL2 zlib (which covers the SDL2.dll in the Windows archive). The installer copies it to `bin_dir/mothdex-licenses/`.
- Each has an SBOM and is listed in the same `checksums.txt` and cosign bundle.
- `release-artifacts.md` must change "All binaries are static" to "`mothdex` is static; `mothdex-gui` is dynamic on Linux (glibc `ld.so`, `libdl`) and needs system SDL2". Guindaste's PR.

**Windows `SDL2.dll`.** Official libsdl-org release 2.32.10 `SDL2-2.32.10-win32-x64.zip`, pinned by URL plus SHA-256 in the repo, verified in the release job, and put next to the exe.
- Never fetched unpinned.
- The release workflow must not cache it (T-4-audit).
- Sentinela reviews this step.

**`install.sh` / `install.ps1`.** The Port details (launcher template, eligibility probe, flags, upgrade and uninstall) are in [`ports.md`](ports.md), which wins where the two differ.
- `--gui` (default on once the GUI ships, which flips D22) installs `mothdex-gui` next to `mothdex` when the archive exists for the arch.
- On Linux, the installer proves that the GUI can start by running `mothdex-gui --probe` from `bin_dir` with a scrubbed environment (ports.md §5.2). It replaces the earlier `ldconfig -p` check: one run proves the interpreter, the userland arch and the SDL2 soname together, and does not need `ldconfig`. If the probe says no, the installer warns with its reason and skips the GUI.
- The launcher runs `mothdex-gui` when it is installed, else `mothdex tui` (profiles §4 already says so).
- Checksum and signature verification are as for `mothdex_`.
- WinGet, Scoop and Homebrew stay CLI-only until E-M4.

## 7. Test plan (testing rule)

**Pure rules** (table-driven, in packages without the `gui` tag):

| Rule | Cases |
|---|---|
| `input` reducer | positions → actions, the `confirm_button` swap, keyboard parity, hint labels per controller type |
| size tiers and layout | 480x320, 640x480, 1280x720, 1920x1080, 3840x2160 → tier, scale and safe area |
| text | measuring and truncation with a fake face; atlas page eviction |
| GameControllerDB | line filter: comments, foreign platform, malformed |
| `app.DecodeCover` limits | over-size bytes, over-size pixels, magic mismatch, an animated GIF gives its first frame |
| exit budget | the order of calls on a fake service with a fake clock, including a stuck `Wait` |

**Model tests** (a handful, fake service, assertions on state and draw list *content*, such as "a text run with X exists", never on pixels or full snapshots):
- Home → Search → Results;
- an `Events` sequence updates the queue;
- an unmapped platform shows the error;
- SIGTERM drives the exit sequence.

**Struct layout** (tag `gui`): `unsafe.Sizeof` and `Offsetof` of the mirrored `SDL_Event` and `SDL_Rect` against values from the SDL2 headers, per `GOARCH`.

**Smoke** (tag `gui`, CI only): the spike jobs of §2.2-§2.4 stay as the GUI smoke test.

No test needs a real display, pad or hardware. The hardware checks are a manual release checklist.

## 8. Gaps

App and composition items for Forja:

| Item | Need |
|---|---|
| G1 | extract `cmd/mothdex/compose.go` `open` into `internal/compose` (UI-neutral `Session`; `cli.Session` wraps it), so both binaries build the service identically |
| G2 | `app.DecodeCover(r io.Reader, contentType string, maxSide int) (image.Image, error)`, reusing `cover.MaxBytes`/`MaxPixelSide`, one frame, adding `x/image/webp`; config keys `gui.confirm_button` (`south\|east`, south), `gui.text_size` (`normal\|large`, normal), `gui.safe_area` (`auto\|0..10`, auto) in the schema, plus F1 metadata (ui.md §5) |
| G3 | none, confirmed: `Wait` on idle returns at once and refreshes when `doneSinceWait > 0`; add a regression test if there is none |
| G4 (backlog) | cap finished jobs kept between `Wait` calls (prune the oldest after 1000) |

Other items:
- **Bússola G1** (backlog): a folder picker for unmapped platforms, inside the same root (§5).
- **Research for Sonda via Regente:**
  - the locale the frontends set for Ports (Batocera `system.language`);
  - `SDL_GAMECONTROLLERCONFIG` and `SDL_VIDEODRIVER` for Ports;
  - SDL2 on Recalbox and muOS;
  - SDL_ttf presence (only for the record).

## Opening from the CLI (T-112)

Use `mothdex gui` or `mdex gui` when the alias is installed. The CLI runs the GUI from its own installation folder and forwards `--config`, `--profile` and `--lang` using the same two-dash spelling. For example: `mothdex gui --config ./mothdex.yaml --profile retrobat --lang pt_br`. The child exit code is preserved. On Unix, cancellation forwards an interrupt and allows five seconds for cleanup; cancellation and signal termination map to exit 130. On Windows, process cancellation uses the supported kill operation. `--json` is rejected for this interactive command. When the GUI is missing, reinstall without `-NoGui` / `--no-gui`, or use `mothdex tui`.
