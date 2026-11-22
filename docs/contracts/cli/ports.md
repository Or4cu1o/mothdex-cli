# Contract: frontend Ports for `mothdex-gui` (E-M3)

Status: draft v1 (T-68) · Owner: Alicerce · Implementer: Guindaste (installers, packaging) · Small GUI items: Vitral ·
Threat model of the installer part: Sentinela, before coding · Launcher UX: Bússola · Risk: **High** (runs on user
devices, writes executables, supply chain).

Sources:
- research: [T1e](../../research/T1e-emulation-systems.md) (frontends, Ports), [T15b](../../research/T15b-platform-folders.md)
  (folders), [T1b](../../research/T1b-sdl2-purego.md) (SDL2);
- spike: [T-52 report](../../qa/T-52-gui-spike-report.md);
- contracts: [`gui.md`](gui.md) §2, §4 and §6, [`profiles.md`](profiles.md) §2-§4, [`release-artifacts.md`](release-artifacts.md),
  [`config.md`](config.md) §3;
- PLAN-v0 §2.6 and §2.7; ROADMAP E-M3; owner decisions D22 (launcher opt-in), D28 9a (community beta).

A **Port** is the launcher file that a frontend lists and runs like a game, so a gamepad user can open `mothdex-gui`
without a terminal. This document fixes what the installers write, where, with which content, and what stays to be
confirmed on hardware. The installer constraints IC-1 to IC-13 of the threat model
[T-70](../../security/T-70-threat-model-ports-installer.md) §5 bind the implementation. It details the installer item of gui.md §6 and the launcher item of profiles.md §4.

Every fact below comes from the sources above. A fact the research did not confirm is labelled ⚠ and listed in §9 as
an open question (OQ-PRT-n). Do not turn an OQ into code behavior without new research.

## 1. Scope

