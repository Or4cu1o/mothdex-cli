# T-84 — Ports open questions

**Question:** resolve `ports.md` §9 sufficiently to inform installer and launcher decisions. **Risk:** Medium. **Researcher:** Sonda. **Checked:** 2026-11-22 UTC throughout. No device or downloaded OS image was tested; no project repository files were changed.

Contract read on GitHub main: [`ports.md`, commit 649e38fe13565275d85370a6c92d57dc335b87d3][contract], particularly §§2, 4, 5 and 9. This report recommends; Regente/Alicerce/owner decide any contract change.

## Short answer

- **Confirmed / Verified from source:** Batocera's shell generator invokes **`/bin/bash script`**, so the script itself does not require an execute bit. Its child gets generated `SDL_GAMECONTROLLERCONFIG`. EmuELEC exports `SDL_GAMECONTROLLERCONFIG_FILE` and prepends `/emuelec/lib:/emuelec/lib32:` to its inherited library path. [B-shell][B-shell] [B-mapping][B-mapping] [E-profile][E-profile]
- **Confirmed / Verified from source:** ES remains a running parent around launches. Closing input/rendering is conditional, and is **not unconditional resource release** on the inspected Batocera, EmuELEC and RetroBat frontend sources. The actual GUI obtaining focus/display/pad remains a hardware check. [B-launch][B-launch] [B-window][B-window] [E-launch][E-launch] [R-launch][R-launch]
- **Confirmed / Verified from source:** Linux Port stdout/stderr are redirected; the launch path creates no terminal emulator or PTY. **Inferred:** the current TUI fallback is not a usable normal frontend experience. RetroBat's Windows generator requests a hidden `.bat` window. [B-process][B-process] [B-command][B-command] [E-process][E-process] [R-exe][R-exe]
- **Documented:** supported EmuELEC after 3.9 is aarch64-only. Its lib32 packages do not establish a 32-bit ES/shell userland. [E-readme][E-readme] [E-lib32][E-lib32]
- **Confirmed / Verified from source:** normal homes are Batocera `/userdata/system`, EmuELEC `/storage`, Recalbox `/recalbox/share/system`. Persistence depends on a successful disk/network mount; all three contain RAM-backed cases. [B-root][B-root] [E-service][E-service] [C-passwd][C-passwd] [B-share][B-share] [E-storage][E-storage] [C-share][C-share]
- **Confirmed / Verified from source:** Recalbox 11 has game-specific ports and a Ports collection. **Unknown:** a supported arbitrary shell-Port installation route. This is insufficient evidence to enable a Mothdex Port there. [C-doom][C-doom] [C-systems][C-systems] [C-notes][C-notes]

## Evidence scope and confidence

“Verified from source” means **Confirmed** code/configuration behavior at the cited revision, not a successful run on hardware. “Documented” means **Confirmed** official documentation. “Inferred” identifies reasoning across those sources. “Unknown” means **Unverified**; it never means absent.

| System | Version/revision actually inspected | Limit |
|---|---|---|
| Batocera | master `15820e4000df65d140b63ee2d0f14ca6238d2b35`; ES `fbd9423d09b6094601ea129338bf1e546b97035c`, selected by that tree's package recipe [B-ES-pin][B-ES-pin] | Master identifies itself as development toward 45 [changelog][B-changelog]. **Do not backport these results to 41/42/43 without checking those tags.** |
| EmuELEC | v4.8 / `02b45fe53b85c45c75dbad8e44a648092dec2e72`; ES `09356d0dae6291008a074d8ac4f8c20678d822d2`, selected by its recipe [E-ES-pin][E-ES-pin] | Applies to the named device cases in the recipes, not every unofficial image. |
| RetroBat | setup v7.0.0 / `fc851fdcbc1ab794f417845f065424a3dfca3405`; frontend master as of release day `86561b0fb0bd0fdb2c873d4f9897c1bed655784f`; launcher `b30c0a921123dd175fd879d4c91125cdcc3e5a8d` (2025-01-30) | **Inferred release correspondence** for component code. Setup points at rolling component assets; no immutable mapping to the installer binary was established. Results require verification on a named installed 7.x build. [R-component-urls][R-component-urls] |
| Recalbox | 11.0-patron-1 / `1392898ad3d3dd82f9631e488d7ab19e7ea9d313` | A named 11.0 tag, not a claim about every subsequent 11.0 build. |

