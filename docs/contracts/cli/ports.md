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
- PLAN-v0 §2.6 and §2.7; ROADMAP E-M3; owner decisions D22 (launcher opt-in), D28 9a (community beta), D32 (launcher only with the GUI, T-88).

A **Port** is the launcher file that a frontend lists and runs like a game, so a gamepad user can open `mothdex-gui`
without a terminal. This document fixes what the installers write, where, with which content, and what stays to be
confirmed on hardware. The installer constraints IC-1 to IC-13 of the threat model
[T-70](../../security/T-70-threat-model-ports-installer.md) §5 bind the implementation. It details the installer item of gui.md §6 and the launcher item of profiles.md §4.

Every fact below comes from the sources above. A fact the research did not confirm is labelled ⚠ and listed in §9 as
an open question (OQ-PRT-n). Do not turn an OQ into code behavior without new research. OQ-PRT-1 to OQ-PRT-9 are
answered from source in [T-84](../../research/T-84-ports-open-questions.md) (Sonda, 2026-11-22, pinned commits, no hardware); §9 holds the answers and their
confidence, and what no source can settle stays a beta item (§8).

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

### 2.2 Content (marker `v3`, D32)

The launcher is a **fixed template**. The only variable part is the absolute install directory, which must pass the
existing path checks (`valid_path` in `install.sh`, `Test-LauncherPath` in `install.ps1`, TH-30). It runs `mothdex-gui`
and nothing else, and it is written **only when the GUI is installed** (D32, §5). There is no `mothdex tui` fallback,
because a Port has no terminal (OQ-PRT-1).

POSIX (`Mothdex.sh`, mode `0755`):

```sh
#!/bin/sh
# mothdex-launcher v3
exec "<bin_dir>/mothdex-gui"
```

Windows (`Mothdex.bat`, CRLF, UTF-8 without BOM):

```bat
@echo off
rem mothdex-launcher v3
setlocal DisableDelayedExpansion
"<bin_dir>\mothdex-gui.exe"
exit /b %errorlevel%
```

**No argument forwarding** (T-70 IC-6): `cmd` re-parses `%*`, so an `&` in an argument would run a command, and a
Port needs no arguments. EmuELEC passes Port arguments to the script, but nothing shows Mothdex needs them (T-84
OQ-PRT-1). If a frontend turns out to pass arguments the GUI needs, they come back in the `.sh`
template only, with the GUI rejecting unknown positional arguments.

`<bin_dir>` is the directory where the binaries actually landed: after the `install.sh` noexec fallback to
`$HOME/.local/bin`, it is the fallback directory.

**A Port has no usable terminal** (OQ-PRT-1, Verified from source): Batocera and EmuELEC redirect the child's stdout and
stderr to logs and create no PTY, and RetroBat runs a `.bat` with a hidden window. The `mothdex tui` fallback line
therefore cannot work from a frontend. D32 (PRT-G8) removed it: a system where the GUI is not installed gets no
launcher at all, and the CLI and TUI stay available from a terminal or SSH.

### 2.3 Environment: the launcher sets nothing

- The launcher **never sets `LD_LIBRARY_PATH`**, `LD_PRELOAD`, `PATH`, any `SDL_*` variable or any `MOTHDEX_*`
  variable. It does not `cd`. The GUI inherits the frontend's environment unchanged.
- **Why:** SDL2 is found by soname through the system loader (§3). A library path set by the launcher would widen the
  search to directories that may be writable (the share partition) and defeat the planting tests of the spike (T-52).
- What the frontends pass (OQ-PRT-1, OQ-PRT-2, OQ-PRT-4, Verified from source; [T-84](../../research/T-84-ports-open-questions.md)):
  - **Batocera** (master): `os.environ` plus `SDL_GAMECONTROLLERCONFIG` generated from the ES controllers and
    `SDL_RENDER_VSYNC`; `LC_ALL` and `LANG` from `system.language`; no `LD_LIBRARY_PATH` override in the shell generator.
  - **EmuELEC** (4.8): the Port wrapper sources `/etc/profile`, which **prepends `LD_LIBRARY_PATH=/emuelec/lib:/emuelec/lib32`**
    and exports `SDL_GAMECONTROLLERCONFIG_FILE=/storage/.config/SDL-GameControllerDB/gamecontrollerdb.txt`; stock
    `LANG` can be reset to `C.UTF-8` while `LANGUAGE` keeps the ES language.
  - **RetroBat** (7.0-era source): the Windows environment; neither mapping variable is set.
