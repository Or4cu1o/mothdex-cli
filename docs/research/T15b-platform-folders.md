# T-15b: ROM folder names per frontend for all 67 Mothdex platform ids

Ref: T-15b · Author: Sonda · Risk: Low (research only) · Checked: 2026-11-20 · Supports: T-15 (mothdex-cli `profiles/*.yaml`, [`profiles.md`](../contracts/cli/profiles.md) §5)

## Short answer

All 67 ids from `GET https://api.mothdex.net/v1/platforms` (one request, 4622 bytes, 2026-11-20) are in the table. Folder names are the **folder the frontend itself uses** for a system, read from its official system definition (sources below). Every non-`none` folder name was checked by script to exist in the corresponding definition; 0 names were missing. Counts of mapped ids: Batocera 57, EmuELEC 44, RetroBat 57, ES-DE 54, Recalbox 44 (of 67). The profile's `profiles/*.yaml` is empty on `main` (`.gitkeep` only), so the "15 mapped" baseline is the §5 seed table in `profiles.md` (e0898e5); the seed rows agree with this table (arcade ids differ by design, see ⁷).

Legend: `x` = Confirmed (name exists in the definition, see sources) · **none** = no system for this platform in that definition (Confirmed absent in that file/tag) · ¹–⁹ see "Ambiguous ids" · ᵃ = EmuELEC folder is the **`path` basename**, which differs from the system `name` (e.g. name `pc-9800`, folder `pc98`); use the path, not the name.

## Table