All evidence links below contain a commit or tag and a file/line anchor where useful. All were checked on the date above. Primary repositories were read through the GitHub connector, web, or temporary read-only research checkouts outside the project clone. No forum evidence was needed.

## OQ-PRT-1 — lifecycle, environment, terminal

| System/version | Answer and confidence | Evidence |
|---|---|---|
| Batocera master | **Verified from source:** `launchGame` deinitializes audio, calls `window->deinit(HideWindow)`, waits for the child, then reinitializes. ES does not normally exit. Input and renderer deinit happen only when `HideWindow=true`; default is false except RK3128. Thus “ES always stops and releases pad/display” is refuted for this revision. A compositor can retain display ownership while the child is a client. | [FileData.cpp:701][B-launch], [Window.cpp:188][B-window], [Settings.cpp:354][B-settings] |
| Batocera master | **Verified from source:** shell command is Bash plus ROM script. Child environment is `os.environ` merged with command additions, including controller mapping and `SDL_RENDER_VSYNC`. No shell-generator override of `LD_LIBRARY_PATH` was found. **Unknown:** exact inherited library-path value on a specific boot/custom configuration; do not equate “not set here” with “unset.” | [sh.py:36][B-shell], [emulator.py:864][B-mapping], [command.py:116][B-command] |
| Batocera master | **Verified from source:** frontend runs the command with log redirections; Python launcher also pipes stdout/stderr. No PTY/terminal creation on this route. **Inferred:** TUI fallback cannot be relied upon as interactive, even if stdin happens to be inherited from a console. | [Platform.cpp:159][B-process], [command.py:128][B-command] |
| EmuELEC 4.8 | **Verified from source:** ES waits, then resumes. `_ENABLEEMUELEC` forces `hideWindow=false`; `Window::deinit(false)` retains InputManager and Renderer. Rockchip package patches remove this forced-false override; behavior then depends on `HideWindow` (default true on non-Windows/non-x86). The wrapper prepares/restores video, clears framebuffers, and does not stop ES. **Unknown:** successful pad/display transfer on each board. | [FileData.cpp:728][E-launch], [Window.cpp:187][E-window], [Settings.cpp:defaults][E-settings], [Rockchip patch][E-rockchip-patch], [HH patch][E-hh-patch], [emuelec-utils:470][E-video] |
| EmuELEC 4.8 | **Verified from source:** Ports call `emuelecRunApp.sh %ROM% -P… --core=… --emulator=… --controllers=…`. Wrapper sources `/etc/profile`, then executes `"$@"`. Profile exports `LD_LIBRARY_PATH=/emuelec/lib:/emuelec/lib32:${LD_LIBRARY_PATH}`, extends PATH, and sets the SDL DB file. Existing contents may be prepended again when sourced repeatedly. **Unknown:** complete inherited/customized environment. | [es_systems.json:2570][E-ports], [emuelecRunApp.sh:7][E-app], [99-emuelec.conf:4][E-profile], [profile:29][E-profile-loader] |
| EmuELEC 4.8 | **Verified from source:** normal frontend command stdout/stderr go to logs (or `/dev/null` with logging disabled); no PTY/terminal is created. **Inferred:** no usable TUI fallback from this launch route. | [Platform.cpp:147][E-process] |
| RetroBat 7.0-era source | **Verified from source:** frontend waits and resumes; `HideWindow=false` on Windows by default, so input/renderer teardown is conditional here too. Windows generator runs `.bat` using shell execution with `WindowStyle.Hidden`, waits, and temporarily shows an invisible form to avoid fullscreen DirectX allocation problems. **Inferred:** a normal visible TUI console is unavailable. | [FileData.cpp:701][R-launch], [Settings.cpp:323][R-settings], [Window.cpp:184][R-window], [ExeLauncher:299][R-exe], [ExeLauncher:562][R-exe-wait], [Generator:315][R-wait] |
| RetroBat 7.0-era source | **Inferred:** inherits Windows process environment; inspected Windows generator does not establish a Linux-style library path or a POSIX locale environment. `LD_LIBRARY_PATH` has no Windows loader meaning. **Unknown:** exact environment of the shipped 7.x binary, user launch method, Steam/overlay effects and pad handover. | [7.0 Windows system command:3499][R-system], [Windows generator][R-exe], [rolling component URLs][R-component-urls] |

