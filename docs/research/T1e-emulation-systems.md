# T1e - Emulation systems: detection, ROM folders, persistence, ports, refresh

Ref: T1e. Author: Sonda. Date checked: 2026-11-20. Supports: PLAN-v0 section 2.2 (`internal/library` profiles table), 2.7 (`install.sh`). Research only; nothing was run on a real device (all "Confirmed" rows come from source repos or official docs, not hardware).

## 1. Question

For Batocera, Recalbox, EmuELEC, Lakka, RetroBat, Bazzite (EmuDeck, RetroDECK): how to detect the system, ROM root and per-system folder names, persistent writable dir for user binaries (noexec), how Ports/custom launchers are added, how to refresh the game list, CPU architectures, and whether curl/wget/CA certs exist. Check PLAN 2.2 table.

## 2. Short answer

- **Detection is solid for Batocera, EmuELEC, Lakka, Bazzite (os-release / version file); weak for Recalbox and RetroBat** (no primary-source version file or registry key found).
- **Game-list refresh `GET http://127.0.0.1:1234/reloadgames` is confirmed in source for Batocera, EmuELEC and RetroBat** (same ES HTTP server code; localhost allowed by default on Batocera). **Recalbox: no equivalent found** (its `recalbox-api` is a legacy config API); Lakka/ES-DE: none.
- **Folder names differ and the PLAN's "Ports" column has 3 errors** (EmuELEC, RetroBat, Recalbox), see section 5.
- Sega Genesis: `megadrive` on Batocera, Recalbox, RetroBat; EmuELEC and ES-DE have **both** `genesis` and `megadrive`. Sega CD: `megacd` (Batocera, RetroBat) vs `segacd` (Recalbox, EmuELEC, ES-DE).
- Architectures: Batocera has x86_64, aarch64, armv6/armv7 (hard-float), riscv64; Recalbox has x86_64, arm64 and **32-bit arm** (rpi3/rpizero2/rpi1); EmuELEC aarch64 (32-bit libs side-by-side); RetroBat Windows 64-bit only.

## 3. Evidence: detection and layout