| System | Port in v0.3.0 | Why |
|---|---|---|
| Batocera | yes, `.sh` | `ports` system takes `.sh` (T1e #4) |
| EmuELEC | yes, `.sh` | `ports` system path is `ports_scripts`, `.sh` (T1e #20) |
| RetroBat | yes, `.bat` | `windows` system takes `.bat`; `ports` takes only `.libretro` (T1e #30) |
| Recalbox | **no Port** | 10.1.2 has no `ports` system (T1e #15); the GUI can still be installed (§5.4) |
| Lakka, EmuDeck, RetroDECK, generic | no Port | `launcher: none` in their profiles (profiles.md §3) |
| muOS, Knulli, ROCKNIX | **out of scope** | no research for their ROM roots, Port folders or SDL (T1e OQ5, T1b #23; Knulli not researched at all). PortMaster is post-1.0 (PLAN §2.7) |

## 2. The launcher file

### 2.1 Location and name

The installer takes the folder from the active profile's `launcher.dir` (profiles.md §2). The file name is fixed.

| System | Folder | File | Run by |
|---|---|---|---|
| Batocera | `/userdata/roms/ports` ✅ | `Mothdex.sh` | the `ports` system (T1e #4) |
| EmuELEC | `/storage/roms/ports_scripts` ✅ (not `roms/ports`, which holds PortMaster data) | `Mothdex.sh` | `emuelecRunApp.sh` (T1e #20) |
| RetroBat | `<RetroBat>\roms\windows` ✅ | `Mothdex.bat` | the `windows` system (T1e #30) |

The profile loader already refuses a ROM folder equal to a launcher folder (profiles.md §2, T-16 CR-2 and CR-3), so a
download never lands where the frontend runs scripts. This contract does not change that rule.

### 2.2 Content (marker `v2`)

The launcher is a **fixed template**. The only variable part is the absolute install directory, which must pass the
existing path checks (`valid_path` in `install.sh`, `Test-LauncherPath` in `install.ps1`, TH-30). The template chooses
at run time, so a later GUI uninstall or a failed GUI install still leaves a working launcher.

POSIX (`Mothdex.sh`, mode `0755`):

```sh
#!/bin/sh
# mothdex-launcher v2
if [ -x "<bin_dir>/mothdex-gui" ]; then exec "<bin_dir>/mothdex-gui"; fi
exec "<bin_dir>/mothdex" tui
```

Windows (`Mothdex.bat`, CRLF, UTF-8 without BOM):

```bat
@echo off
rem mothdex-launcher v2
setlocal DisableDelayedExpansion
if not exist "<bin_dir>\mothdex-gui.exe" goto tui
"<bin_dir>\mothdex-gui.exe"
exit /b %errorlevel%
:tui
"<bin_dir>\mothdex.exe" tui
```

**No argument forwarding** (T-70 IC-6): `cmd` re-parses `%*`, so an `&` in an argument would run a command, and a
Port needs no arguments. If OQ-PRT-1 shows that a frontend passes arguments the GUI needs, they come back in the `.sh`
template only, with the GUI and TUI rejecting unknown positional arguments.

`<bin_dir>` is the directory where the binaries actually landed: after the `install.sh` noexec fallback to
`$HOME/.local/bin`, it is the fallback directory.

Whether a Port has a usable terminal for the `mothdex tui` fallback is ⚠ OQ-PRT-1. If the beta shows it has none, the
installer stops writing a launcher when the GUI is not eligible (§5.2); the template does not change.

### 2.3 Environment: the launcher sets nothing

- The launcher **never sets `LD_LIBRARY_PATH`**, `LD_PRELOAD`, `PATH`, any `SDL_*` variable or any `MOTHDEX_*`
  variable. It does not `cd`. The GUI inherits the frontend's environment unchanged.
- **Why:** SDL2 is found by soname through the system loader (§3). A library path set by the launcher would widen the
  search to directories that may be writable (the share partition) and defeat the planting tests of the spike (T-52).
- Whatever the frontend itself passes (`LD_LIBRARY_PATH`, `SDL_VIDEODRIVER`, `SDL_GAMECONTROLLERCONFIG`, locale) is
  not known for any system (⚠ OQ-PRT-1, OQ-PRT-2, OQ-PRT-4). The GUI does not unset inherited values either: a frontend
  may need them for its own SDL build, and removing them is a choice to make only after the beta reports them.
- A fix that needs a per-system setting goes into the profile data and the GUI, never into the launcher template.

## 3. SDL2 per system

The rules are those of gui.md §2 and stay unchanged:
- Linux: `dlopen("libSDL2-2.0.so.0")` by soname only; SDL2 is **never bundled** on Linux.
- Windows: the pinned `SDL2.dll` 2.32.10 (`scripts/sdl2-windows.pin`) next to `mothdex-gui.exe`, loaded with
  `LOAD_LIBRARY_SEARCH_APPLICATION_DIR|LOAD_LIBRARY_SEARCH_SYSTEM32`.
- The binding needs SDL 2.0.0 at least and targets 2.0.18 or later (T-52).

| System | SDL2 source | Known | Arch of the GUI archive | Interpreter |
|---|---|---|---|---|
| Batocera | system `libSDL2-2.0.so.0` | SDL2 2.32.10 on master and 44 ✅ (T1b #17) | amd64, arm64, armv7, armv6 from `uname -m` | glibc ✅; armhf `/lib/ld-linux-armhf.so.3` ⚠ OQ-PRT-6 |
| EmuELEC | system `libSDL2-2.0.so.0` | SDL2 2.32.10, Amlogic `mali-fbdev` patch ✅ (T1b #19) | arm64 | glibc Inferred (T1b #24); 32-bit userland next to 64-bit ⚠ OQ-PRT-7 |
| Recalbox | system `libSDL2-2.0.so.0` | SDL2 present, version Unverified (T1b #18) | amd64, arm64, armv7 (rpi3 and rpizero2 are 32-bit, T1e #14) | glibc ✅; armhf ⚠ OQ-PRT-6 |
| RetroBat | the pinned `SDL2.dll` from the archive | RetroBat's own SDL is unknown (T1b #22) and not used | windows amd64 | n/a |

Batocera `x86_wow64` (i686 userland) gets no GUI in v0.3.0: linux 386 is optional in gui.md §6, and the eligibility
probe (§5.2) fails there, so the launcher runs `mothdex tui`.

## 4. Gamepad mapping, display and locale

The mapping source is gui.md §4, the same on every system. Later sources override earlier ones:
1. the embedded `gamecontrollerdb.txt` (pin `4e87e3c6c257`);
2. `<DataDir>/gamecontrollerdb.txt`;
3. `SDL_GAMECONTROLLERCONFIG_FILE`, then `SDL_GAMECONTROLLERCONFIG`, as passed by the frontend.

| System | Frontend mapping passed to Ports | Video driver | Locale for Ports |
|---|---|---|---|
| Batocera | ⚠ OQ-PRT-2 | ⚠ OQ-PRT-3 (KMSDRM, Wayland or X11 per board) | ⚠ OQ-PRT-4 (`system.language`) |
| EmuELEC | ⚠ OQ-PRT-2 | ⚠ OQ-PRT-3 (`mali-fbdev` patch, T1b #19) | ⚠ OQ-PRT-4 |
| Recalbox | n/a (no Port) | ⚠ OQ-PRT-3 | n/a |
| RetroBat | ⚠ OQ-PRT-2 | Windows default (desktop session) | Windows user locale; gui.md §3 fallbacks apply |

Whether the frontend stops EmulationStation and releases the pad and the display while a Port runs is Unverified
(T1b OQ1): ⚠ OQ-PRT-1.

**Diagnostics for the beta (Vitral, small).** At start, `mothdex-gui` writes one log line with:
- the loaded SDL version and the video driver SDL chose;
- whether `LD_LIBRARY_PATH`, `SDL_VIDEODRIVER`, `SDL_GAMECONTROLLERCONFIG` and `SDL_GAMECONTROLLERCONFIG_FILE` are
  set, as names with `set` or `unset`, and the value of `SDL_VIDEODRIVER` only;
- `LANG` and `LC_ALL`;
- the number of mapping lines added from each source and the type of each connected controller;
- whether `LD_PRELOAD` and `SDL_DYNAMIC_API` are set, as names only (T-70 IC-13).

No value of a mapping, path list or other variable is logged, and no path at all. This line is what the beta report template (§8) asks
testers to paste.

## 5. Install, upgrade and uninstall

Everything goes through the existing installers (`install.sh`, `install.ps1`). There is no new installer and no new
download channel. All checks of release-artifacts.md and of the installers stay: checksum always, before anything is
executable; cosign when available; TH-30 paths; TH-31 directory ownership; the exec-bit probe and its fallback.

### 5.1 Flags and the D22 flip

| Flag (`install.sh` / `install.ps1`) | Before the GUI release | From the release that first ships `mothdex-gui_` archives (v0.3.0) |
|---|---|---|
| `--launcher` / `-Launcher` | opt-in, off by default (D22 as today) | **on by default** |
| `--no-launcher` / `-NoLauncher` | n/a | turns the launcher off |
| `--gui` / `-Gui` | n/a (no archive; the flag is accepted and does nothing) | **on by default** |
| `--no-gui` / `-NoGui` | n/a | installs the CLI only |

- **When the default flips:** in the same installer change that adds GUI install support, merged with the release PR
  of the first version that publishes `mothdex-gui_` archives. The installer decides by the release it installs, not by
  its own version, and only from that release's verified `checksums.txt` (T-70 IC-3): a `mothdex-gui_` archive for the
  arch that is not listed means the GUI part is skipped with a note and the launcher stays opt-in (D22 rule) for that
  release; one that is listed but cannot be downloaded or verified aborts the whole install.
- The flip is an owner decision already taken (D22: on by default once E-M3 ships, gui.md §6). T-70 §6 keeps it, on
  condition of IC-7 (`install.ps1` writes `Mothdex.bat` only into a trusted `roms\windows`, as `install.sh` already does)
  and IC-12 (every run that writes the launcher prints its path and how to opt out).
- `--dry-run` prints the GUI archive, the eligibility result (§5.2) and the launcher path, and changes nothing.

### 5.2 GUI eligibility (Linux)

The GUI is dynamic on Linux, so the installer must prove it can start before it writes the launcher that prefers it.

1. Download `mothdex-gui_<version>_linux_<arch>` from the **same release** as `mothdex_`, with the same arch mapping
   (`uname -m`, release-artifacts.md §3). Versions are never mixed.
2. Verify it exactly like the CLI archive: SHA-256 against the same `checksums.txt`, cosign when available. **A
   verification failure aborts the whole install** before anything is written, as for the CLI; it never degrades to
   "CLI only".
3. Extract by exact member name only (T-70 IC-4), copy the binary to its staging name in `bin_dir`, and run
   `mothdex-gui --probe` there with a scrubbed environment and a time bound (IC-1, IC-2, IC-10), never from the temp
   dir. Exit 0 means eligible: rename it into place. Exit 1 means not eligible: delete the staging file. An exec failure
   (126, 127, `ENOEXEC`) is an installer error that is reported, never a silent "not eligible".
4. Not eligible (missing interpreter, missing or too old SDL2, wrong userland arch): warn with the probe's one-line
   reason, do not install the GUI, and keep the CLI install. The launcher, when on, then runs `mothdex tui`.

**`mothdex-gui --probe`** (gap PRT-G3, Vitral):
- loads SDL2 exactly as at normal start (§3), calls `SDL_GetVersion` and applies the version rule;
- opens no window, initializes no video or input subsystem, reads no config and writes no file or log;
- prints `sdl <major>.<minor>.<patch>` and exits 0, or prints the reason and exits 1, the code gui.md §5 uses for SDL
  problems.

This replaces the `ldconfig -p` check of gui.md §6. One run proves the interpreter, the userland arch and the SDL
soname together, and does not depend on `ldconfig` being present on these images (Unverified).

Windows: the archive carries `SDL2.dll`. `install.ps1` runs `mothdex-gui.exe --probe` the same way. A failure there is
a packaging bug, so it is reported as an error. As on Linux, neither `mothdex-gui.exe` nor `SDL2.dll` is installed
(T-70 IC-9); the CLI install still completes and the launcher runs `tui`.

### 5.3 Files written

| File | Where | Mode / ACL |
|---|---|---|
| `mothdex` (`mothdex.exe`) | `bin_dir` | as today |
| `mothdex-gui` (`mothdex-gui.exe`) | `bin_dir`, next to `mothdex` | Linux `0755`; Windows the `bin_dir` ACL (TH-31) |
| `SDL2.dll` (Windows only) | `bin_dir`, next to `mothdex-gui.exe` | the `bin_dir` ACL. Never in a folder other users can write (PR-69-backlog) |
| `mothdex-licenses/` (§6) | `bin_dir` | read-only files |
| `Mothdex.sh` / `Mothdex.bat` | `launcher.dir` (§2.1) | `0755` on Linux |

- Each binary is written to a temp name in `bin_dir`, then renamed, as `install_binary` does today. The launcher is
  written last, so a crash mid-install never leaves a launcher pointing at a missing GUI.
- RetroBat: `bin_dir` is the installer's per-user `%LocalAppData%\Programs\mothdex` (install.ps1, TH-31). The
  `retrobat` profile YAML still says `${MATCH}\mothdex` for `bin_dir`. That is gap PRT-G1 (§10): the installer is
  authoritative.

### 5.4 Per system

| System | Binaries | GUI | Launcher by default (from v0.3.0) |
|---|---|---|---|
| Batocera | `/userdata/system/bin` ⚠ (convention) | if eligible | `Mothdex.sh` in `/userdata/roms/ports` |
| EmuELEC | `/storage/.local/bin` ⚠ | if eligible | `Mothdex.sh` in `/storage/roms/ports_scripts` |
| Recalbox | `/recalbox/share/system/bin` ⚠ | if eligible (runs from a desktop or terminal session only) | none (`launcher: none`); ⚠ OQ-PRT-8 |
| RetroBat | `%LocalAppData%\Programs\mothdex` | always (Windows archive) | `Mothdex.bat` in `<RetroBat>\roms\windows` |

On Batocera and Recalbox shares without permission bits, the TH-31 root-only FAT exception of profiles.md §4 applies
to the launcher folder unchanged. Whether a `.sh` Port on exFAT runs without the exec bit is ⚠ OQ-PRT-5.

### 5.5 Upgrade

Re-running the installer is the upgrade path:
- It replaces `mothdex`, `mothdex-gui` and `SDL2.dll` atomically (§5.3) with the files of the release it installs.
- It rewrites the launcher **only** if the existing file carries a marker it knows (`v1` or `v2`). A `v1` launcher
  (always `mothdex tui`) becomes `v2`.
- A file without a marker is kept, with the existing warning.
- An upgrade with `--no-gui` removes an installed `mothdex-gui` (and `SDL2.dll` on Windows), so the `v2` launcher
  falls back to `tui`.
- After writing a new launcher, the installer runs the profile's refresh once (profiles.md §4: `http_get` only where
  verified, never fatal), so the Port shows in the list. On Recalbox and other `manual` profiles it prints the hint.

### 5.6 Uninstall

`--uninstall` / `-Uninstall` removes:
- `mothdex`, `mothdex-gui`, `SDL2.dll` (Windows) and `mothdex-licenses/` from `bin_dir`;
- the launcher, only when it carries a `v1` or `v2` marker.

It keeps the config, data, history, cache and every downloaded ROM. It never removes a file without a marker, and it
lists what it removed. `--dry-run` lists without removing.

## 6. Third-party notices

The repo keeps the notices in `THIRD_PARTY/` (`README.md` plus licence texts), and the GUI archives carry that
folder (gui.md §6). For Ports:

| Component | Licence | Where the notice lives |
|---|---|---|
| Noto Sans (embedded) | OFL-1.1 | `THIRD_PARTY/NotoSans-OFL-1.1.txt` in the archive, installed to `bin_dir/mothdex-licenses/` |
| GameControllerDB (embedded) | zlib | `THIRD_PARTY/SDL_GameControllerDB-zlib.txt`, same |
| SDL2.dll 2.32.10 (Windows, shipped) | zlib | **missing**: add `THIRD_PARTY/SDL2-zlib.txt` from the SDL2 release (gap PRT-G2) |
| purego (in the binary) | Apache-2.0 | **missing**: Apache-2.0 §4(a) asks for a copy of the licence in binary distributions (gap PRT-G2) |
| golang.org/x/image (in the binary) | BSD-3-Clause | **missing**: BSD-3 asks to reproduce the notice in documentation of binary distributions (gap PRT-G2) |

- The installer copies the archive's `THIRD_PARTY/` as `bin_dir/mothdex-licenses/`, so an installed Port carries the
  notices next to the binaries. Uninstall removes it (§5.6).
- gui.md §6 names the archive file `THIRD_PARTY.md`; the repo layout `THIRD_PARTY/` wins (gap PRT-G5).
- Linux SDL2 is the system's and is not shipped, so it needs no notice from us.

## 7. ROM destination

Unchanged. A Port only starts the GUI, and the GUI enqueues with `DestDir ""` (gui.md §5), so the destination is the
profile's `RomRoot/<folder>` from profiles.md §4. Launcher folders can never be ROM folders (§2.1).

| System | ROM root | Folder names |
|---|---|---|
| Batocera | `/userdata/roms` ✅ | `folders_verified: true` (T15b) |
| EmuELEC | `/storage/roms` ✅ | `folders_verified: true`; folder = basename of `path` (T15b) |
| Recalbox | `/recalbox/share/roms` ✅ | `folders_verified: false` (T15b: 51 of 146 read) |
| RetroBat | `<RetroBat>\roms` ✅ | `folders_verified: true` |

Whether `HOME` and the XDG directories that hold config, data and logs (config.md §3) are on persistent storage when a
Port runs is ⚠ OQ-PRT-9. If they are not, the fix is a profile data path, never a launcher variable (§2.3).

## 8. What stays unverified (community beta, D28 9a)

There is no hardware (ROADMAP risks), so these items ship labelled Unverified in the release notes until a beta report
confirms each one on a named system and device:

| # | Item | Systems |
|---|---|---|
| B1 | The Port appears in the list and starts the GUI; the frontend comes back on exit (gui.md exit codes) | Batocera, EmuELEC, RetroBat |
| B2 | The GUI takes the display and the pad while EmulationStation waits; the `tui` fallback has a terminal (OQ-PRT-1) | Batocera, EmuELEC, RetroBat |
| B3 | The video driver SDL picks, and legibility at 640x480 and 480x320 (gui.md §3) | Batocera, EmuELEC |
| B4 | Pads map correctly from the embedded DB, and frontend mapping variables reach the GUI (OQ-PRT-2) | all |
| B5 | The armhf interpreter path, and real ARMv6 (T-52 Unverified) | Batocera, Recalbox |
| B6 | `bin_dir` persists across reboots and upgrades, and `HOME` is persistent (OQ-PRT-9) | Batocera, EmuELEC, Recalbox |
| B7 | `--probe` result on each image, including EmuELEC 32-bit userland (OQ-PRT-7) | Batocera, EmuELEC, Recalbox |
| B8 | A `.sh` Port on an exFAT share (OQ-PRT-5) | Batocera |
| B9 | The refresh after install shows the Port | Batocera, EmuELEC, RetroBat |
| B10 | Upgrade from a `v1` launcher, and uninstall leaves ROMs and config intact | all |

The beta report template (one issue per system and device, D4) asks for: system and version, device and CPU arch,
the installer output, the GUI diagnostics line (§4), and each B-item as pass, fail or not tried. Guindaste writes the
template; the compatibility matrix lists the B-items per system.

## 9. Open questions (research for Sonda, via Regente)

| Id | Question | Blocks |
|---|---|---|
| OQ-PRT-1 | Batocera, EmuELEC, RetroBat: does the frontend stop ES and release the pad and display for a Port? Which environment does the child get (`LD_LIBRARY_PATH` especially)? Does it have a terminal? (T1b OQ1) | B1, B2 |
| OQ-PRT-2 | Do these frontends set `SDL_GAMECONTROLLERCONFIG` or `SDL_GAMECONTROLLERCONFIG_FILE` for Ports? (gui.md §4) | B4 |
| OQ-PRT-3 | Which `SDL_VIDEODRIVER` and SDL video backends are in each image (KMSDRM, fbdev Mali, Wayland, X11)? | B3 |
| OQ-PRT-4 | Which locale variables does a Port get (Batocera `system.language`)? (gui.md §8) | locale |
| OQ-PRT-5 | Does Batocera run a `.sh` Port without the exec bit (exFAT share), for example through `sh file`? (T1e #6) | B8 |
| OQ-PRT-6 | Does `/lib/ld-linux-armhf.so.3` exist on Batocera and Recalbox armv6 and armv7 images? (gui.md §2) | B5 |
| OQ-PRT-7 | EmuELEC: is the userland that runs ES and Ports aarch64, or 32-bit armhf on some Amlogic builds? (T1e: "32-bit libs side-by-side") | B7 |
| OQ-PRT-8 | Recalbox 11.0: is there a `ports` system or another supported way to add a launcher? (T1e OQ2) | Recalbox Port |
| OQ-PRT-9 | Batocera, EmuELEC, Recalbox: the value of `HOME` for root, and whether it is on persistent storage | B6 |
| OQ-PRT-10 | muOS and Knulli: ROM roots, Port folder and format, SDL2 and libc | scope (§1) |

## 10. Gaps and owners

| Id | Owner | Item |
|---|---|---|
| PRT-G1 | Guindaste | `profiles/retrobat.yaml` `bin_dir` says `${MATCH}\mothdex`, while `install.ps1` and profiles.md §3 use `%LocalAppData%\Programs\mothdex`. Align the YAML with the installer |
| PRT-G2 | Guindaste | add `SDL2-zlib.txt`, the Apache-2.0 text for purego and the BSD-3 text for `x/image` to `THIRD_PARTY/` and to its README table. CLI archives with BSD-licensed Go modules: backlog check |
| PRT-G3 | Vitral | `mothdex-gui --probe` (§5.2) and the start-up diagnostics line (§4) |
| PRT-G4 | Guindaste | installers: GUI archive, eligibility, `v2` launcher, `--no-gui`/`--no-launcher` and `-NoGui`/`-NoLauncher`, the default flip, `mothdex-licenses/`, uninstall and upgrade rules; installer tests for each case (`tests/installer/`); the beta report template (§8) |
| PRT-G5 | Alicerce, with Guindaste's PR | gui.md §6: replace the `ldconfig -p` check with `--probe`, and `THIRD_PARTY.md` with `THIRD_PARTY/`; release-artifacts.md: the static-binary sentence (already listed in gui.md §6) |
| PRT-G6 | Bússola | **closed (v1: no metadata).** The name `Mothdex` comes from the file name. No `gamelist.xml` entry: the file belongs to the frontend and the person, and EmulationStation rewrites it on exit and on scrape. No image in v1: art is deferred to the brand session (D28 8b). If that session approves art, this contract adds how each frontend shows it without writing `gamelist.xml` (for example a media file next to the script), Unverified per frontend and checked under B1 |
| PRT-G7 | Sentinela | **closed by T-70** ([threat model](../../security/T-70-threat-model-ports-installer.md)): constraints IC-1 to IC-13 bind PRT-G3 and PRT-G4, IC-8 (`SetDefaultDllDirectories` in `mothdex-gui.exe`) goes to Vitral; residuals R1-R5 recorded there |

## 11. Test plan (risk paths)

- **Installer cases** (`tests/installer/`, containers with a fake file tree, no hardware):
  - eligibility pass and fail, including a missing interpreter and a missing soname;
  - a checksum mismatch on the GUI archive aborts the whole install;
  - `v1` to `v2` upgrade;
  - a file without a marker is kept;
  - `--no-gui` removes the GUI and the launcher falls back;
  - uninstall removes only marked files and `mothdex-licenses/`;
  - the launcher content equals the template for a given `bin_dir`;
  - a path with unsafe characters skips the launcher;
  - the noexec fallback writes the launcher with the fallback dir;
  - T-70: a probe run with `LD_PRELOAD` set in the installer environment does not see it (IC-1);
  - `/tmp` noexec still installs the GUI, because the probe runs in `bin_dir` (IC-2);
  - a probe exec failure is reported, not taken as "not eligible" (IC-2);
  - a GUI archive not listed in `checksums.txt` is skipped, a listed one that fails to download aborts (IC-3);
  - an archive with an extra member, a symlink member or `../` aborts (IC-4);
  - a `mothdex-licenses` symlink is refused (IC-5);
  - `%*` and `"$@"` are absent from both templates (IC-6);
  - the refresh ignores `http_proxy` (IC-11).
- **`install.ps1`** (Pester): the same cases; `SDL2.dll` lands only in the ACL-checked `bin_dir`; the launcher is
  skipped in a `roms\windows` where Users can write (IC-7); a probe failure installs neither `mothdex-gui.exe` nor
  `SDL2.dll` (IC-9).
- **GUI on Windows** (Vitral, spike job): a garbage `vulkan-1.dll` on `PATH` is ignored (IC-8).
- **GUI** (Vitral, tag `gui`, existing spike jobs): `--probe` exits 0 with the distro SDL, and 1 with no SDL on the
  library path; the diagnostics line names variables without values.
- No test reads this document's text, and none needs a real frontend.