**Tester check for remaining Unknowns:** launch the actual Mothdex Port from ES, record system/image/build/device, confirm ES PID survives, GUI gains focus, every pad works, hotkey exit and ordinary exit return to ES, and launch twice. Privately inspect the child environment using a whitelist; public reports should keep library-path/mapping values redacted as the contract requires. Record `isatty(0/1/2)` and whether a controlling terminal exists; a `TERM` value alone is not evidence of a terminal. Test CLI-only launch separately. EmuELEC passes arguments to the script, but the inspected sources do **not** establish that Mothdex needs to forward them.

## OQ-PRT-2 — SDL mapping variables

| System/version | Answer and confidence | Evidence |
|---|---|---|
| Batocera master | **Verified from source:** `Sh.needs_sdl_game_controller_config=True`; common execution adds `SDL_GAMECONTROLLERCONFIG` generated from ES controllers. It also writes `/tmp/gamecontrollerdb.txt`. **No `SDL_GAMECONTROLLERCONFIG_FILE` export was found in that generator/common execution**; writing a DB file does not itself export its name. An externally inherited file variable remains possible. | [sh.py:36][B-shell], [emulator.py:230][B-db], [emulator.py:857–884][B-mapping] |
| EmuELEC 4.8 | **Verified from source:** wrapper sources profile, which exports `SDL_GAMECONTROLLERCONFIG_FILE=/storage/.config/SDL-GameControllerDB/gamecontrollerdb.txt`. `--controllers=` is an argument, not evidence of an `SDL_GAMECONTROLLERCONFIG` environment variable. **Unknown:** whether a customized boot/Port additionally supplies the inline mapping variable. | [wrapper:7][E-app], [profile:8][E-profile], [Ports command][E-ports] |
| RetroBat 7.0-era source | **Unknown for shipped 7.x.** Neither variable is assigned in the inspected Windows generator; controller data is passed to emulatorLauncher as arguments. This is insufficient to promise either variable exists in Mothdex. | [Windows system][R-system], [generator][R-exe] |

**Tester:** report set/unset for both variables inside the actual child, count mappings loaded per source, and test a pad with a frontend custom mapping that differs visibly from the embedded DB. On EmuELEC confirm DB readability/existence; do not publish its contents. For RetroBat record exact frontend and emulatorLauncher versions.

## OQ-PRT-3 — video environment and compiled backends

Compiled-in support, compositor, and the driver SDL actually selects are separate observations.