- The GUI does not unset inherited values: EmuELEC's library path looks required by its own stack, and removing it is a
  choice for after the beta. Consequence: `--probe` runs with a scrubbed environment (§5.2, IC-1) while the Port run
  inherits EmuELEC's library path, so the SDL2 the probe found may not be the one the Port loads (lib32 entries are
  skipped by the loader as the wrong ELF class). PRT-G9 (§10) and B12 cover it.
- **EmuELEC override directories (T-91, Sonda):** `/emuelec` links to `/storage/.config/emuelec`, so `/emuelec/lib`
  and `/emuelec/lib32` are **writable** user override directories that take precedence over the system. A fresh 4.8
  build ships only placeholder files there (Inferred from the recipes), while the stock SDL2 2.32.10 and the Mali, EGL,
  GLES and libdrm libraries are in `/usr/lib` and `/usr/lib32`. A library a user, or anything that can write
  `/storage`, puts there is loaded by the GUI, which runs as root. That is residual R2 of T-70, now with a concrete
  path (Sentinela). The launcher still does not clear it (§2.3 rule), and PRT-G9's diagnostics name the source of
  SDL2 only: later EGL or Mali loads are not covered. B12 holds the device check.
- A fix that needs a per-system setting goes into the profile data and the GUI, never into the launcher template.

## 3. SDL2 per system

The rules are those of gui.md §2 and stay unchanged:
- Linux: `dlopen("libSDL2-2.0.so.0")` by soname only; SDL2 is **never bundled** on Linux.
- Windows: the pinned `SDL2.dll` 2.32.10 (`scripts/sdl2-windows.pin`) next to `mothdex-gui.exe`, loaded with
  `LOAD_LIBRARY_SEARCH_APPLICATION_DIR|LOAD_LIBRARY_SEARCH_SYSTEM32`, after `SetDefaultDllDirectories` with the same flags
  for the whole process (gui.md §2, T-70 IC-8).
- The binding needs SDL 2.0.0 at least and targets 2.0.18 or later (T-52).