| Mothdex id | Mothdex name | Batocera | EmuELEC | RetroBat | ES-DE (EmuDeck, RetroDECK) | Recalbox |
|---|---|---|---|---|---|---|
| `nes` | Nintendo Entertainment System | `nes` | `nes` | `nes` | `nes` | `nes` |
| `fds` | Famicom Disk System | `fds` | `fds` | `fds` | `fds` | `fds` |
| `snes` | Super Nintendo Entertainment System | `snes` | `snes` | `snes` | `snes` | `snes` |
| `n64` | Nintendo 64 | `n64` | `n64` | `n64` | `n64` | `n64` |
| `64dd` | Nintendo 64DD | `n64dd` | none | `n64dd` | `n64dd` | `64dd` |
| `gc` | GameCube | `gamecube` | `gamecube` | `gamecube` | `gc` | `gamecube` |
| `wii` | Wii | `wii` | `wii` | `wii` | `wii` | `wii` |
| `wiiu` | Wii U | `wiiu` | none | `wiiu` | `wiiu` | none |
| `vb` | Virtual Boy | `virtualboy` | `virtualboy` | `virtualboy` | `virtualboy` | `virtualboy` |
| `ps1` | PlayStation | `psx` | `psx` | `psx` | `psx` | `psx` |
| `ps2` | PlayStation 2 | `ps2` | none | `ps2` | `ps2` | `ps2` |
| `ps3` | PlayStation 3 | `ps3` | none | `ps3` | `ps3` | none |
| `sms` | Master System | `mastersystem` | `mastersystem` | `mastersystem` | `mastersystem` | `mastersystem` |
| `md` | Mega Drive / Genesis | `megadrive` | `megadrive`¹ | `megadrive` | `megadrive`¹ | `megadrive` |
| `scd` | Sega CD & Mega CD | `megacd` | `segacd` | `megacd` | `segacd`² | `segacd` |
| `32x` | 32X | `sega32x` | `sega32x` | `sega32x` | `sega32x` | `sega32x` |
| `sat` | Saturn | `saturn` | `saturn` | `saturn` | `saturn` | `saturn` |
| `dc` | Dreamcast | `dreamcast` | `dreamcast` | `dreamcast` | `dreamcast` | `dreamcast` |
| `xbox` | Xbox | `xbox` | none | `xbox` | `xbox` | `xbox` |
| `x360` | Xbox 360 | `xbox360` | none | `xbox360` | `xbox360` | none |
| `tg16` | PC Engine & TurboGrafx-16 | `pcengine` | `pcengine`³ | `pcengine` | `pcengine`³ | `pcengine` |
| `tgcd` | PC Engine CD & TurboGrafx CD | `pcenginecd` | `pcenginecd`³ | `pcenginecd` | `pcenginecd`³ | `pcenginecd` |
| `pcfx` | PC-FX & PC-FXGA | `pcfx` | `pcfx` | `pcfx` | `pcfx` | `pcfx` |
| `a26` | Atari 2600 | `atari2600` | `atari2600` | `atari2600` | `atari2600` | `atari2600` |
| `a52` | Atari 5200 | `atari5200` | `atari5200` | `atari5200` | `atari5200` | `atari5200` |
| `a78` | Atari 7800 | `atari7800` | `atari7800` | `atari7800` | `atari7800` | `atari7800` |
| `jag` | Atari Jaguar | `jaguar` | `atarijaguar` | `jaguar` | `atarijaguar` | `jaguar` |
| `jcd` | Jaguar CD Interactive Multimedia System | `jaguarcd` | `atarijaguarcd` | `jaguarcd` | `atarijaguarcd` | none |
| `ngcd` | Neo Geo CD | `neogeocd` | `neocd`ᵃ | `neogeocd` | `neogeocd` | `neogeocd` |
| `3do` | 3DO Interactive Multiplayer | `3do` | `3do` | `3do` | `3do` | `3do` |
| `cdi` | CD-i | `cdi` | `cdi`ᵃ | `cdi` | `cdimono1` | `cdi` |
| `intv` | Intellivision | `intellivision` | `intellivision` | `intellivision` | `intellivision` | `intellivision` |
| `cv` | ColecoVision | `colecovision` | `coleco`ᵃ | `colecovision` | `colecovision` | `colecovision` |
| `pip` | Pippin | none | none | none | none | none |
| `gb` | Game Boy | `gb` | `gb` | `gb` | `gb` | `gb` |
| `gbc` | Game Boy Color | `gbc` | `gbc` | `gbc` | `gbc` | `gbc` |
| `gba` | Game Boy Advance | `gba` | `gba` | `gba` | `gba` | `gba` |
| `pmin` | Pokémon Mini | `pokemini` | `pokemini` | `pokemini` | `pokemini` | `pokemini` |
| `nds` | Nintendo DS | `nds` | `nds` | `nds` | `nds` | `nds` |
| `dsi` | Nintendo DSi | none⁶ | none⁶ | none⁶ | none⁶ | none⁶ |
| `3ds` | Nintendo 3DS | `3ds`⁴ | `3ds`⁴ | `3ds`⁴ | `n3ds`⁴ | none |
| `n3ds` | New Nintendo 3DS | `3ds`⁴ | `3ds`⁴ | `3ds`⁴ | `n3ds`⁴ | none |
| `psp` | PlayStation Portable | `psp` | `psp` | `psp` | `psp` | `psp` |
| `psv` | PlayStation Vita | `psvita` | none | `psvita` | `psvita` | none |
| `gg` | Game Gear | `gamegear` | `gamegear` | `gamegear` | `gamegear` | `gamegear` |
| `lynx` | Atari Lynx | `lynx` | `atarilynx` | `lynx` | `atarilynx` | `lynx` |
| `mame` | MAME | `mame`⁷ | `mame`⁷ | `mame`⁷ | `mame`⁷ | `mame`⁷ |
| `hbmame` | HBMAME | none | none | `hbmame` | none | none |
| `fbn` | FinalBurn Neo | `fbneo`⁷ | `fbneo`⁷ᵃ | `fbneo`⁷ | `fbneo`⁷ | `fbneo`⁷ |
| `naomi` | NAOMI | `naomi`⁷ | `naomi` | `naomi` | `naomi` | `naomi`⁷ |
| `naomi2` | NAOMI 2 | `naomi2` | none | `naomi2` | `naomi2` | `naomi2` |
| `chih` | Chihiro | `chihiro` | none | `chihiro` | none | none |
| `tri` | Triforce | `triforce` | none | `triforce` | `triforce` | none |
| `lind` | Lindbergh | `lindbergh` | none | `lindbergh` | none | none |
| `ring` | RingEdge | none | none | none | none | none |
| `ring2` | RingEdge 2 | none | none | none | none | none |
| `s246` | System 246 | `namco2x6`⁸ | none | `namco2x6`⁸ | none | none |
| `m2` | M2 | none⁹ | none | none | none | none |
| `sgv` | System GV | none | none | none | none | none |
| `s573` | System 573 | none | none | none | none | none |
| `fbeat` | FireBeat | none | none | none | none | none |
| `eamu` | e-Amusement | none | none | none | none | none |
| `dos` | MS-DOS | `dos` | `pc`⁵ | `dos` | `dos` | `dos` |
| `pc` | PC | `windows`⁵ | none | `windows`⁵ | `windows`⁵ | none |
| `mac` | Macintosh | `macintosh` | `macintosh` | none | `macintosh` | `macintosh` |
| `pc98` | PC-98 Series | `pc98` | `pc98`ᵃ | `pc98` | `pc98` | `pc98` |
| `fmt` | FM Towns | `fmtowns` | `fmtownsux`ᵃ | `fmtowns` | `fmtowns` | none |