| Image/configuration | Answer and confidence | Evidence |
|---|---|---|
| Batocera master x86_64 | **Verified from source:** board contains `BR2_PACKAGE_SDL_X11=y`, but that symbol alone concerns SDL1. The independent SDL2 selection is `SDL2_X11` when XORG7 is selected; x86_64 selects Batocera Xorg. KMSDRM selection is conditional on libGBM. **Unknown:** actual runtime `SDL_VIDEODRIVER` and selected driver on a device. | [x86_64.board:32][B-x86], [Config.in:519–529][B-select], [Config.in:2262][B-sdl] |
| Batocera master bcm2711 (RPi4) | **Verified from source:** selects labwc Wayland **and XWayland**. SDL2 selection enables Wayland for Batocera Wayland, X11 for XORG7, KMSDRM when libGBM is available. **Inferred:** Wayland/X11-capable session; do not force KMSDRM merely because the board uses DRM beneath its compositor. | [bcm2711.board:37][B-pi4], [Wayland/XWayland selection][B-wayland], [SDL2 selection][B-sdl] |
| Batocera master bcm2835 (RPi Zero/1) | **Verified from source:** labwc Wayland selected; **no XWayland selection in this board file**, unlike RPi4. Earlier lead “both boards enable XWayland” is **not confirmed and is contradicted by these board settings**. **Unknown:** full resolved backend inventory of a built image. | [bcm2835.board:37][B-pi1], [SDL2 selection][B-sdl] |
| EmuELEC 4.8 Amlogic-ng / -no / -old | **Verified from source:** SDL2 2.32.10 recipe sets `SDL_MALI=ON`, `SDL_KMSDRM=OFF`, `SDL_X11=OFF`, `SDL_WAYLAND=OFF`. The framebuffer patch registers driver name **`mali`**, not `fbcon` or `mali-fbdev`. Earlier Amlogic lead confirmed for these named cases. | [SDL2/package.mk:17–82][E-sdl], [MALI bootstrap:448][E-mali] |
| EmuELEC 4.8 OdroidGoAdvance / GameForce / RK356x / OdroidM1 | **Verified from source:** recipe enables KMSDRM; X11/Wayland remain off. Earlier Rockchip lead confirmed for these cases, not every possible future Rockchip target. **Unknown:** selected driver and `SDL_VIDEODRIVER` in a running child. | [SDL2/package.mk:38–81][E-sdl] |
| RetroBat / Mothdex Windows DLL | **Inferred:** Windows desktop display path; Linux KMSDRM/Mali/X11/Wayland choices are not deployment settings here. **Unknown:** exact driver inventory and selected driver of Mothdex's shipped SDL2.dll until queried. RetroBat's own SDL DLL is not Mothdex's DLL under the contract. | [contract §3][contract], [Windows launch path][R-exe] |
| Recalbox 11.0-patron-1 (no Mothdex Port) | **Verified from source:** rpi3/rpizero2 select KMSDRM; rpi4_64/rpi5_64 select XWayland plus KMSDRM; x86_64 selects Xorg. SDL2 recipe is **2.30.11**, enables Wayland when `BR2_PACKAGE_WAYLAND`, X11 when `SDL2_X11`, KMSDRM when `SDL2_KMSDRM`. **Unknown:** resolved inventory/driver on a particular built image. | [target selection:38–125][C-video-targets], [backend selection:250][C-video], [SDL2 recipe][C-sdl] |

**Tester for all runtime Unknowns:** enumerate `SDL_GetNumVideoDrivers` / `SDL_GetVideoDriver`, then initialize video and record `SDL_GetCurrentVideoDriver`; log `SDL_VIDEODRIVER` value or unset plus compositor/session type. Compare an actual ES launch with an SSH launch (which is not equivalent). No general `SDL_VIDEODRIVER` export was found in the inspected shell-Port generators; this does not prove it is globally unset. Other Batocera boards, 41/42/43, and unlisted EmuELEC targets remain **Unknown** in this bounded pass.

## OQ-PRT-4 — locale

| System/version | Answer and confidence | Evidence |
|---|---|---|
| Batocera master | **Verified from source:** `emulationstation-standalone` reads `system.language`, defaults empty value to `en_US`, exports `LC_ALL=<language>.UTF-8` and `LANG=$LC_ALL` before ES. Shell command inherits these unless user hooks change them. This directly answers the `system.language` question. | [standalone:86–92][B-locale], [child env merge][B-command] |
| EmuELEC 4.8 | **Verified from source:** `emustation-config` derives `<system.language>.UTF-8`, exports LANG and LANGUAGE, imports them to systemd. However the Port wrapper re-sources profile; stock `10-locale.conf` resets `LANG=C.UTF-8` if LOCPATH is empty. There is a literal `export LOCPATH=$locpat` (not `$locpath`) in the inspected script. **Inferred:** stock child can have `LANG=C.UTF-8` while LANGUAGE still carries the ES language. **Unknown:** actual child LANG/LANGUAGE/LC_ALL across boot/config overrides; do not assume the UI language reliably reaches LANG. | [emustation-config:101–122][E-locale], [service][E-service], [wrapper][E-app], [10-locale.conf][E-locale-profile] |
| RetroBat 7.0-era source | **Unknown:** no Port-specific POSIX locale export established in Windows generator. **Inferred:** Windows user locale must be handled through the application's Windows locale mechanism/fallback, not assumed from LANG/LC_ALL. | [Windows generator][R-exe] |