| System | SDL2 source | Known | Arch of the GUI archive | Interpreter |
|---|---|---|---|---|
| Batocera | system `libSDL2-2.0.so.0` | SDL2 2.32.10 on master and 44 ✅ (T1b #17) | amd64, arm64, armv7, armv6 from `uname -m` | glibc ✅; armhf `/lib/ld-linux-armhf.so.3` Inferred from EABIHF + glibc (T-84 OQ-PRT-6), image path ⚠ B5 |
| EmuELEC | system `libSDL2-2.0.so.0` (the Port inherits `LD_LIBRARY_PATH=/emuelec/lib:/emuelec/lib32`, §2.3) | SDL2 2.32.10; Amlogic builds `mali` only, Rockchip builds KMSDRM (T-84 OQ-PRT-3) | arm64 | glibc Inferred (T1b #24); supported 4.8 is **aarch64-only**, lib32 is compatibility only (T-84 OQ-PRT-7, Documented) |
| Recalbox | system `libSDL2-2.0.so.0` | SDL2 2.30.11 in 11.0-patron-1 (T-84 OQ-PRT-3) | amd64, arm64, armv7 (rpi3 and rpizero2 are 32-bit, T1e #14) | glibc ✅; armhf Inferred (T-84 OQ-PRT-6), image path ⚠ B5 |
| RetroBat | the pinned `SDL2.dll` from the archive | RetroBat's own SDL is unknown (T1b #22) and not used | windows amd64 | n/a |

Batocera `x86_wow64` (i686 userland) gets no GUI in v0.3.0: linux 386 is optional in gui.md §6, and the eligibility
probe (§5.2) fails there, so no launcher is written (D32).

## 4. Gamepad mapping, display and locale

The mapping source is gui.md §4, the same on every system. Later sources override earlier ones:
1. the embedded `gamecontrollerdb.txt` (pin `4e87e3c6c257`);
2. `<DataDir>/gamecontrollerdb.txt`;
3. `SDL_GAMECONTROLLERCONFIG_FILE`, then `SDL_GAMECONTROLLERCONFIG`, as passed by the frontend.

| System | Frontend mapping passed to Ports | Video driver | Locale for Ports |
|---|---|---|---|
| Batocera | `SDL_GAMECONTROLLERCONFIG` inline, from the ES controllers ✅ (no `_FILE`) | x86_64 Xorg; RPi4 labwc Wayland + XWayland; RPi Zero/1 Wayland without XWayland; KMSDRM when libGBM ✅ build-time, selected driver ⚠ B3 | `LC_ALL` = `LANG` = `<system.language>.UTF-8` ✅ |
| EmuELEC | `SDL_GAMECONTROLLERCONFIG_FILE` (a full DB file) ✅; inline variable ⚠ | Amlogic: SDL2 driver `mali` only; Rockchip targets: KMSDRM ✅ build-time | `LANG` may be `C.UTF-8` while `LANGUAGE` has the ES language (Inferred) ⚠ B13 |
| Recalbox | n/a (no Port) | rpi3/zero2 KMSDRM, rpi4/5 XWayland + KMSDRM, x86_64 Xorg ✅ build-time | n/a |
| RetroBat | neither variable ✅ (7.0-era source) | Windows desktop | no POSIX locale; Windows user locale, gui.md §3 fallbacks apply |

With the gui.md §4 order, Batocera's inline lines and EmuELEC's whole DB file override the embedded DB for the same
controller. That is intended (the frontend's pad setup wins). EmuELEC's file may be older than our pin, so a pad can map
as the older line says; B4 checks it.

**ES keeps running** around a Port on all three (OQ-PRT-1, Verified from source): it waits for the child and resumes.
It releases input and renderer only when `HideWindow` is set, which is off by default on Batocera, forced off by
EmuELEC on Amlogic and off on Windows. Whether the GUI actually gets focus, display and pads is B2.

**Diagnostics for the beta (Vitral, small).** At start, `mothdex-gui` writes one log line with:
- the loaded SDL version, where SDL2 was loaded from as one word (`system`, `ld_library_path`, `other`, `unknown`; `n/a` on Windows; never a path), and the video driver SDL chose. The word `ld_library_path` means SDL2 was found through a directory listed in `LD_LIBRARY_PATH`, not that it is an override: a system directory listed there gives the same word, so check which directory it was. On stock EmuELEC the listed `/emuelec/lib*` directories are empty overrides and the stock SDL2 is in `/usr/lib` (T-91);
- whether `LD_LIBRARY_PATH`, `SDL_VIDEODRIVER`, `SDL_GAMECONTROLLERCONFIG` and `SDL_GAMECONTROLLERCONFIG_FILE` are
  set, as names with `set` or `unset`, and the value of `SDL_VIDEODRIVER` only;
- `LANG`, `LC_ALL` and the first entry of `LANGUAGE`, each only when it looks like a word (PRT-G10);
- the number of mapping lines added from each source and the type of each connected controller;
- whether `LD_PRELOAD` and `SDL_DYNAMIC_API` are set, as names only (T-70 IC-13).

No value of a mapping, path list or other variable is logged, and no path at all. This line is what the beta report template (§8) asks
testers to paste.

## 5. Install, upgrade and uninstall

Everything goes through the existing installers (`install.sh`, `install.ps1`). There is no new installer and no new
download channel. All checks of release-artifacts.md and of the installers stay: checksum always, before anything is
executable; cosign when available; TH-30 paths; TH-31 directory ownership; the exec-bit probe and its fallback.

### 5.1 Flags and the D22 flip

| Flag (`install.sh` / `install.ps1`) | Effect |
|---|---|
| `--launcher` / `-Launcher` | the default; kept for compatibility. The launcher is written only when the GUI is installed |
| `--no-launcher` / `-NoLauncher` | the installer does not write or rewrite the launcher. When the GUI is not installed, an existing marked launcher is still removed (it would start nothing; Regente, T-88) |
| `--gui` / `-Gui` | the default: install `mothdex-gui` when the release has an archive for the machine |
| `--no-gui` / `-NoGui` | installs the CLI only; removes an installed GUI, its notices and a marked launcher |

- **D22 as amended by D32:** "the launcher is on when the GUI is installed". Whether a GUI archive exists comes only from
  the release's verified `checksums.txt` (T-70 IC-3). A `mothdex-gui_` archive that is not listed for the arch means
  the GUI part is skipped with a note, and so is the launcher. One that is listed but cannot be downloaded or verified
  aborts the whole install.
- **Without the GUI** (not eligible, a probe error, `--no-gui`, no GUI archive): nothing is written. An existing launcher
  that carries a `v1`, `v2` or `v3` marker is removed with a note, and the frontend is asked to reload (§5.5). A file
  without a marker, or a link, is never touched. The removal does not run the folder-trust check: IC-7 guards writes, because a written launcher is
  later run by the frontend. Deleting a marked regular file creates nothing that could run, `rm -f` on a path swapped
  for a link removes the link and not its target, and the worst an untrusted folder can do is make a Port disappear (Crivo S2).
- T-70 §6 keeps the default on condition of IC-7 (`install.ps1` writes `Mothdex.bat` only into a trusted `roms\windows`, as
  `install.sh` already does) and IC-12 (every run that writes the launcher prints its path and how to opt out).
- `--dry-run` prints the GUI archive, the eligibility result (§5.2) and the launcher path, and leaves nothing changed
  (PR-88 decision a). It probes only when `bin_dir` already exists and passed the trust check: it stages the binary
  there, runs `--probe` as in §5.2 step 3 and always removes the staged file. When `bin_dir` does not exist yet, it
  creates nothing and prints "eligibility not checked".

### 5.2 GUI eligibility (Linux)

The GUI is dynamic on Linux, so the installer must prove it can start before it writes the launcher that prefers it.

1. Download `mothdex-gui_<version>_linux_<arch>` from the **same release** as `mothdex_`, with the same arch mapping
   (`uname -m`, release-artifacts.md §3). Versions are never mixed.
2. Verify it exactly like the CLI archive: SHA-256 against the same `checksums.txt`, cosign when available. **A
   verification failure aborts the whole install** before anything is written, as for the CLI; it never degrades to
   "CLI only".
3. Extract by exact member name only (T-70 IC-4), copy the binary to its staging name in `bin_dir`, and run
   `mothdex-gui --probe` there with a scrubbed environment and a time bound (IC-1, IC-2, IC-10), never from the temp
   dir. Exit 0 means eligible: rename it into place. Exit 8 means not eligible: delete the staging file. Releases 0.0.x
   and 0.1.x said "not eligible" with exit 1, so for those versions only 1 counts as not eligible; from 0.2.0 on exit 1
   is an internal error. An exec failure (126, 127, `ENOEXEC`) is an installer error that is reported, never a silent
   "not eligible".
4. Not eligible (missing interpreter, missing or too old SDL2, wrong userland arch): warn with the probe's one-line
   reason, do not install the GUI, and keep the CLI install. No launcher is written, and a marked one is removed (D32).
5. Exit status (PR-88 decision b): not eligible, or no GUI archive in the release, is a normal outcome, exit 0. A
   probe that cannot run, or any exit code other than those of step 3, is an installer error: the CLI install and the
   launcher still complete, the installer changes neither the PATH nor its install state (T-110 TM-11), and exits 1. On Windows every non-zero probe result is that error (IC-9).

**`mothdex-gui --probe`** (gap PRT-G3, Vitral):
- loads SDL2 exactly as at normal start (§3), calls `SDL_GetVersion` and applies the version rule;
- opens no window, initializes no video or input subsystem, reads no config and writes no file or log;
- gives up by itself after 15 s (one line "SDL2 did not answer within 15 seconds", exit 1), inside the installer's
  own 20 s bound (T-70 IC-10), so a library that blocks in a constructor cannot hang the install even without `timeout`;
- prints `sdl <major>.<minor>.<patch>` and, on Linux, a word for where the loader found SDL2 (`system`, `ld_library_path`, `other` or `unknown`; never a path), in one sanitized line of at most 200 bytes, and exits 0, or prints the reason and exits 1, the code gui.md §5 uses for SDL
  problems.

This replaces the `ldconfig -p` check of gui.md §6. One run proves the interpreter, the userland arch and the SDL
soname together, and does not depend on `ldconfig` being present on these images (Unverified).

Windows: the archive carries `SDL2.dll`. `install.ps1` runs `mothdex-gui.exe --probe` the same way. A failure there is
a packaging bug, so it is reported as an error. As on Linux, neither `mothdex-gui.exe` nor `SDL2.dll` is installed
(T-70 IC-9); the CLI install still completes, no launcher is written, and a marked one is removed (D32).

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

| System | Binaries | GUI | Launcher (only when the GUI is installed, D32) |
|---|---|---|---|
| Batocera | `/userdata/system/bin` ⚠ (convention) | if eligible | `Mothdex.sh` in `/userdata/roms/ports` |
| EmuELEC | `/storage/.local/bin` ⚠ | if eligible | `Mothdex.sh` in `/storage/roms/ports_scripts` |
| Recalbox | `/recalbox/share/system/bin` ⚠ | if eligible (runs from a desktop or terminal session only) | none (`launcher: none`; no generic shell-Port system in 11.0, OQ-PRT-8) |
| RetroBat | `%LocalAppData%\Programs\mothdex` | always (Windows archive) | `Mothdex.bat` in `<RetroBat>\roms\windows` |

On Batocera and Recalbox shares without permission bits, the TH-31 root-only FAT exception of profiles.md §4 applies
to the launcher folder unchanged. Batocera runs a shell Port as `/bin/bash <script>` (OQ-PRT-5, Verified from source),
so the **script** needs no exec bit; the binaries in `bin_dir` still do, which the exec-bit probe checks (B8).

### 5.5 Upgrade

Re-running the installer is the upgrade path:
- It replaces `mothdex`, `mothdex-gui` and `SDL2.dll` atomically (§5.3) with the files of the release it installs.
- With the GUI installed, it rewrites the launcher **only** if the existing file carries a marker it knows (`v1`, `v2`
  or `v3`); a `v1` or `v2` launcher becomes `v3`.
- Without the GUI, it removes a marked launcher of any version (§5.1); `--no-launcher` does not stop that removal.
- A file without a marker is kept, with the existing warning.
- An upgrade with `--no-gui` removes an installed `mothdex-gui` (and `SDL2.dll` on Windows) and the marked launcher.
- **Versions are never mixed (§5.2 step 1).** Any install that does not install this release's GUI (no archive
  listed, not eligible, probe error, or `--no-gui`) also removes a `mothdex-gui`, its `SDL2.dll` and `mothdex-licenses/`
  left by an earlier install, and says so. Otherwise the launcher would keep starting an older GUI next to the new CLI,
  or a GUI the probe has just shown cannot start.
- After writing or removing a launcher, the installer runs the profile's refresh once (profiles.md §4: `http_get` only where
  verified, never fatal), so the Port shows in the list. On Recalbox and other `manual` profiles it prints the hint.

### 5.6 Uninstall

`--uninstall` / `-Uninstall` removes:
- `mothdex`, `mothdex-gui`, `SDL2.dll` (Windows) and `mothdex-licenses/` from `bin_dir`;
- the launcher, only when it carries a `v1`, `v2` or `v3` marker.

It keeps the config, data, history, cache and every downloaded ROM. It never removes a file without a marker, and it
lists what it removed. `--dry-run` lists without removing.

`mothdex-licenses/` has no manifest (PR-88 decision c). Each installer holds a fixed list of the notice names it ships,
equal to the files of `THIRD_PARTY/` in the repository, `README.md` included. Extraction refuses any other
`THIRD_PARTY/` member, and uninstall, upgrade and `--no-gui` remove only regular files with those names. They never
follow a link or reparse point, and remove the folder only if it is then empty. Anything else is kept, with a warning,
so a file of the user's in that folder is never deleted. A change to `THIRD_PARTY/` changes both lists in the same PR,
and a test fails when they differ.

**Installer and release go together (fail closed).** An installer accepts a GUI archive whose `THIRD_PARTY/` is a subset
of its list, so a newer installer still installs an older release. An installer older than a release that adds a
notice refuses that release's GUI archive as an unexpected member, and installs nothing. That is intended: it never
installs a notice it cannot later remove. The release notes of every release that adds a notice say so, and repeat
the rule of docs/release.md: fetch `install.sh` and `install.ps1` from the release tag. `SDL2.dll` is removed only
when `mothdex-gui.exe` is next to it.

## 6. Third-party notices

The repo keeps the notices in `THIRD_PARTY/` (`README.md` plus licence texts), and the GUI archives carry that
folder (gui.md §6). For Ports:

| Component | Licence | Where the notice lives |
|---|---|---|
| Noto Sans (embedded) | OFL-1.1 | `THIRD_PARTY/NotoSans-OFL-1.1.txt` in the archive, installed to `bin_dir/mothdex-licenses/` |
| Handjet (embedded) | OFL-1.1 | `THIRD_PARTY/Handjet-OFL-1.1.txt` in the archive, installed to `bin_dir/mothdex-licenses/` |
| GameControllerDB (embedded) | zlib | `THIRD_PARTY/SDL_GameControllerDB-zlib.txt`, same |
| SDL2.dll 2.32.10 (Windows, shipped) | zlib | `THIRD_PARTY/SDL2-zlib.txt`, in every GUI archive (PRT-G2, done in T-71) |
| purego (in the binary) | Apache-2.0 | `THIRD_PARTY/purego-Apache-2.0.txt` (Apache-2.0 §4(a); PRT-G2, done) |
| golang.org/x/image (in the binary) | BSD-3-Clause | `THIRD_PARTY/x-image-BSD-3-Clause.txt` (PRT-G2, done) |

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

`HOME` for root is Batocera `/userdata/system`, EmuELEC `/storage`, Recalbox `/recalbox/share/system` (OQ-PRT-9,
Verified from source). These are persistent **normally**. All three fall back to a RAM-backed tmpfs when the storage
mount fails (Batocera 256 MiB; EmuELEC live mode or no disk). Then config, history and every download live in RAM and
are lost at reboot. PRT-G11 (§10) proposes a warning. The fix is never a launcher variable (§2.3).

## 8. What stays unverified (community beta, D28 9a)

There is no hardware (ROADMAP risks), so these items ship labelled Unverified in the release notes until a beta report
confirms each one on a named system and device:

| # | Item | Systems | Tester steps (T-84) |
|---|---|---|---|
| B1 | The Port appears in the list and starts the GUI; the frontend comes back on exit (gui.md exit codes) | Batocera, EmuELEC, RetroBat | launch from ES twice; record system, image, build, device; check the ES process survives; exit by menu and by hotkey |
| B2 | The GUI gets focus, display and every pad while ES waits (ES keeps running, §4); whether the child has a controlling terminal | Batocera, EmuELEC, RetroBat | record `isatty` of fds 0, 1 and 2 and whether a controlling terminal exists (a `TERM` value is not evidence); try every connected pad |
| B3 | The video driver SDL picks, and legibility at 640x480 and 480x320 (gui.md §3) | Batocera, EmuELEC | the diagnostics line (driver, `SDL_VIDEODRIVER` set or unset); compare an ES launch with an SSH launch, which is not equivalent |
| B4 | Pads map correctly; the frontend mapping reaches the GUI (Batocera inline, EmuELEC `_FILE`) and overrides the embedded line | all | mapping counts per source from the diagnostics line; a pad with a custom frontend mapping visibly different from the embedded DB |
| B5 | `/lib/ld-linux-armhf.so.3` exists on armv6 and armv7 images, and real ARMv6 runs the GUI | Batocera, Recalbox | `test -e` and `readlink -f` of the loader, `readelf -l /bin/bash`, then the real `--probe`; record image, CPU and `uname -m` |
| B6 | `bin_dir` and `HOME` persist across reboot and upgrade, and the RAM fallback is seen (§7) | Batocera, EmuELEC, Recalbox | `findmnt -T "$HOME"` (privately); a harmless sentinel file under the data dir across a reboot and an upgrade, then delete it |
| B7 | `--probe` result on each image; EmuELEC 4.8 userland is aarch64 | Batocera, EmuELEC, Recalbox | `getconf LONG_BIT`, ELF class of `/bin/bash` and `/usr/bin/emulationstation`, then `--probe` (`uname -m` alone is the kernel) |
| B8 | A `.sh` Port on an exFAT share runs (Batocera reads it with `bash`) | Batocera | a mode-0644 script on a filesystem with modes, then the same Port on exFAT; record mount options; test `bin_dir` exec separately |
| B9 | The refresh after install shows the Port | Batocera, EmuELEC, RetroBat | |
| B10 | Upgrade from a `v1` or `v2` launcher to `v3`; a marked launcher goes when the GUI does; uninstall leaves ROMs and config intact | all | |
| B11 | On RetroBat the GUI window is visible although the `.bat` runs hidden | RetroBat | launch from ES; record whether the window shows, gets focus and is fullscreen |
| B12 | Which SDL2 the Port actually loads on EmuELEC, with `/emuelec/lib` first in `LD_LIBRARY_PATH` (§2.3) | EmuELEC | the SDL version in the diagnostics line against `--probe` run over SSH; privately, whether `/emuelec/lib` holds a `libSDL2-2.0.so.0` and who can write it |
| B13 | The GUI language follows the ES language | Batocera, EmuELEC | a non-English ES language, restart ES; record `LANG`, `LC_ALL`, `LANGUAGE` and the language the GUI chose |

Public reports keep library-path and mapping values redacted (IC-13); the steps above that read them are private.

The beta report template (one issue per system and device, D4) asks for: system and version, device and CPU arch,
the installer output, the GUI diagnostics line (§4), and each B-item as pass, fail or not tried. Guindaste writes the
template ([`ports-beta.yml`](../../../.github/ISSUE_TEMPLATE/ports-beta.yml)); the compatibility matrix lists the B-items per system ([`ports-beta-matrix.md`](../../qa/ports-beta-matrix.md)).

## 9. Open questions: answers (T-84)

Research [T-84](../../research/T-84-ports-open-questions.md) (Sonda, 2026-11-22; 69 sources at pinned commits: Batocera master `15820e4`, EmuELEC v4.8 `02b45fe`,
RetroBat 7.0-era source, Recalbox 11.0-patron-1 `1392898`). "Verified from source" is code at that revision, not a run on
a device. Batocera results are for master (toward 45) and are not backported to 41 to 43.

| Id | Answer | Confidence | Source | Left to the beta |
|---|---|---|---|---|
| OQ-PRT-1 | ES waits and resumes on all three; it releases input and renderer only with `HideWindow`, off by default. No terminal: Linux stdout and stderr go to logs, no PTY; RetroBat runs the `.bat` hidden. Environment per §2.3 (EmuELEC prepends `LD_LIBRARY_PATH`). | Verified from source; terminal unusable is Inferred | T-84 OQ-PRT-1: [Batocera `sh.py`](https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/emulators/sh.py#L36), [EmuELEC `99-emuelec.conf`](https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/sx05re/emuelec/profile.d/99-emuelec.conf#L4) | B1, B2, B11 |
| OQ-PRT-2 | Batocera: inline `SDL_GAMECONTROLLERCONFIG`. EmuELEC: `SDL_GAMECONTROLLERCONFIG_FILE`. RetroBat: neither. | Verified from source | T-84 OQ-PRT-2: [Batocera `emulator.py`](https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/emulator.py#L857) | B4 |
| OQ-PRT-3 | Build-time backends per §4 table; EmuELEC Amlogic registers the driver `mali`. No `SDL_VIDEODRIVER` export found in the shell-Port generators (not proof of unset). | Verified from source (build time) | T-84 OQ-PRT-3: [EmuELEC SDL2 recipe](https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/packages/multimedia/SDL2/package.mk#L17) | B3 |
| OQ-PRT-4 | Batocera sets `LC_ALL` and `LANG` from `system.language`. EmuELEC may hand the Port `LANG=C.UTF-8` with the language only in `LANGUAGE`. RetroBat: no POSIX locale. | Verified (Batocera); Inferred (EmuELEC) | T-84 OQ-PRT-4: [Batocera standalone](https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/package/batocera/emulationstation/batocera-emulationstation/emulationstation-standalone#L86) | B13 |
| OQ-PRT-5 | Batocera runs `/bin/bash <script>`: the script needs no exec bit. The binaries still do. | Verified from source | T-84 OQ-PRT-5: [`sh.py`](https://github.com/batocera-linux/batocera.linux/blob/15820e4000df65d140b63ee2d0f14ca6238d2b35/python-src/batocera-launch/batocera_launch/emulators/sh.py#L36) | B8 |
| OQ-PRT-6 | Batocera ARMv6 and ARMv7 and Recalbox rpi3/zero2 are EABIHF glibc, so the loader is expected at `/lib/ld-linux-armhf.so.3`. Recalbox 11.0 ships no ARMv6 image config. | Inferred | T-84 OQ-PRT-6 | B5 |
| OQ-PRT-7 | Supported EmuELEC after 3.9 is aarch64-only; lib32 is compatibility libraries, not a 32-bit ES or shell. Keep the arm64 GUI; never pick armv7 automatically. | Documented | T-84 OQ-PRT-7: [EmuELEC README](https://github.com/EmuELEC/EmuELEC/blob/02b45fe53b85c45c75dbad8e44a648092dec2e72/README.md#L10) | B7 |
| OQ-PRT-8 | Recalbox 11.0 has game-specific port systems and a Ports collection, but no generic shell-Port system. `launcher: none` stays. | Verified from source; supported custom route Unknown | T-84 OQ-PRT-8 | none (Recalbox has no Port) |
| OQ-PRT-9 | `HOME` per §7; persistent normally, RAM tmpfs when the storage mount fails, on all three. | Verified from source | T-84 OQ-PRT-9 | B6 |
| OQ-PRT-10 | muOS and Knulli: deferred, still out of scope (§1). | Unknown | T-84 OQ-PRT-10 | n/a |

## 10. Gaps and owners

| Id | Owner | Item |
|---|---|---|
| PRT-G1 | Guindaste | `profiles/retrobat.yaml` `bin_dir` says `${MATCH}\mothdex`, while `install.ps1` and profiles.md §3 use `%LocalAppData%\Programs\mothdex`. Align the YAML with the installer |
| PRT-G2 | Guindaste | add `SDL2-zlib.txt`, the Apache-2.0 text for purego and the BSD-3 text for `x/image` to `THIRD_PARTY/` and to its README table. CLI archives with BSD-licensed Go modules: backlog check |
| PRT-G3 | Vitral | `mothdex-gui --probe` (§5.2) and the start-up diagnostics line (§4) |
| PRT-G4 | Guindaste | installers: GUI archive, eligibility, launcher (now `v3`, D32), `--no-gui`/`--no-launcher` and `-NoGui`/`-NoLauncher`, the default flip, `mothdex-licenses/`, uninstall and upgrade rules; installer tests for each case (`tests/installer/`); the beta report template (§8) |
| PRT-G5 | Alicerce, with Guindaste's PR | gui.md §6: replace the `ldconfig -p` check with `--probe`, and `THIRD_PARTY.md` with `THIRD_PARTY/`; release-artifacts.md: the static-binary sentence (already listed in gui.md §6) |
| PRT-G6 | Bússola | **closed (v1: no metadata).** The name `Mothdex` comes from the file name. No `gamelist.xml` entry: the file belongs to the frontend and the person, and EmulationStation rewrites it on exit and on scrape. No image in v1: art is deferred to the brand session (D28 8b). If that session approves art, this contract adds how each frontend shows it without writing `gamelist.xml` (for example a media file next to the script), Unverified per frontend and checked under B1 |
| PRT-G7 | Sentinela | **closed by T-70** ([threat model](../../security/T-70-threat-model-ports-installer.md)): constraints IC-1 to IC-13 bind PRT-G3 and PRT-G4, IC-8 (`SetDefaultDllDirectories` in `mothdex-gui.exe`) goes to Vitral; residuals R1-R5 recorded there |
| PRT-G8 | Guindaste (installer, High), Sentinela delta, Alicerce text | **Done (D32, T-88, cli#109): §2.2 and §5 describe it.** Was: **Proposed (T-84 OQ-PRT-1), needs a decision:** a Port has no terminal, so drop the `tui` fallback. The launcher is written only when the GUI is installed; a `v3` template runs `mothdex-gui` only. Not eligible, `--no-gui` or no GUI archive means no launcher, and an existing marked launcher is removed with a note. D22 then reads "launcher on when the GUI is installed". |
| PRT-G9 | Vitral (GUI, Medium) and Sonda; Sentinela for R2 | **Done (T-89, cli#110); see the T-91 bullet of §2.3.** Was: **Proposed (T-84 OQ-PRT-1):** the probe runs scrubbed while the EmuELEC Port inherits `/emuelec/lib` first. The diagnostics line reports whether the loaded SDL2 came from a system directory or from an `LD_LIBRARY_PATH` entry, as a word, never a path, next to its version. Sonda checks whether `/emuelec/lib` is read-only and holds a `libSDL2-2.0.so.0`. The launcher still sets nothing. |
| PRT-G10 | Forja (i18n, Medium); Sentinela for the IC-13 change | **Done (T-90, cli#108; clarified T-100).** The first non-empty value of `LC_ALL`, `LC_MESSAGES`, `LANG` decides. If it is `C` or `C.UTF-8` (including codeset/case variants), or all three are unset, try the first entry of `LANGUAGE` for EmuELEC. An explicit `POSIX` keeps English and ignores `LANGUAGE`, unlike `C`: this is the deliberate compatibility choice, covered by `TestNormalizeAndDetect` in `internal/i18n`. With `C` and no usable `LANGUAGE`, keep English. The diagnostics line adds `LANGUAGE` as a plain word, like `LANG`. |
| PRT-G11 | Forja (profiles, Medium), Bússola for the text | **Done (T-90, cli#108; follow-ups T-100).** When the ROM root or data directory is on tmpfs or ramfs, profile detection adds a warning: downloads and history are in RAM and lost at reboot. A missing folder is checked through its nearest existing ancestor; other filesystem errors leave storage unknown. `StorageWarnings []string` retains its English messages; startup, GUI System and CLI profile output translate the known messages in the selected locale. |
| PRT-G12 | Vitral (GUI, Medium) | **Done (T-89, cli#110).** Was: **Proposed (T-84 OQ-PRT-1):** RetroBat starts the `.bat` hidden. Check that the GUI window does not inherit a hidden show state on Windows and shows itself explicitly; verified by B11. |

## 11. Test plan (risk paths)

- **Installer cases** (`tests/installer/`, containers with a fake file tree, no hardware):
  - eligibility pass and fail, including a missing interpreter and a missing soname;
  - a checksum mismatch on the GUI archive aborts the whole install;
  - `v1` and `v2` to `v3` upgrade;
  - a file without a marker is kept;
  - `--no-gui` removes the GUI and the marked launcher; without the GUI no launcher is written (D32);
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