## Ambiguous ids (one id to several folders, or the reverse)

1. ¹ `md`: ES-DE and EmuELEC define **both** `megadrive` and `genesis` (regional variants). Table uses `megadrive` (matches contract OQ-PRO-2). Batocera, RetroBat and Recalbox have only `megadrive`. ES-DE also has `megadrivejp`.
2. ² `scd` ("Sega CD & Mega CD"): Batocera and RetroBat use `megacd`; EmuELEC, Recalbox use `segacd`; ES-DE has `segacd` (table), `megacd` and `megacdjp`. Table keeps the seed-map values.
3. ³ `tg16`/`tgcd`: EmuELEC has both `pcengine` and `tg16` (and `pcenginecd` / `tg16cd`); ES-DE has `pcengine`/`tg16` and `pcenginecd`/`tg-cd`. Table uses `pcengine`/`pcenginecd` (the seed choice, and the only one present in all five). A user on EmuELEC or ES-DE who stores TurboGrafx games in `tg16` needs the override.
4. ⁴ `3ds` and `n3ds` (reverse case): no frontend has a separate New 3DS folder; both ids go to the same folder (`3ds`, ES-DE `n3ds`). Inferred (folder semantics), not stated by a source. Recalbox 10.1.2 has no 3DS system.
5. ⁵ `pc` vs `dos`: Batocera, RetroBat, ES-DE have `windows` (table for `pc`) and `dos`. EmuELEC's `pc` system is **DOS x86**, so `dos` maps to `pc` there and Mothdex `pc` has **none**. ES-DE also has `pc` (IBM PC) and `windows3x`/`windows9x`; Mothdex does not say which Windows era `pc` means (**Unverified**). Recalbox 10.1.2 has `dos` but no Windows system.
6. ⁶ `dsi`: no frontend has a DSi system; DSi titles normally share `nds`. **Unverified** whether each frontend's emulator accepts DSi-only titles, so the table says none instead of guessing `nds`.
7. ⁷ Arcade (`mame`, `fbn`, `naomi`, `hbmame`): the names exist, but these frontends split arcade by emulator or set (also `arcade`, `neogeo`, `cps1-3`, `atomiswave`...), so a folder is not determined by the platform alone. The contract keeps them unmapped by choice; the table shows only the same-named folder. `hbmame` exists only in RetroBat. ᵃ EmuELEC's `fbn` system uses folder `fbneo`.
8. ⁸ `s246` (System 246): Batocera and RetroBat group it under `namco2x6` (System 246/256), so it is one folder for several Mothdex-like boards. Mapping is Inferred from the name.
9. ⁹ `m2` ("M2"): not Sega Model 2 (that is `model2`, which Mothdex lists no id for). The Mothdex `m2` hardware has no folder in any of the five definitions; the identification with Panasonic M2 is **Unverified**.
10. Other none-cases worth noting: `ring`, `ring2`, `sgv`, `s573`, `fbeat`, `eamu`, `pip` have no system in any of the five. `pip` (Pippin) was searched by name in all five and not found.
11. Other reverse cases: none further. Recalbox has `neogeocd` (not `ngcd`); ES-DE `neogeocd` and `neogeocdjp` (region), `sega32x`, `sega32xjp`, `sega32xna`, `saturnjp`, `snesna` exist as region variants (table keeps the base name).