**Tester:** choose a non-English UI locale, restart ES, then record child LANG, LC_ALL, LANGUAGE, whether LOCPATH is set, and Mothdex's chosen language. On Windows also record Windows display/regional language separately from ES language. No EmuELEC locale bug is asserted as reproduced; the source inconsistency is a reason to test.

## OQ-PRT-5 — Batocera non-executable `.sh`

**Confirmed / Verified from source, Batocera master:** yes for the shell Port route: `/bin/bash <rom>` reads the script, so no execute bit on the **script** is required. This does not bypass noexec on the **Mothdex binary**, nor prove all exFAT mount configurations work. [sh.py:49–54][B-shell]

**Unknown:** B8 on a real exFAT share and Batocera 41/42/43. Tester should launch a readable mode-0644 script on a filesystem that supports modes, then the same Port from exFAT, record mount options and GUI exit/result. Test executable eligibility of `bin_dir` separately; interpreting a launcher is not an executable-filesystem probe.

## OQ-PRT-6 — ARM loader

| Target/version | Answer and confidence | Evidence |
|---|---|---|
| Batocera master bcm2835 ARMv6 and bcm2836 ARMv7 | **Verified from source:** both choose ARM EABIHF and common config chooses glibc. **Inferred:** expected interpreter is `/lib/ld-linux-armhf.so.3`; glibc's ARM hard-float loader name is `ld-linux-armhf.so.3`, and Buildroot copies dynamic-loader libraries into the target. **Unknown:** actual path/symlink existence in released images; source build intent is not an image manifest. | [ARMv6 board][B-pi1], [ARMv7 board][B-pi2], [common config:11][B-common], [glibc ARM ABI naming][G-arm], [pinned Buildroot glibc install][B-glibc] |
| Recalbox 11.0-patron-1 rpi3 / rpizero2, 32-bit ARM | **Verified from source:** ARM + Cortex-A53/FPU + glibc; pinned Buildroot defaults FPU-capable ARM to EABIHF and copies loader libraries. **Inferred:** expected armhf loader path. **Unknown:** actual image path/symlink. These boards are ARMv8 CPUs running 32-bit userland, not proof of an ARMv7 CPU baseline. | [rpi3 config][C-rpi3], [rpizero2 config][C-zero2], [Buildroot ABI default:516][C-arm-abi], [glibc installation][C-glibc], [ABI naming][G-arm] |
| Recalbox ARMv6 / legacy ARMv7 images | **Unknown for the requested image guarantee.** This 11.0 tag has no rpi1 defconfig among its current configs; legacy target definitions alone do not prove an available ARMv6 release image. Earlier 10.x image contents were not downloaded. | [11.0 configs tree][C-configs], [legacy target definitions][C-video-targets] |

**Tester:** on each exact image run `test -e /lib/ld-linux-armhf.so.3`, inspect `readlink -f` and `readelf -l /bin/bash` (or a native dynamic binary), and run the verified Mothdex `--probe`. Record image name and CPU as well as `uname -m`. Inspect a known native ELF if `readelf` is unavailable on device. A symlink name alone does not establish that its target exists or that the GUI can run. Keep B5/B7 unverified until then.

## OQ-PRT-7 — EmuELEC userland bitness

**Confirmed / Documented:** README explicitly says aarch64-only after 3.9 and directs 32-bit builders to `master_32bit`. **Confirmed / Verified from source:** `config/arch.aarch64` selects 64-bit/LP64; ES is a normal target package, whereas `lib32-SDL2` explicitly uses the separate lib32 toolchain and relocates libraries to `/usr/lib32`. **Inferred:** supported 4.8 ES and the shell launching an ordinary Mothdex Port are 64-bit; individual Port scripts may choose 32-bit binaries. The “32-bit libs side-by-side” lead is confirmed as compatibility support, and does not imply all Amlogic Ports run under armhf. [README:10–36][E-readme], [arch.aarch64][E-arch], [ES recipe][E-ES-pin], [lib32-SDL2][E-lib32]

**Unknown:** unofficial builds and every historical 4.x artifact were not inspected. Tester of any claimed exception should identify image provenance and run `getconf LONG_BIT`, inspect `/bin/bash` and `/usr/bin/emulationstation` ELF class/machine, then the actual GUI. `uname -m` alone reports the kernel and cannot establish userland bitness. No evidence here supports automatically choosing armv7 for supported EmuELEC 4.8.

