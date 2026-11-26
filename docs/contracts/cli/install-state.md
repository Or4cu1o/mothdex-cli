# Contract: install state file

Status: draft v1 (T-111) · Owner: Alicerce · Writers: `install.sh`, `install.ps1` (Guindaste, T-113 b), `internal/update`
(T-116) · Readers: `internal/update`, installer `--uninstall` · Risk: High (supply chain; it decides what upgrade and
uninstall touch) · Spec: `beta-adjustments-spec.md` §2 and §5 (workspace `docs/contracts/`) · Threat model:
[`T-110`](../../security/T-110-threat-model-beta-adjustments.md) TM-11 to TM-14 and TM-33.

The file records what an installer run put on the machine. It has two jobs. It tells `mothdex upgrade` that the
installation is managed, and it tells `--uninstall` which alias and PATH entry the installer created, so that nothing
else is removed. It is a record, not an authority: every reader checks it against the disk (§4).

## 1. Location

One file, **`.mothdex-install.json`**, in the install folder, next to `mothdex` or `mothdex.exe`:

| System | Default install folder (`dir`) |
|---|---|
| Windows | `%LocalAppData%\Programs\mothdex`, or `-BinDir` |
| Linux, macOS, WSL (`generic`, `emudeck`, `retrodeck`) | `~/.local/bin`, or `--bin-dir` |
| Batocera / EmuELEC, Lakka / Recalbox | `/userdata/system/bin` / `/storage/.local/bin` / `/recalbox/share/system/bin` (profiles.md), or `--bin-dir` |

The leading dot keeps it out of `ls` in a shared `~/.local/bin`. Readers find it from the running binary, never from
configuration: `filepath.Dir` of `os.Executable()` after `filepath.EvalSymlinks`. A symlinked `mdex` therefore leads
to the real folder.

## 2. Format (schema v1)

UTF-8 JSON, one **flat** object, **no BOM**, at most 16 KiB.

- **Canonical layout:** one field per line, two-space indent, in the order below. Writers always emit exactly this
  layout. Go writes with `json.MarshalIndent(v, "", "  ")` on a flat struct.
- **PowerShell 5.1** writes with `[IO.File]::WriteAllText` and `UTF8Encoding($false)`, because
  `Set-Content -Encoding UTF8` adds a BOM. Readers still strip one leading BOM.
- **TM-14, never code.** The file is never sourced or evaluated. `install.sh` reads it line by line with one strict
  regex per key; paths are limited to the installer's charset `A-Z a-z 0-9 _ . / + -`, so they need no JSON escapes.
  `install.ps1` uses `ConvertFrom-Json`, and Go uses `encoding/json` with `DisallowUnknownFields` off. A line or value
  that fails its rule makes the whole file invalid (§4).

```json
{
  "schema_version": 1,
  "source": "installer",
  "installer": "install.ps1",
  "dir": "C:\\Users\\ana\\AppData\\Local\\Programs\\mothdex",
  "version": "0.2.0",
  "target": "windows_amd64",
  "profile": "retrobat",
  "gui": true,
  "alias": "mdex",
  "alias_kind": "hardlink",
  "alias_file_id": "2861457364:65536:183745",
  "path_added": true,
  "path_kind": "user_env",
  "path_file": "",
  "path_value": "C:\\Users\\ana\\AppData\\Local\\Programs\\mothdex",
  "launcher": "C:\\RetroBat\\roms\\windows\\Mothdex.bat",
  "installed_at": "2026-11-26T14:00:00Z",
  "updated_at": "2026-11-26T14:00:00Z",
  "updated_by": "installer"
}
```

| Field | Type and rule |
|---|---|
| `schema_version` | integer, `1`. A reader that finds a higher number treats the installation as unmanaged (§4). |
| `source` | always `"installer"`. Packages or other tools that write a file like this one in the future use a different value. |
| `installer` | `"install.sh"` or `"install.ps1"`. A run through `install.cmd` reports `install.ps1`. |
| `dir` | absolute install folder, written as the installer resolved it |
| `version` | installed version, SemVer without `v`, the same as `mothdex version` (cli-output.md §3.1) |
| `target` | `<os>_<arch>` of the installed archive (release-artifacts.md §2) |
| `profile` | the profile id the installer used (`generic`, `retrobat`, `batocera`, …) |
| `gui` | `true` when `mothdex-gui` (with `SDL2.dll` and the licence notices on Windows) was installed |
| `alias` | `""` when no alias was created. Otherwise it matches `^[a-z][a-z0-9-]{1,15}$` and is not on the TM-12 deny-list (`mothdex`, `mothdex-gui`, `con`, `prn`, `aux`, `nul`, `com0`-`com9`, `lpt0`-`lpt9`). |
| `alias_kind` | `"symlink"` (Unix, relative target `mothdex`), `"hardlink"` (Windows), or `""` when there is no alias |
| `alias_file_id` | Windows hard link only: the file ID the link had when it was last created, written `<volume serial>:<index high>:<index low>` in decimal from `GetFileInformationByHandle`, and matching `^[0-9]{1,10}:[0-9]{1,10}:[0-9]{1,10}$`. `""` for a symlink or no alias. It identifies a stale link of ours: one that still names an older `mothdex.exe` because a relink failed. Added after v1 was first written, so a state without this field stays valid and counts as `""`, and it is the one field whose line may be missing from the canonical layout. |
| `path_added` | `false` when the PATH already held `dir`, with `--no-modify-path`, or when the write failed |
| `path_kind` | `"user_env"` (Windows user `Path`), `"rc_line"` (a line in `path_file`), `"fish"` (`fish_add_path`, with `path_file` the `config.fish` that was written), or `""` |
| `path_file` | absolute rc file path, or `""` |
| `path_value` | the exact text added: the folder for `user_env`, or the full TM-9 line `export PATH="<dir>:$PATH" # mothdex` for `rc_line` |
| `launcher` | absolute path of the Port or launcher written, or `""` |
| `installed_at`, `updated_at` | RFC 3339 UTC |
| `updated_by` | `"installer"` or `"upgrade"` |