## Sources (official system definitions)

| Frontend | File | Ref (checked 2026-11-20) | Folder rule |
|---|---|---|---|
| Batocera | https://github.com/batocera-linux/batocera.linux/blob/master/package/batocera/emulationstation/batocera-es-system/es_systems.yml | master @ `82bef98ef7cbce9f32cb3327f00610e4b25247e5` | top-level key = system = ROM folder (5 explicit `path:` entries exist: `ports`, `videopacplus`, `/userdata/screenshots` and others, none for the platforms in the table). **Confirmed** by presence of key; folder=key rule is **Inferred** from the file layout |
| EmuELEC | https://github.com/EmuELEC/EmuELEC/blob/dev/packages/sx05re/emuelec-emulationstation/config/es_systems.json (the task named `es_systems.cfg`; the repo ships `.json`) | dev @ `7051dda96e3e0fe2d205efb9f68aa9f9eef277c8` (latest tag v4.8) | folder = basename of `path` (`/storage/roms/<folder>`), **not** `name` |
| RetroBat | https://github.com/RetroBat-Official/retrobat/blob/main/system/templates/emulationstation/es_systems.cfg | main @ `57b1ac2ba63941768aae225eb9430a96d655d3de` (latest tag beta_8.4.0) | `path` = `~\..\roms\<folder>` |
| ES-DE | https://gitlab.com/es-de/emulationstation-de/-/blob/master/resources/systems/linux/es_systems.xml | master @ `50ca983a8de5b104c32d296ee60b6af6ccb307bb` | `path` = `%ROMPATH%/<folder>`; EmuDeck uses ES-DE names (**Inferred**, not checked in the EmuDeck repo; T1e #36); RetroDECK ships ES-DE |
| Recalbox | https://gitlab.com/recalbox/recalbox/-/tree/10.1.2/package/recalbox-romfs2/systems (one directory per system, `system.ini` with `name`) | tag `10.1.2` (commit `f669095411…`, short; full SHA not retrieved). 11.0 exists only as alpha/patron tags, not used | folder = `system.ini` `name`. **Confirmed** for 51 of 146 systems (name equals directory); the other 95 are **Inferred** from the directory name because GitLab rate-limited (HTTP 429) further reads |

The "systemlist" named in the request is, for Recalbox, the `recalbox-romfs2/systems/*` tree above (no file called `systemlist` was found in 10.1.2). The ES-DE file is the Linux one; the Windows and macOS files were not compared. Recalbox `system.ini` has per-board overrides (`key.<boardtag>`); whether a system is enabled on every board was not checked, so `xbox` and `wii` on Recalbox mean "defined", not "shown on all boards".

## Risks and limitations

- Folder names are from definitions, not from devices; no hardware run (as in T1e, "Confirmed" = source).
- Recalbox: 95 of 146 `system.ini` unread (rate limit); impacts the **Confirmed** label only, not the table content, since names matched directory names in all 51 samples. Re-check with a clone of the tag if T-15 wants the label raised.
- Batocera, EmuELEC, RetroBat, ES-DE are branch heads, not release tags (commits given). Names change rarely but do; T-15 should pin the profile `folders_verified` date.
- ES-DE system names are for the default ES-DE ROM layout; custom systems or EmuDeck changes to `%ROMPATH%` layout are out of scope.
- Mothdex names were not cross-checked against `mothdex-db/platforms.py` (the API list was the single source). `pc`, `m2`, `s246` and `dsi` meanings come from names only.

## Open questions for Regente/Alicerce

1. `pc` should mean Windows (→ `windows`) or DOS-era PC (→ `pc` on ES-DE/EmuELEC)? Needs an owner call.
2. For `tg16`/`tgcd`, keep `pcengine`/`pcenginecd` everywhere, or switch EmuELEC and ES-DE to `tg16`/`tg-cd`?
3. Arcade ids stay unmapped (contract) or get the same-named folder with a warning?
