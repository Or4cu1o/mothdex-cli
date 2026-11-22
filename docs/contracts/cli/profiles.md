# Contract: system profiles (`internal/library`, `profiles/*.yaml`)

Status: draft v1 (T-2 phase 2) · Owner: Alicerce · Implementer: Forja (T-15) · Installer consumer: Guindaste (T-21) · Risk: Medium
Sources: [T1e research](../../research/T1e-emulation-systems.md) (Sonda), PLAN §9 errata, [`mothdex-db/platforms.py`](https://github.com/cavv-dev/mothdex-db/blob/main/platforms.py) (Mothdex platform ids).

A profile tells the CLI **where ROMs go** on a given system, where the installer puts the binary and launcher, and how
to make the frontend notice new files. Profiles are **data** (YAML embedded in the binary, overridable by the user);
the Go code only evaluates them.

## 1. Principles

- **Fail safe to `generic`.** Any doubt (no match, ambiguous match, ROM root missing or not writable, unreadable
  probe) selects `generic` and adds a warning. Detection never fails a command.
- **Unverified facts are labelled.** Each probe, path or hook carries `verified: true|false` (from T1e). `profile show`
  prints unverified items, so community beta testers (D4) can confirm them.
- **The user always wins:** `--profile <id>` › config `profile.id` › auto-detection. Config can override the ROM root
  and individual folder names.
- Detection is a **pure function** over an injected environment (`fs.FS` rooted at `/`, `GOOS`, home dir, executable dir,
  env lookup). Tests use fake file trees and no real system.

## 2. Profile schema (YAML, `schema: 1`)

```yaml
schema: 1
id: batocera                 # [a-z0-9-]+, unique
name: Batocera
os: linux                    # linux | windows | darwin | any
priority: 100                # higher wins when several match; ties => ambiguous => generic
detect:                      # all `all` probes must pass, plus at least one `any` probe when `any` is present
  all:
    - { file: /usr/share/batocera/batocera.version, verified: true }
  any: []
roms:
  root: /userdata/roms       # may use ${HOME}, ${EXE_DIR}, ${CONFIG:<key>}, ${MATCH}
  root_verified: true
  folders_verified: true     # whether the `folders` names are confirmed (omitted = false)
  layout: per_system         # per_system (folder from map) | per_platform_id (folder = Mothdex id)
  unmapped: error            # error | platform_id  (what to do for a Mothdex platform missing from `folders`)
  folders: { md: megadrive, scd: megacd, ps1: psx }   # Mothdex platform id -> folder (see §5)
bin_dir: { path: /userdata/system/bin, verified: false }   # persistent dir for the installer (T-21)
launcher:                    # Port / launcher created by the installer; kind none = no launcher
  kind: sh                   # sh | bat | none
  dir: /userdata/roms/ports
  verified: true
refresh:                     # after a download batch
  kind: http_get             # http_get | manual | none
  url: http://127.0.0.1:1234/reloadgames
  timeout: 3s
  verified: true
```

Probe types, all read-only: `file` (exists), `dir` (exists), `os_release: {KEY: value}` (`/etc/os-release`, exact and
case-sensitive match), `glob` (at least one match, ≤ 2 path levels of wildcards), `config: <key>` (the config value is
non-empty and the path exists), `files: [path, …]` (every listed file exists; used for RetroBat's `RetroBat.exe` +
`retrobat.ini`). Exactly one type per probe. A probe that errors (permission, I/O) counts as **not matched**.

**Variables** (T-15, accepted): `${HOME}`, `${EXE_DIR}`, `${CONFIG:<key>}` (only the keys the loader allows) and
`${MATCH}`. `${MATCH}` is the path found by detection, taken from the first passing probe that yields one (`all`
probes in order, then the matching `any` probe):

| Probe | `${MATCH}` |
|---|---|
| `file` | directory of the file |
| `files` | directory of the **first** listed file |
| `dir` | the directory |
| `glob` | the first match in sorted order (a warning names the others when several match, e.g. two SD cards) |
| `config` | the configured path if it is a directory, otherwise its directory |
| `os_release` | none |

`${MATCH}` is valid only in paths used **after** detection (`roms.root`, `bin_dir.path`, `launcher.dir`), never inside
a probe. The loader rejects a profile that uses `${MATCH}` when none of its probes can yield a path. At runtime an
empty `${MATCH}` makes the root unresolvable, so detection falls back to `generic` (§1).

`folders_verified` covers the whole `folders` map; when it is false (or omitted), `profile show` lists "folder
names" among the unverified items. Current values: `true` for Batocera, EmuELEC and RetroBat; `false` for Recalbox,
EmuDeck and RetroDECK (inferred names, T1e §4).

`folders` keys must be Mothdex platform ids from `/platforms`; unknown keys are a load warning. **Values** (T-16 CR-2,
CR-3) must be a single local component (`filepath.IsLocal`, no separator), and the loader **rejects** (error, profile
not loaded) any value equal to a launcher folder (`ports`, `ports_scripts`, `windows`, or the last component of
`launcher.dir`), so a download can never land where the frontend runs scripts. Embedded profiles are
validated in CI. User overrides go in `<config dir>/profiles/<id>.yaml`, which replaces the embedded profile with the
same id, or adds a new one.

## 3. Profiles

| id | Detection | ROM root | Layout | bin_dir | Launcher | Refresh |
|---|---|---|---|---|---|---|
| `batocera` | file `/usr/share/batocera/batocera.version` ✅ | `/userdata/roms` ✅ | per_system | `/userdata/system/bin` ⚠ (convention; `/userdata/system` is persistent ✅) | `sh` in `/userdata/roms/ports` ✅ | `http_get` reloadgames ✅ |
| `emuelec` | os_release `ID=emuelec` ✅ | `/storage/roms` ✅ | per_system | `/storage/.local/bin` ⚠ | `sh` in `/storage/roms/ports_scripts` ✅ | `http_get` reloadgames ✅ |
| `recalbox` | dir `/recalbox/share/roms` ⚠ (any: file `/recalbox/recalbox.version` ⚠) | `/recalbox/share/roms` ✅ | per_system | `/recalbox/share/system/bin` ⚠ | `none` (no ports system in 10.1.2) | `manual` (no reload API found ⚠) |
| `lakka` | os_release `NAME=Lakka` ✅ (any: `ID=lakka` ✅) | `/storage/roms` ✅ | per_platform_id (Lakka has no per-system convention) | `/storage/.local/bin` ⚠ | `none` (SSH use only) | `none` (RetroArch scanner) |
| `retrobat` | config `profile.retrobat_path` containing `RetroBat.exe` + `retrobat.ini` ✅; else `C:\RetroBat\RetroBat.exe` ⚠; registry not used ⚠ | `<retrobat>\roms` ✅ | per_system | `%LocalAppData%\Programs\mothdex` (per-user, TH-31; T-21b) | `bat` in `<retrobat>\roms\windows` ✅ (`ports` takes only `.libretro`) | `http_get` reloadgames ✅ |
| `emudeck` | dir `${HOME}/Emulation/roms` ✅ (any: glob `/run/media/*/Emulation/roms` ✅, card name varies) | the matched dir | per_system (ES-DE names ⚠ inferred) | `${HOME}/.local/bin` ⚠ | `none` (Steam/ES-DE shortcut: backlog) | `manual` (no ES-DE HTTP reload ⚠) |
| `retrodeck` | dir `${HOME}/retrodeck/roms` ✅ (custom path inside the Flatpak config ⚠: user sets `profile.rom_root`) | the matched dir | per_system (ES-DE names) | `${HOME}/.local/bin` ⚠ | `none` (backlog) | `manual` |
| `generic` | always (fallback, priority 0) | `<paths.data>/ROMs`; portable: `<exe dir>/ROMs` (config.md) | per_platform_id | OS default (installer's choice) | `none` | `none` |

✅ = Confirmed in T1e (source or docs, not hardware). ⚠ = Unverified or Inferred: it must be confirmed by the community
beta before the profile is called stable.

**Bazzite** is a host, not a ROM layout: `os_release ID=bazzite` is recorded as `host: bazzite` in `ProfileInfo`. The
layout comes from `emudeck` or `retrodeck`. If both match, that is ambiguous, so `generic` is selected with a hint to
set `profile.id`. EmuDeck and RetroDECK on other Linux hosts (SteamOS, Fedora) use the same profiles.

**Detection order** is by `priority`: `batocera` 100, `emuelec` 90, `lakka` 90, `recalbox` 80, `retrobat` 80,
`emudeck` 50, `retrodeck` 50, `generic` 0. A profile is selected only if its ROM root also exists and is writable (or
creatable, for `generic`).

## 4. Runtime behavior

```go
type ProfileInfo struct {
    ID, Name, Host string
    Source   string   // auto | config | flag
    RomRoot  string
    Verified bool     // false if any probe/path/hook used is unverified
    Warnings []string // why generic was chosen, unverified items, ambiguity
}

func Detect(env Env, profiles []Profile, cfg Config) ProfileInfo   // pure
func (p Profile) DestDir(platformID string) (string, error)         // ErrUnmappedPlatform when unmapped=error
func (p Profile) Refresh(ctx context.Context, h HTTPDoer) error     // never fatal for callers
```

- **Destination** (used by app-service `DownloadRequest.DestDir == ""`): `RomRoot/<folder>`. In `per_platform_id`
  layout the folder is the platform id, which must match `^[a-z0-9][a-z0-9_-]{0,31}$` and be in `/platforms`;
  confinement to `RomRoot` applies in every layout (downloader §6.2). If the platform is missing
  from `folders` and `unmapped: error`, the job fails in Preflight with code `unmapped_platform` (exit 2). The hint names
  `-o <dir>` or a `profile.folders.<id>` override. Interactive UIs offer a folder picker instead.
  Arcade ids (`mame`, `fbn`, `naomi`…) are deliberately **unmapped** on systems that split arcade by emulator, so the
  user chooses (T1e §4).
- **Refresh** runs **once per batch** after `Wait` returns with at least one `Done` job, never per file. `http_get`
  (T-16 CR-10) accepts only the URL `http://127.0.0.1:1234/reloadgames` or `http://[::1]:1234/reloadgames` (scheme
  `http`, fixed path `/reloadgames`, no query; host only the IP literals `127.0.0.1` or `[::1]`; the loader rejects anything else, including
  `localhost`), uses a client with `Proxy: nil`, no redirects, a 3 s timeout, and reads at most 64 KiB of body. The ES reply arrives before the
  reload finishes, and a failure becomes a warning. `manual` prints the localized hint `profile.refresh_manual`.
- **Installer contract (T-21):** artifact names, the `uname` mapping and checksum verification are fixed in
  [`release-artifacts.md`](release-artifacts.md). `install.sh` uses the same detection rules, read from `mothdex profile show --json`
  after a first run from a temp dir, or ported as a shell `case` with the same probes. It must check that `bin_dir`
  supports the exec bit (NTFS/exFAT do not, per T1e) and fall back to another directory with a warning. The launcher
  is opt-in (D22: `--launcher`, `-Launcher` on Windows; off by default while `mothdex tui` does not exist), and
  `--uninstall` still removes a launcher it created. The launcher file runs `<bin_dir>/mothdex-gui` (or `mothdex tui`
  until E-M3). The E-M3 launcher, GUI install and the D22 default flip are in [`ports.md`](ports.md).
- **World-writable directories (TH-31):** `bin_dir` and the launcher dir must not be world-writable and must be owned
  by the installing user or root; a symlinked `bin_dir` is refused. The only exception is a share partition without
  permission bits on an appliance: the installer runs as root, the profile is `batocera` or `recalbox`, the directory
  is inside `/userdata` or `/recalbox/share`, is not a symlink, is owned by root, and `stat -f` proves the filesystem
  is exFAT, FAT or NTFS (`exfat`, `vfat`, `msdos`, `ntfs`, `ntfs3`, `fuseblk`). The installer then warns that any
  process or SMB client that can write there can replace the binary. A failed detection keeps the refusal.

## 5. Folder map (Mothdex id → frontend folder)

The maps live in `profiles/*.yaml` (data). Source: [T15b research](../../research/T15b-platform-folders.md) (Sonda,
2026-11-20), which reads each frontend's own system definition for all 67 Mothdex ids. Applied in T-15c with
Regente decisions D18 (`pc` → `windows`, `tg16`/`tgcd` names, arcade unmapped) and D19 (`pc` unmapped in v0.1):

| Profile | Mapped ids | `folders_verified` | Basis |
|---|---|---|---|
| `batocera` | 48 | true | `es_systems.yml` keys (Confirmed) |
| `emuelec` | 41 | true | basename of `path` in `es_systems.json`, not the system `name` (e.g. `dos` → `pc`, `ngcd` → `neocd`) |
| `retrobat` | 47 | true | `path` in `es_systems.cfg` |
| `retrodeck` | 48 | false | ES-DE names; RetroDECK's own layout not checked in its repository |
| `emudeck` | 48 | false | ES-DE names; EmuDeck's layout is Inferred |
| `recalbox` | 40 | false | `system.ini` names: 51 of 146 read, the rest Inferred from directory names |
| `lakka`, `generic` | — | — | `per_platform_id` layout (folder = Mothdex id) |

Rules for the map (D18):

- **Same name everywhere** where the frontend has it; region variants (`genesis`, `megadrivejp`, `tg16`, `tg-cd`, …)
  are not used. `md` → `megadrive` (OQ-PRO-2), `tg16`/`tgcd` → `pcengine`/`pcenginecd` (D18; the only names present in all
  five frontends; EmuELEC and ES-DE users who keep `tg16` override per folder).
- `3ds` and `n3ds` share one folder (`3ds`, or `n3ds` on ES-DE): no frontend separates them.
- **No system in that frontend → unmapped** (for example `ps2` on EmuELEC, `wiiu` on Recalbox, `dsi` everywhere, since
  DSi titles usually share `nds` and that is not confirmed).
- **Arcade ids stay unmapped on purpose** (D18; `mame`, `hbmame`, `fbn`, `naomi`, `naomi2`, `chih`, `tri`, `lind`, `s246`,
  `ring`, `ring2`, `m2`, `sgv`, `s573`, `fbeat`, `eamu`). These frontends split arcade games by **emulator and romset
  version** (`mame`, `fbneo`, `neogeo`, `cps1`–`3`, `atomiswave`, `namco2x6`, …), and a set only runs in the folder
  whose emulator matches its romset. The platform id alone cannot pick that folder, so a wrong guess would put games
  where they never start. The user chooses with `-o <dir>` or `profile.folders.<id>`, and `unmapped: error`
  explains this.
- **`pc` is unmapped in v0.1** (Regente decision D19, OQ-PRO-4). D18 says `pc` → `windows` where it exists (Batocera,
  RetroBat, ES-DE), but `windows` is a launcher folder (T-16 CR-3: RetroBat runs `.exe`/`.bat`/`.lnk` from
  `roms\windows`), so the loader rejects that value and downloads into it need `--allow-executable` (downloader §6.2).
  EmuELEC and Recalbox have no Windows system (D18: none).

## 6. Config additions (config.md §2)

```yaml
profile:
  id: auto            # auto | batocera | emuelec | recalbox | lakka | retrobat | emudeck | retrodeck | generic
  rom_root: ""        # overrides the profile's root
  retrobat_path: ""   # Windows: RetroBat install dir
  folders: {}         # Mothdex id -> folder, merged over the profile map
  refresh: true       # false disables the post-batch hook
```

Env: `MOTHDEX_PROFILE_ID`, `MOTHDEX_PROFILE_ROM_ROOT`, … (config.md §4 rule). Flag: `--profile`.

## Resolved questions (Regente, T-2 phase 2)

- OQ-PRO-1: moved to Risks & Backlog (device verification by the community beta, D4).
- OQ-PRO-2: accepted. ES-DE maps `md` to `megadrive`; users can override per folder.
- OQ-PRO-3: moved to Risks & Backlog (ROCKNIX and muOS, post-1.0 with PortMaster).
- OQ-PRO-4 (D19): `pc` stays unmapped in v0.1. D18's `pc` → `windows` conflicts with the launcher-folder rule (CR-3); users who want it pass `-o` and `--allow-executable`.