Readers ignore unknown fields, and writers write only the v1 fields: adding a field keeps `schema_version: 1`.
Removing a field or changing its meaning bumps the version.

## 3. Who writes it, and when

- **Installer, last step of a successful install or reinstall,** after the binaries are in place and `--version`
  passed (TM-11), and after the alias and PATH entry. A reinstall keeps `installed_at` and keeps the `path_*` fields when it adds nothing new, then rewrites
  the rest. `--dry-run` writes nothing.
- **Installs from 0.1.x** have no file. Running the one-line installer again creates it; until then `mothdex upgrade`
  reports `update_unavailable` (update.md §6).
- **`internal/update`, after a successful swap and its check (update.md §4).** It sets `version`, `target`, `gui`,
  `updated_at` and `updated_by: "upgrade"`. After relinking a Windows alias it also sets `alias_file_id` to the new
  link's ID. It never changes `dir`, `alias`, `alias_kind`, `path_*`, `profile` or `launcher`.
- **Writing is atomic.** The writer creates a temporary file in the same folder with `O_EXCL`, then renames it over
  the old one (`os.Rename`; `[IO.File]::Replace` or `Move-Item -Force` on Windows). It refuses when
  `.mothdex-install.json` is a link or a reparse point. The file mode is `0644`, and on Windows it inherits the folder
  ACL. Folder permissions are the installer's job (TH-31): the file holds no secret.
- **If the write fails,** the installer warns `mothdex upgrade will not be available for this install` and still
  exits 0. The install itself is complete.
- **`--uninstall`** deletes the file last, after it has removed what the file names.

## 4. Who reads it, and the checks

**Every reader re-validates every value** (TM-14). An invalid file counts as absent: the reader warns and touches
nothing the file names.

**`internal/update`** (managed check, TM-33; update.md §6). The installation is managed only when all of these hold.
Otherwise the result is `update_unavailable`, with `details.reason` set to the first check that failed:

| Check | `reason` |
|---|---|
| the file exists and holds valid JSON within 16 KiB | `no_state` / `bad_state` |
| `schema_version == 1` and `source == "installer"` | `state_newer` / `not_installer` |
| `dir`, cleaned and with symlinks resolved, equals the folder of the running binary (case-insensitive on Windows) | `moved` |
| the folder passes the installer trust rules: not a link or reparse point, not writable by others (Unix mode `o+w`; group write is accepted, as in the installer, because a user-private group with umask 002 makes `0775` common), and on Windows no write ACE for Users, Everyone or Authenticated Users | `untrusted_dir` |
| the effective user owns the folder (so `sudo mdex upgrade` on a user install is refused), and a temporary file can be created and removed in it | `not_owner` / `read_only` |
| every field passes its §2 rule, `version` is valid SemVer, and `target` is the running binary's `GOOS_GOARCH` | `bad_state` |

The file never redirects a write. Upgrade writes only to the running binary's folder and only the names in update.md
§4, so editing `dir` gains nothing.

**Installer `--uninstall`.** It removes only what the file names, and only after checking it on disk:

- **Alias:** removed only when it is a symlink to `mothdex`, or on Windows a hardlink whose file ID equals that of
  `mothdex.exe` or the recorded `alias_file_id` (TM-12, TM-13). Otherwise it stays, with a warning. The same test
  decides whether a reinstall may relink it.
- **PATH** (TM-8, TM-10): on Windows, removes exactly the `path_value` entry from the raw user `Path` and leaves the
  rest byte for byte. On Unix, removes only lines equal to `path_value`, and only when `path_value` is the TM-9 line
  for `dir` and `path_file` is one of `~/.bashrc`, `~/.bash_profile` (bash login file on macOS), `~/.zshrc`, `~/.profile` or `~/.config/fish/config.fish` under the
  current `HOME`.
- **Launcher:** only when the file still carries our marker (ports.md).
- **Without a state file** (installs from 0.1.x), uninstall behaves as it does today and cannot remove an alias or
  PATH entry. It says so.

Other readers (`mothdex logs bundle` manifest, `mothdex version -v`) may show `version`, `profile`, `gui` and
`alias`. They never show `dir` or paths without the home-folder replacement of logs.md §6.

## 5. Tests (owners: Guindaste for installers, T-116 for Go)

- **Installers** (`tests/installer`):
  - the file is written after the install, with no BOM;
  - reinstalling keeps `installed_at`;
  - `--dry-run` writes nothing;
  - uninstall removes only the alias and PATH entry the file names, and leaves a foreign `mdex` in place;
  - the TM-14 hostile state (`alias=../../etc/x`, `path_file=/etc/profile`, a value holding `$(touch /tmp/p)`)
    removes nothing and runs nothing.
- **Go:** a table of state files covering every row of §4, each with its `reason`; a copied state file, a
  group-writable folder and `sudo` are refused; a BOM is accepted.