| # | Claim | Label | Source | Version | Checked |
|---|---|---|---|---|---|
| 1 | Batocera writes `/usr/share/batocera/batocera.version` (version + date) and `/usr/share/batocera/batocera.arch` at build | Confirmed | https://github.com/batocera-linux/batocera.linux/blob/master/package/batocera/core/batocera-system/batocera-system.mk | master (45-dev), branch 44 | 2026-11-20 |
| 2 | Batocera `/etc/os-release` fields (ID/NAME) | Unverified (Buildroot default is not confirmed for Batocera; check on device) | - | - | - |
| 3 | Batocera hostname `batocera`, issue "READY TO RETRO"; ES stack = batocera-emulationstation | Confirmed | .../configs/batocera-board.common | master | 2026-11-20 |
| 4 | Batocera user data at `/userdata` (roms, saves, bios, system); `/userdata/roms/ports` takes `.sh` only | Confirmed | https://wiki.batocera.org/systems:ports ; board/batocera/fsoverlay/etc/init.d/S11share | v44/master | 2026-11-20 |
| 5 | Batocera persistent scripts: `/userdata/system/services/`, `/userdata/system/scripts/`; `custom.sh` abolished in v43 | Confirmed | https://wiki.batocera.org/launch_a_script | v43+ | 2026-11-20 |
| 6 | Batocera mount script `S11share` has **no `noexec`** option; scripts need the exec bit, which NTFS/exFAT lack (docs) | Confirmed (no noexec in init script) / Confirmed (exec-bit warning) | S11share; wiki launch_a_script | master | 2026-11-20 |
| 7 | Batocera includes wget, curl, ca-certificates (`BR2_PACKAGE_WGET`, `LIBCURL_CURL`, `CA_CERTIFICATES` selected) | Confirmed | .../batocera-system/Config.in lines 314-317 | master | 2026-11-20 |
| 8 | Batocera ES HTTP server: `GET /reloadgames`, port 1234; listens 127.0.0.1 unless "Enable public web access" is set | Confirmed | https://github.com/batocera-linux/batocera-emulationstation (es-app/src/services/HttpServerThread.cpp); https://wiki.batocera.org/es-webserver | master | 2026-11-20 |
| 9 | Batocera ES also exposes `POST /addgames/{system}`, `/removegames/{system}`, `/launch`, `/notify`, `GET /systems` | Confirmed | same HttpServerThread.cpp | master | 2026-11-20 |
| 10 | Batocera arches: x86_64, x86_wow64 (i686), aarch64 (bcm2837...), arm hard-float cortex-a7/a9/a17 (bcm2836, h3, s812, rk3288), armv6 (bcm2835), riscv64 (jh7110); glibc | Confirmed | .../configs/batocera-*.board | master | 2026-11-20 |
| 11 | Recalbox: ROMs at `/recalbox/share/roms/<system>`; `/boot` and `/partitions` read-only; SFTP/SSH available | Confirmed (wiki search snippet) | https://wiki-next.recalbox.com/en/tutorials/system/access/network-access-sftp | wiki | 2026-11-20 |
| 12 | Recalbox version file `/recalbox/recalbox.version` exists on device | Unverified (a commit says file is written at build; device path not confirmed) | https://gitlab.com/recalbox/recalbox (commit "recalbox.version ... overwritten at compile time") | - | 2026-11-20 |
| 13 | Recalbox: hostname `RECALBOX`, issue "PLAY AGAIN"; x86_64 image has WGET, LIBCURL_CURL, CA_CERTIFICATES; glibc | Confirmed (x86_64) / Unverified (other boards) | https://gitlab.com/recalbox/recalbox (configs/recalbox-x86_64_defconfig) | 10.1.2 | 2026-11-20 |
| 14 | Recalbox boards: x86_64, rpi1, rpi3, rpi4, rpi4_64, rpi5_64, rpizero2, odroidxu4, odroidgo2, rg351v, rg353x; **rpi3 and rpizero2 defconfigs are 32-bit `BR2_arm` (cortex-a53)**, rpi5_64 is aarch64 | Confirmed | https://gitlab.com/recalbox/recalbox (configs/, package/recalbox-system/) | 10.1.2 | 2026-11-20 |
| 15 | Recalbox 10.1.2 romfs2 systems list has **no `ports` system** (ports are separate systems such as `doom`, `quake`, `cavestory`, ...) | Confirmed (10.1.2) / Unverified (11.0) | https://gitlab.com/recalbox/recalbox/-/tree/10.1.2/package/recalbox-romfs2/systems | 10.1.2 | 2026-11-20 |
| 16 | Recalbox REST API (`system.api.enabled=0` default) is the legacy Node config API (hostname, wifi, kodi, bios...), **no reload-games endpoint**; web manager on 8080 (`system.manager.enabled=1`) | Confirmed (no endpoint in README) | https://github.com/recalbox/recalbox-api ; recalbox.conf; https://wiki.recalbox.com/en/tutorials/system/access/recalbox-manager-web-interface | api 1.0.12 | 2026-11-20 |
| 17 | Recalbox game-list refresh by HTTP | Unverified (none found; fallback = restart ES or ES menu) | - | - | - |
| 18 | EmuELEC os-release: `NAME="EmuELEC"`, `ID="emuelec"`, `VERSION_ID`, `LIBREELEC_ARCH`, `LIBREELEC_PROJECT`; `/etc/release` holds version; `EE_VERSION` file = `4.8` | Confirmed (generation script) | https://github.com/EmuELEC/EmuELEC (scripts/image, packages/sx05re/emuelec/config/EE_VERSION) | v4.8 (2025-11-02) | 2026-11-20 |
| 19 | EmuELEC ROMs `/storage/roms/<system>` (symlink `/roms`); config `/storage/.config/emuelec`; PortMaster at `/storage/roms/ports/PortMaster` | Confirmed | emuelec package.mk; es_systems.json | dev | 2026-11-20 |
| 20 | EmuELEC `ports` system path is **`/storage/roms/ports_scripts`**, extension `.sh`, run via `emuelecRunApp.sh` | Confirmed | https://github.com/EmuELEC/EmuELEC/blob/dev/packages/sx05re/emuelec-emulationstation/config/es_systems.json | dev | 2026-11-20 |
| 21 | EmuELEC ES fork has `GET /reloadgames` on 1234 (localhost unless public access) | Confirmed | https://github.com/EmuELEC/emuelec-emulationstation (HttpServerThread.cpp lines ~98, 301-303) | master | 2026-11-20 |
| 22 | EmuELEC arch: aarch64 targets (Amlogic-ng, Amlogic-old cortex-a53); has curl, wget, openssl packages; CA bundle presence | Confirmed (arch, packages) / Inferred (CA bundle) | EmuELEC projects/Amlogic-ce, packages/web/curl, packages/sx05re/tools/sysutils/wget | dev | 2026-11-20 |
| 23 | `/storage/.local/bin` (PLAN install dir) on EmuELEC/Lakka | Unverified | - | - | - |
| 24 | Lakka os-release: `NAME="Lakka"`, `ID="lakka"` (v6.1 branch; master template had `ID="libreelec"`, so use NAME/PRETTY_NAME too) | Confirmed | https://github.com/libretro/Lakka-LibreELEC (scripts/image, branches master and Lakka-v6.1) | v6.1 (2026-02-21) | 2026-11-20 |
| 25 | Lakka rootfs is read-only; editable data under `/storage` (ROMs `/storage/roms`), SSH root/root when enabled; curl package exists in tree; wget unknown | Confirmed (RO, paths) / Unverified (wget) | https://www.lakka.tv/doc/Accessing-Lakka-filesystem/ ; Lakka-LibreELEC packages/web/curl | v6.1 | 2026-11-20 |
| 26 | Lakka has no per-system ROM folder convention (RetroArch scanner builds playlists) | Inferred | lakka.tv doc (editable folders list: Playlists, Roms) | v6.1 | 2026-11-20 |
| 27 | RetroBat layout: `<install>\roms\<system>\`, `emulationstation\`, `system\`, `retrobat.ini`, `RetroBat.exe`; install in a short path (`C:\RetroBat`) not Program Files; Windows 10/11 64-bit | Confirmed | https://wiki.retrobat.org (get-started/retrobat-folder-structure, adding-a-game, prerequisites) | 8.3.1 / 8.4.0 beta | 2026-11-20 |
| 28 | RetroBat registry key for install path | Unverified (use `RetroBat.exe`/`retrobat.ini` discovery or user config) | - | - | - |
| 29 | RetroBat ES system `path` template is `~\..\roms\<system>` (relative to `emulationstation\`) | Confirmed | https://github.com/RetroBat-Official/retrobat/blob/main/system/templates/emulationstation/es_systems.cfg | main | 2026-11-20 |
| 30 | RetroBat `windows` system: folder `roms\windows`, extensions `.exe .bat .cmd .lnk ...`; `ports` system is **libretro games only (`.libretro`)** | Confirmed | https://wiki.retrobat.org (supported-game-systems/others/windows, ports) ; es_systems.cfg | 8.3.1 | 2026-11-20 |
| 31 | RetroBat ES fork has `GET /reloadgames` on port 1234 (same HttpServerThread) | Confirmed (source, master branch) | https://github.com/RetroBat-Official/emulationstation (es-app/src/services/HttpServerThread.cpp:526,849) | master | 2026-11-20 |
| 32 | Windows ships curl.exe (Win10 1803+) | Unverified (not checked from MS docs here) | - | - | - |
| 33 | Bazzite os-release: `NAME="Bazzite"`, `ID=bazzite`, `ID_LIKE` from base, `VARIANT_ID=<image name>` (e.g. bazzite, bazzite-deck...), `VERSION_ID` | Confirmed | https://github.com/ublue-os/bazzite/blob/main/build_files/image-info.sh | main (45.20261120) | 2026-11-20 |
| 34 | Bazzite ships Homebrew integration (brew files, `bbrew`, Brewfile in tree) | Confirmed (in repo) / Inferred ("brew already installed" on user machines) | https://github.com/ublue-os/bazzite (system_files/.../homebrew, bbrew-helper) | main | 2026-11-20 |
| 35 | EmuDeck: `Emulation/{bios,roms,saves,storage,tools,hdpacks}` on internal home (`~/Emulation`) or SD at `/run/media/<CARDNAME>/Emulation` (name varies, e.g. mmcblk0p1) | Confirmed | https://emudeck.github.io/cheat-sheet ; https://emudeck.github.io/frequently-asked-questions/steamos | wiki | 2026-11-20 |
| 36 | EmuDeck system folder names = ES-DE names | Inferred (not verified in EmuDeck repo) | ES-DE es_systems.xml | master | 2026-11-20 |
| 37 | RetroDECK default `~/retrodeck` with `roms`, `bios`, `saves`; template path `/home/deck/retrodeck/roms`; Flatpak with `--filesystem=host`; repo GPL-3.0 | Confirmed | https://github.com/RetroDECK/RetroDECK (config/retrodeck/retrodeck.json, net.retrodeck.retrodeck.yml) | main | 2026-11-20 |
| 38 | ES-DE (used by EmuDeck/RetroDECK) system folders `%ROMPATH%/<name>` incl. `genesis` **and** `megadrive`, `segacd`, `ports`, `windows`, `steam`; custom systems in `~/ES-DE/custom_systems/es_systems.xml`; gamelists in `~/ES-DE/gamelists/` | Confirmed | https://gitlab.com/es-de/emulationstation-de (resources/systems/linux/es_systems.xml, USERGUIDE.md) | master | 2026-11-20 |
| 39 | ES-DE has an HTTP reload endpoint | Unverified (none found) | - | - | - |
| 40 | Bazzite arch (x86_64; arm64?) and curl/CA | Unverified (Fedora base normally has curl and ca-certificates: Inferred) | - | - | - |

## 4. Folder-name mapping (per frontend, from each frontend's own system list)

Blank = system not present under that name. "(alt)" = second name also present.

| Mothdex platform | Batocera | Recalbox | EmuELEC | RetroBat | ES-DE (EmuDeck/RetroDECK) | Lakka |
|---|---|---|---|---|---|---|
| nes | nes | nes | nes | nes | nes | free-form |
| snes | snes | snes | snes (also sfc) | snes | snes | free-form |
| megadrive / genesis | megadrive | megadrive | megadrive + genesis | megadrive | genesis + megadrive | free-form |
| mastersystem | mastersystem | mastersystem | mastersystem | mastersystem | mastersystem | free-form |
| gb | gb | gb | gb | gb | gb | free-form |
| gbc | gbc | gbc | gbc | gbc | gbc | free-form |
| gba | gba | gba | gba | gba | gba | free-form |
| psx | psx | psx | psx | psx | psx | free-form |
| n64 | n64 | n64 | n64 | n64 | n64 | free-form |
| dreamcast | dreamcast | dreamcast | dreamcast | dreamcast | dreamcast | free-form |
| mame / arcade | mame (+fbneo, naomi...) | mame (+fbneo, fba) | arcade + mame + fbneo | arcade + mame + fbneo | arcade + mame + fbneo | free-form |
| neogeo | neogeo | neogeo | neogeo | neogeo | neogeo | free-form |
| pcengine | pcengine | pcengine | pcengine | pcengine | pcengine | free-form |
| saturn | saturn | saturn | saturn | saturn | saturn | free-form |
| segacd | **megacd** | segacd | segacd | **megacd** | segacd | free-form |
| gamegear | gamegear | gamegear | gamegear | gamegear | gamegear | free-form |
| sega32x | sega32x | sega32x | sega32x | sega32x | sega32x | free-form |

Sources: Batocera https://github.com/batocera-linux/batocera.linux/blob/master/package/batocera/emulationstation/batocera-es-system/es_systems.yml (no `genesis`, no `segacd` key); Recalbox https://gitlab.com/recalbox/recalbox/-/tree/10.1.2/package/recalbox-romfs2/systems (folder = system `name`, per system.ini; `megadrive`, `segacd` present, no `genesis`); EmuELEC es_systems.json (paths above); RetroBat es_systems.cfg (no `genesis`); ES-DE es_systems.xml. Version/date: Batocera master 2026-11-20; Recalbox 10.1.2; EmuELEC dev; RetroBat main; ES-DE master; all checked 2026-11-20. Label: **Confirmed** for names; "Recalbox folder = system name" is **Inferred** from `name` in system.ini (ROM root `/recalbox/share/roms` is Confirmed, row 11). EmuDeck mapping equals ES-DE: **Inferred**. Batocera/Recalbox/EmuELEC arcade split (mame vs fbneo vs neogeo): a Mothdex "arcade" platform needs a user choice, keep it as data.

## 5. PLAN-v0 section 2.2 table review

| Row | PLAN says | Verdict | Fix / note |
|---|---|---|---|
| Batocera detection | `/usr/share/batocera/batocera.version` | **Correct** (Confirmed, row 1) | Optionally also read `batocera.arch` |
| Batocera ROM root | `/userdata/roms/<sys>` | **Correct** (row 4) | Use `megadrive`, `megacd` names |
| Batocera post-download | `curl localhost:1234/reloadgames`; Port in `/userdata/roms/ports/` | **Correct** (rows 4, 8). Add: `.sh` only; data in `roms/ports/.data` is a wiki convention | Works only from localhost (default) |
| Recalbox detection | `/recalbox` | **Weak/Unverified**: directory exists (Inferred from share path) but no version file confirmed (row 12) | Use `/recalbox/share/roms` + os-release; verify on device |
| Recalbox ROM root | `/recalbox/share/roms/<sys>` | **Correct** (row 11) | |
| Recalbox post-download | "Port/atalho" | **Wrong/Unverified**: no `ports` system in 10.1.2 (row 15); no reload API (rows 16-17) | Say "no refresh; user refreshes in ES or restarts ES (Unverified)" |
| EmuELEC detection | "arquivo de versão do EmuELEC" | **Vague but resolvable**: `/etc/os-release` `ID="emuelec"` (row 18); `EE_VERSION` on-device path Unverified | Prefer os-release |
| EmuELEC ROM root | `/storage/roms/<sys>` | **Correct** (row 19) | |
| EmuELEC Port | `/storage/roms/ports/` | **Wrong**: launcher scripts go in `/storage/roms/ports_scripts/` (row 20); `roms/ports` holds PortMaster data | Also `/reloadgames` exists (row 21) |
| Lakka detection | `/etc/os-release` (`Lakka`) | **Correct** (row 24): `NAME="Lakka"`, `ID="lakka"` (v6.1) | Match NAME, not only ID |
| Lakka ROM root | `/storage/roms/<sys>` | **Partly correct**: root `/storage/roms` Confirmed; per-system subfolders are not a Lakka convention (rows 25-26) | Treat as flat/user-chosen |
| Lakka integration | CLI/TUI via SSH, rootfs read-only, no ES | **Correct** (row 25); SSH must be enabled by the user | |
| RetroBat detection | "registro/pasta RetroBat" | **Registry Unverified**; folder structure Confirmed (rows 27-28) | Detect `RetroBat.exe` + `retrobat.ini`, or user-provided path |
| RetroBat ROM root | `<RetroBat>\roms\<sys>` | **Correct** (rows 27, 29) | |
| RetroBat Port | `.bat` in `roms\windows`/ports | **Half wrong**: `.bat` in `roms\windows` is right; **`ports` accepts only `.libretro`** (row 30) | Drop "ports"; `/reloadgames` exists (row 31) |
| Bazzite detection | `/etc/os-release` + EmuDeck/RetroDECK | **Correct** (`ID=bazzite`, row 33); EmuDeck/RetroDECK detection: check dirs below | |
| Bazzite ROM root | `~/Emulation/roms` or `~/retrodeck/roms` | **Correct as defaults** (rows 35, 37); EmuDeck SD card at `/run/media/<card>/Emulation/roms`; RetroDECK path is configurable (can be on external media) | Read `~/.var/app/net.retrodeck.retrodeck/config/retrodeck/retrodeck.json`? **Unverified location**; ask user |
| Bazzite integration | Steam/ES-DE shortcut (backlog) | OK as backlog; ES-DE custom systems dir `~/ES-DE/custom_systems/` (row 38) | |
| Generic | `./ROMs/<platform>` | n/a (legacy behavior) | Not researched |

PLAN 2.7 `install.sh` dirs: `/userdata/system/bin` is a fine **convention** (Batocera has `/userdata/system/` as the persistent area, row 5; no standard `bin` dir confirmed); `/storage/.local/bin` is **Unverified** (row 23). `curl`/`wget`/CA: present on Batocera (row 7), Recalbox x86_64 (row 13), EmuELEC packages (row 22); **Lakka wget and Windows curl.exe Unverified**.

## 6. Options compared (how to detect)

| Option | Pros | Cons |
|---|---|---|
| A. Ordered probes of specific files (batocera.version, os-release ID/NAME, RetroBat.exe...) + user override flag/config | Deterministic, testable with fixtures | Recalbox/RetroBat probes are weak |
| B. Only `/etc/os-release` | One file | Batocera os-release unverified; Recalbox unknown; RetroBat n/a |
| C. Ask user once and persist | Always right | Poor `install.sh` UX |

## 7. Recommendation and conditions

Option A with a user override. Probe order: Batocera version file, os-release (`ID`: emuelec, lakka, bazzite; Batocera/Recalbox by their own file/path), `/recalbox/share/roms`, Windows `RetroBat.exe`. Keep the platform->folder map as YAML data (table in section 4 as the seed). Conditions:

1. Fix the four PLAN rows flagged in section 5 (Recalbox Port, EmuELEC Port path, RetroBat ports, Lakka per-system folders) before coding `internal/library`.
2. Use `reloadgames` only on Batocera/EmuELEC/RetroBat, GET with a short timeout, treat failure as non-fatal; for Recalbox/ES-DE print "refresh from the frontend menu".
3. Before writing into the persistent dir, check the exec bit and that the filesystem supports it; on failure, fall back to running from another dir or warn (NTFS/exFAT, row 6).
4. Verify the Unverified rows on real devices or images (Batocera and Recalbox os-release, Recalbox version file, RetroBat install discovery) before release.

## 8. Risks and limitations

- All evidence is from source repos and docs, not from running images; image contents may differ (package selected in Buildroot is not always in the final rootfs).
- Frontends rename systems between releases (Recalbox 11 not checked for `ports`).
- Batocera `/reloadgames` is localhost-only by default; running from SSH on the box is fine, from another host is not.
- ES `reloadgames` returns before the reload finishes (it posts to the UI thread); no completion signal (Inferred from code).
- `noexec` on user-mounted network/external storage was not verified for any frontend.

## 9. Open questions

1. Batocera and Recalbox `/etc/os-release` actual `ID`/`NAME`.
2. Recalbox: on-device version file path; any game-list refresh mechanism; `ports` in 11.0.
3. RetroBat: registry key or other install-discovery; does Windows curl.exe exist on supported builds.
4. EmuDeck/RetroDECK: exact system folder lists and where RetroDECK stores its chosen ROM path inside the Flatpak.
5. ROCKNIX and muOS (ROM roots, ports dirs, SDL): not researched beyond SDL package presence (see T1b).
6. Bazzite arm64 builds and its curl/CA presence (Fedora base assumed).