## OQ-PRT-8 — Recalbox 11 launcher route

**Confirmed / Verified from source:** 11.0-patron-1 has game-specific port systems, e.g. Doom: `roms.folder=%ROOT%/ports/Doom`, `port=1`, with a fixed libretro core and `.wad`-family extensions. Inspection of the tag's 179 `system.ini` files found no generic `ports`/shell system for arbitrary `.sh` launchers. The release-notes history explicitly names a PORTS collection [C-notes][C-notes]. **Inferred:** its Ports collection should not be treated as Batocera's shell-Port system. [Doom system.ini][C-doom], [systems tree][C-systems]

**Unknown:** an officially supported arbitrary-launcher/custom-system method in 11.0; none was established in this pass. Do not turn the existence of a Ports collection into authorization to modify system configuration. **Recommendation, conditional on current scope:** retain `launcher: none`; no change proposed to Recalbox's existing installation-only support.

**Tester/maintainer evidence needed:** cite the official 11.0 customization interface (if one exists), supply its documented custom system/command format, and demonstrate launch, exit, frontend return, reboot and upgrade persistence with an ordinary shell app. A manually patched read-only system partition is not evidence of a supported installation route.

## OQ-PRT-9 — HOME and persistence

| System/version | Answer and confidence | Evidence |
|---|---|---|
| Batocera master | **Verified from source:** root passwd is rewritten to home `/userdata/system`; ES startup also explicitly exports it. `/userdata` normally mounts persistent storage, but falls back to a 256 MiB tmpfs if disk mount fails. **Inferred:** config/data/logs beneath HOME normally persist, with a real exception during storage failure/RAM mode. | [post-build:12–15][B-root], [ES startup:13][B-service], [S11share:52–84][B-share] |
| EmuELEC 4.8 | **Verified from source:** root home created as `/storage`; ES service explicitly sets `HOME=/storage`. Boot mounts the storage device there; live mode or no disk uses tmpfs. **Inferred:** normally persistent, not unconditional. | [busybox recipe:168][E-root], [service:6][E-service], [init:672–708][E-storage] |
| Recalbox 11.0-patron-1 | **Verified from source:** passwd and ES startup use `/recalbox/share/system`. Share mount routine supports persistent storage and falls back to tmpfs if the internal share mount fails. **Inferred:** normally persistent, with fallback exception. | [passwd:1][C-passwd], [ES startup:51–53][C-service], [S11share:125–145][C-share] |

**Unknown:** persistence across a particular device's reboot/upgrade, user mount customization, and complete XDG environment. Tester should record the effective child's HOME and XDG set/unset privately, inspect `findmnt -T "$HOME"` or `/proc/mounts`, and use a harmless owned sentinel under the chosen Mothdex data directory across reboot and a normal upgrade. Delete only that sentinel afterward. A path beginning `/userdata`, `/storage` or `/recalbox/share` is insufficient evidence by itself. The source does not justify a new launcher HOME override.

## OQ-PRT-10 — muOS and Knulli

**Unknown / deliberately deferred.** Establishing a versioned ROM-root/launcher/SDL/libc matrix was not cheap within this focused pass. Both remain out of scope as requested. A future task needs exact firmware versions/devices; testers would need to verify ROM root, launcher folder/format, shell invocation, ELF ABI/libc/loader, SDL2 soname/backend inventory, and frontend lifecycle. No guessed paths are proposed.

## Options and recommendation to Regente

| Option | Trade-off / condition |
|---|---|
| Keep fixed launchers and preserve inherited environment | Matches inspected frontend integration, including EmuELEC's required-looking library/mapping setup. Keep B1–B4 hardware gates; no source finding proves actual Mothdex usability. Recommended within the current contract. |
| Always retain automatic TUI fallback | Provides a binary fallback but the normal Linux launch output is redirected and RetroBat hides the batch window. **Not recommended as a promised frontend fallback** without a successful interactive device demonstration. Regente/Alicerce should decide the installer eligibility rule allowed by §2.2. |
| Force a single SDL driver or clear LD_LIBRARY_PATH | Could work around one device but conflicts with the different source-confirmed stacks. No evidence supports a cross-platform rule; any change needs a separate decision/test. |
| Add Recalbox Port support from the Ports collection alone | Collection membership is established; arbitrary script support is not. Retain `launcher: none` pending official interface evidence. |

**Remaining limits:** no image contents, device tests, installed RetroBat component binary provenance, or Batocera 41/42/43 tag-by-tag comparison; no claim of green CI or hardware validation. Highest-value beta checks are frontend launch/return and focus, terminal usability, selected SDL driver, EmuELEC locale, ARM interpreter, and persistence on the actual mounted share. Research completion does not close the contract's B-items.

## Primary-source links

[contract]: https://github.com/Or4cu1o/mothdex-cli/blob/649e38fe13565275d85370a6c92d57dc335b87d3/docs/contracts/cli/ports.md
[B-ES-pin]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/emulationstation/batocera-emulationstation/batocera-emulationstation.mk#L7
[B-shell]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/emulators/sh.py#L36
[B-mapping]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/emulator.py#L857
[B-db]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/emulator.py#L230
[B-command]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/command.py#L116
[B-launch]: https://github.com/batocera-linux/batocera-emulationstation/blob/fbd9423d09b6094601ea129338bf1e546b97035c/es-app/src/FileData.cpp#L701
[B-window]: https://github.com/batocera-linux/batocera-emulationstation/blob/fbd9423d09b6094601ea129338bf1e546b97035c/es-core/src/Window.cpp#L188
[B-settings]: https://github.com/batocera-linux/batocera-emulationstation/blob/fbd9423d09b6094601ea129338bf1e546b97035c/es-core/src/Settings.cpp#L354
[B-process]: https://github.com/batocera-linux/batocera-emulationstation/blob/fbd9423d09b6094601ea129338bf1e546b97035c/es-core/src/utils/Platform.cpp#L159
[B-x86]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/configs/batocera-x86_64.board#L32
[B-pi4]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/configs/batocera-bcm2711.board#L37
[B-pi1]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/configs/batocera-bcm2835.board
[B-pi2]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/configs/batocera-bcm2836.board#L4
[B-select]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/core/batocera-system/Config.in#L519
[B-sdl]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/core/batocera-system/Config.in#L2262
[B-wayland]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/core/batocera-system/Config.in#L2304
[B-locale]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/emulationstation/batocera-emulationstation/emulationstation-standalone#L86
[B-root]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/board/batocera/scripts/post-build-script.sh#L12
[B-service]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/emulationstation/batocera-emulationstation/S31emulationstation#L13
[B-share]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/board/batocera/fsoverlay/etc/init.d/S11share#L52
[B-common]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/configs/batocera-board.common#L11
[B-glibc]: https://github.com/batocera-linux/buildroot/blob/50c810b99e562c14cf08567de734d37098a7f1e2/package/glibc/glibc.mk
[E-ES-pin]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec-emulationstation/package.mk#L5
[E-app]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec/bin/emuelecRunApp.sh#L7
[E-ports]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec-emulationstation/config/es_systems.json#L2570
[E-profile]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec/profile.d/99-emuelec.conf#L4
[E-profile-loader]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sysutils/busybox/config/profile#L29
[E-launch]: https://github.com/EmuELEC/emuelec-emulationstation/blob/09356d0dae6291008a074d8ac4f8c20678d822d2/es-app/src/FileData.cpp#L728
[E-window]: https://github.com/EmuELEC/emuelec-emulationstation/blob/09356d0dae6291008a074d8ac4f8c20678d822d2/es-core/src/Window.cpp#L187
[E-settings]: https://github.com/EmuELEC/emuelec-emulationstation/blob/09356d0dae6291008a074d8ac4f8c20678d822d2/es-core/src/Settings.cpp
[E-process]: https://github.com/EmuELEC/emuelec-emulationstation/blob/09356d0dae6291008a074d8ac4f8c20678d822d2/es-core/src/utils/Platform.cpp#L147
[E-rockchip-patch]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec-emulationstation/patches/Rockchip/emuelec-emulationstation-window-fix-rockchip.patch
[E-hh-patch]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec-emulationstation/patches/Rockchip/HH/emuelec-emulationstation-oga-fixes.patch
[E-video]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec/bin/emuelec-utils#L470
[E-sdl]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/multimedia/SDL2/package.mk#L17
[E-mali]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/multimedia/SDL2/patches/Amlogic/0001-mali-fbdev-add-driver.patch#L448
[E-locale]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec/bin/emustation-config#L101
[E-locale-profile]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sysutils/busybox/profile.d/10-locale.conf
[E-service]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec-emulationstation/system.d/emustation.service#L6
[E-readme]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/README.md#L10
[E-arch]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/config/arch.aarch64
[E-lib32]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/lib32/multimedia/lib32-SDL2/package.mk
[E-root]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sysutils/busybox/package.mk#L168
[E-storage]: https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sysutils/busybox/scripts/init#L672
[R-component-urls]: https://github.com/RetroBat-Official/retrobat-setup/blob/fc851fdcbc1ab794f417845f065424a3dfca3405/system/scripts/shared-variables.cmd#L88
[R-system]: https://github.com/RetroBat-Official/retrobat-setup/blob/fc851fdcbc1ab794f417845f065424a3dfca3405/system/templates/emulationstation/es_systems.cfg#L3499
[R-launch]: https://github.com/RetroBat-Official/emulationstation/blob/86561b0fb0bd0fdb2c873d4f9897c1bed655784f/es-app/src/FileData.cpp#L701
[R-settings]: https://github.com/RetroBat-Official/emulationstation/blob/86561b0fb0bd0fdb2c873d4f9897c1bed655784f/es-core/src/Settings.cpp#L323
[R-window]: https://github.com/RetroBat-Official/emulationstation/blob/86561b0fb0bd0fdb2c873d4f9897c1bed655784f/es-core/src/Window.cpp#L184
[R-exe]: https://github.com/RetroBat-Official/emulatorlauncher/blob/b30c0a921123dd175fd879d4c91125cdcc3e5a8d/emulatorLauncher/Generators/ExeLauncher.Generator.cs#L299
[R-exe-wait]: https://github.com/RetroBat-Official/emulatorlauncher/blob/b30c0a921123dd175fd879d4c91125cdcc3e5a8d/emulatorLauncher/Generators/ExeLauncher.Generator.cs#L562
[R-wait]: https://github.com/RetroBat-Official/emulatorlauncher/blob/b30c0a921123dd175fd879d4c91125cdcc3e5a8d/emulatorLauncher/Common/Generator.cs#L315
[C-passwd]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/custom/system/skeleton/etc/passwd#L1
[C-service]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/board/recalbox/fsoverlay/etc/init.d/S31emulationstation#L51
[C-share]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/board/recalbox/fsoverlay/etc/init.d/S11share#L125
[C-doom]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/package/recalbox-romfs2/systems/doom/system.ini
[C-systems]: https://gitlab.com/recalbox/recalbox/-/tree/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/package/recalbox-romfs2/systems
[C-configs]: https://gitlab.com/recalbox/recalbox/-/tree/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/configs
[C-video-targets]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/package/recalbox-system/Config.in#L38
[C-video]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/package/recalbox-system/Config.in#L250
[C-sdl]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/custom/package/sdl2/sdl2.mk
[C-rpi3]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/configs/recalbox-rpi3_defconfig#L1
[C-zero2]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/configs/recalbox-rpizero2_defconfig#L1
[C-arm-abi]: https://github.com/buildroot/buildroot/blob/e4a5ab3b319753f41e2b5cf22e90b6b0304ca225/arch/Config.in.arm#L516
[C-glibc]: https://github.com/buildroot/buildroot/blob/e4a5ab3b319753f41e2b5cf22e90b6b0304ca225/package/glibc/glibc.mk#L191
[G-arm]: https://github.com/bminor/glibc/blob/glibc-2.39/sysdeps/unix/sysv/linux/arm/shlib-versions

[B-changelog]: https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/batocera-Changelog.md#L1
[C-notes]: https://gitlab.com/recalbox/recalbox/-/blob/1392898ad3d3dd82f9631e488d7ab19e7ea9d313/RELEASE-NOTES.md#L1376
