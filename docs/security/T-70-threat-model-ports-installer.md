# T-70: Threat model, installer part of the frontend Ports contract

Status: v1 · Author: Sentinela · Date: 2026-11-22 · Risk: **High** · Consumers: Guindaste (PRT-G4, installers),
Vitral (PRT-G3, `--probe` and diagnostics), Alicerce (ports.md) · Closes gap PRT-G7 of
[ports.md](../contracts/cli/ports.md) §10.
Gate: Guindaste codes PRT-G4 against the constraints in §5. Each one is either met or rejected by Regente.

## 0. Scope, method, limits

**In scope:** what ports.md (PR 82 at `6c324e1`) makes run on user devices:
- the D22 default flip (§5.1);
- GUI archive download, verification and extraction (§5.2 steps 1-3);
- `mothdex-gui --probe` run by the installer, as root on appliances (§5.2);
- the `v2` launcher templates and the environment they pass on (§2.2, §2.3);
- `SDL2.dll` placement and loading on Windows (§3, §5.3);
- upgrade, `--no-gui` and uninstall (§5.5, §5.6);
- `mothdex-licenses/` (§6);
- the post-install refresh (§5.5);
- the GUI diagnostics line and the beta report (§4, §8).

**Out of scope:**
- the release pipeline and signing (reviewed in cli#62 and T-24);
- the GUI's own handling of API data (covers, cli#66; SDL buffers, cli#69);
- the downloader (T-16).

**Method:** STRIDE over the install and launch flows. I read ports.md together with the current `install.sh` and
`install.ps1` on `main`: `valid_path`, `dir_safe`, `fat_exception_ok`, `can_exec_in`, `download_and_verify`,
`install_launcher`, `Test-LauncherPath`, `Test-FolderTrusted`, `Expand-Binary` and `Install-Launcher`.

**Tools:** manual review and `grep` over `gh api` reads. No scanners apply: there is no implementation yet.

**Not verified:**
- real devices;
- the environment frontends pass to Ports (OQ-PRT-1);
- whether Batocera exposes `/userdata/system` over SMB by default;
- the inherited ACL of `C:\RetroBat` (as in T-16 TH-31).

This document does not claim the design is free of vulnerabilities.

## 1. Key facts that shape the model

- F1. Batocera, EmuELEC and Recalbox run EmulationStation and every Port as **root**. Whatever the launcher starts
  runs as root, and the installer usually runs as root too (over SSH).
- F2. `install.sh` creates `TMP` with `mktemp -d "${TMPDIR:-/tmp}/..."`, so the directory is `0700` and owned by the
  installer's user. Its parent is whatever `TMPDIR` says. The script checks `bin_dir` and the launcher dir with
  `dir_safe` and `can_exec_in`, but it does not check `TMP`.
- F3. Hardened desktops commonly mount `/tmp` `noexec`. Whether appliance images do is not verified.
- F4. SDL2 reads `SDL_DYNAMIC_API` at start-up. When it is set, SDL loads the shared library it names in place of its
  own entry points. The dynamic loader also honours `LD_PRELOAD` and `LD_LIBRARY_PATH`. Each of these runs code from
  the environment before `--probe` can decide anything.
- F5. Windows `cmd` expands `%*` before it parses the line, so `&`, `|`, `>` and `^` inside the arguments are parsed
  again as syntax. `setlocal DisableDelayedExpansion` does not prevent this.
- F6. `LoadLibraryEx` with `LOAD_LIBRARY_SEARCH_APPLICATION_DIR|SYSTEM32` restricts the load of `SDL2.dll` and its
  static imports. Libraries that SDL itself loads later with plain `LoadLibrary`, such as `vulkan-1.dll`,
  `opengl32.dll`, XInput, `hid.dll` and `setupapi.dll`, still use the process default search order. That order
  includes `PATH`, unless the process calls `SetDefaultDllDirectories`.
- F7. Today `Install-Launcher` in `install.ps1` writes `Mothdex.bat` without a trust check on
  `<RetroBat>\roms\windows`. `install.sh` does refuse an untrusted launcher dir.
- F8. `checksums.txt`, which cosign signs when it is available, lists every release asset. It is therefore the only
  trustworthy answer to "does this release have a GUI archive for this arch?". A failed download is not.

## 2. Assets

- A1: root, or the RetroBat user, on the device.
- A2: integrity of `bin_dir` (`mothdex`, `mothdex-gui`, `SDL2.dll`, `mothdex-licenses/`).
- A3: integrity of the launcher folder: the frontend runs every file in it.
- A4: the user's ROMs, config and data, which uninstall and upgrade must not touch.
- A5: tester privacy in public beta reports.

## 3. Actors and trust boundaries

- **Actors:**
  - X1: a network attacker without the signing key (TLS and the checksum hold).
  - X2: a LAN or SMB client of an appliance share.
  - X3: another local user (Windows multi-user, or Linux with a shared `TMPDIR`).
  - X4: a compromised release pipeline. This is residual; signing mitigates it.
  - X5: the frontend, trusted, but its environment is unknown (OQ-PRT-1).
- **Boundaries:**
  - B1: the release download, from network to `TMP`.
  - B2: `TMP` to the probe exec, which runs as root.
  - B3: `TMP` to `bin_dir` and to the launcher dir.
  - B4: the frontend's environment and arguments reaching the launcher, then the GUI or TUI.
  - B5: the GUI process loading SDL2 and its plug-in libraries.

## 4. Threats

| Id | Threat (STRIDE) | L | I | Status in ports.md v1 |
|---|---|---|---|---|
| PT-01 | Probe executed as root inherits `LD_PRELOAD`, `LD_LIBRARY_PATH` or `SDL_DYNAMIC_API` from the installer shell (for example `sudo -E`, or a root shell that sourced a user profile). That turns a user-level foothold into root before eligibility is decided (E) | Low | High | not covered → IC-1 |
| PT-02 | Probe exec from `TMP` whose parent comes from `TMPDIR`. If that parent is writable by others and not sticky, X3 renames the `mothdex-install.*` dir away between verify and exec and plants a binary, which root then runs (E). With `/tmp` noexec, the exec fails and the installer reports "not eligible", so the GUI is silently dropped (D) | Low | High | not covered → IC-2 |
| PT-03 | "No GUI archive for this arch" inferred from a failed download (X1 drop, or a 404 from a mis-uploaded release), so the install degrades to CLI-only or keeps the launcher opt-in. The result differs from what the signed release says (T, D) | Low | Low | ambiguous → IC-3 |
| PT-04 | GUI archive extraction: more members than the CLI archive (`mothdex-gui`, `SDL2.dll`, `THIRD_PARTY/`). A whole-directory extract (`tar -x` of a dir, `ZipFile.ExtractToDirectory`) admits `..`, absolute names, symlink or hardlink members, or extra executables that land in `bin_dir` as root (T, E; needs X4 or a packaging bug) | Low | High | not covered → IC-4 |
| PT-05 | `mothdex-licenses/` copy or uninstall follows a symlink planted in `bin_dir`: the copy writes as root outside `bin_dir`, and a recursive delete of the link with a trailing slash deletes the target tree (T, D) | Low | Med | not covered → IC-5 |
| PT-06 | Launcher argument forwarding. In `Mothdex.bat`, `%*` is re-parsed by `cmd` (F5), so an argument carrying `&` runs a command. In `Mothdex.sh`, `"$@"` is safe for the shell, but GUI and TUI flags become reachable from the frontend. A Port needs no arguments (E, T) | Low | High | template forwards args → IC-6 |
| PT-07 | Windows launcher dir not trust-checked (F7). With the D22 flip, `Mothdex.bat` is written by default into a folder that other local users may be able to modify (inherited ACL under `C:\RetroBat`, not verified). X3 replaces it, and it then runs as whoever starts RetroBat (E) | Low | High | pre-existing, amplified by the flip → IC-7 |
| PT-08 | DLL search hijack of the libraries SDL loads at run time (F6), through a `PATH` entry writable by another user (E) | Low | High | not covered → IC-8 (Vitral) |
| PT-09 | Windows probe failure "reported as an error, the CLI install still completes", but the contract does not say whether `mothdex-gui.exe` and `SDL2.dll` are installed anyway. The launcher would then prefer a GUI known not to start (D) | Med | Low | ambiguous → IC-9 |
| PT-10 | Probe output echoed by the installer: `dlerror` text, control characters, unbounded length. A hung probe (a broken lib blocking in a constructor) hangs the installer (T, D) | Low | Low | → IC-10 |
| PT-11 | Post-install refresh written in shell or PowerShell (new code; today only the Go side has TH-22's rules). A proxy variable, a redirect, `localhost` resolved by DNS, or no timeout, sends the request elsewhere or hangs (S, D) | Low | Low | not covered → IC-11 |
| PT-12 | The default-on launcher adds a root-run entry point that parses network data (covers, titles) through the GUI. On appliances, X2 can already write `.sh` files into the Ports folder over SMB, so the launcher adds no new capability for X2. It does add a trusted-looking name to replace, and a larger root-run attack surface for every user who never asked for a GUI (E) | Low | High | residual; D22 kept with IC-7 and IC-12 |
| PT-13 | The diagnostics line and the installer output pasted into public beta issues leak user names and home paths (`C:\Users\<name>\AppData\Local\...`, `/home/<name>`) (I) | Med | Low | → IC-13 |
| PT-14 | Upgrade, uninstall or `--no-gui` removes or rewrites more than the installer wrote: a user-edited marked launcher, a foreign `SDL2.dll`, ROMs or config (D) | Low | Med | covered (marker rule, fixed names, §5.6); keep |
| PT-15 | Launcher prefers `<bin_dir>/mothdex-gui` whenever it is executable, so a file planted there wins even after `--no-gui`. On the TH-31 FAT exception, every file is executable (E) | Low | High | same trust as `mothdex` itself (TH-31 warning); residual |

## 5. Installer constraints (coding rules for PRT-G4 and PRT-G3)

Severity follows risk-process. On a High item, a Minor constraint is required unless Regente rejects it.

| Id | Sev | Constraint | Owner | Threat |
|---|---|---|---|---|
| IC-1 | Minor | Run the probe with a scrubbed environment: `env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin "$bin" --probe` on POSIX. On Windows, start the process with an environment holding only `SystemRoot`, `PATH` set to `%SystemRoot%\System32` and `TEMP`. Never pass `LD_*`, `SDL_*` or `MOTHDEX_*`. The probe only tests the system soname path, the one ports.md §3 supports, so nothing is lost | Guindaste | PT-01 |
| IC-2 | Minor | Do not run the probe from `TMP`. Copy the verified binary to the staging name in `bin_dir` (`.mothdex-gui.new.$$`), which `dir_trusted` and `can_exec_in` have already proven, and run the probe there. Then rename it on success, or delete it on failure. Tell an exec failure (126, 127, `ENOEXEC`) apart from a probe "no" (exit 1): an exec failure is an installer error that is reported, never a silent "not eligible" | Guindaste | PT-02 |
| IC-3 | Minor | Decide whether a GUI archive exists only from the verified `checksums.txt`. Listed but not downloadable, or failing verification: abort the whole install, as ports.md §5.2 step 2 says. Not listed: skip the GUI with a note. The D22 default for that release follows the same test | Guindaste | PT-03 |
| IC-4 | Minor | Extract by exact member name only. POSIX: `mothdex-gui` and `THIRD_PARTY/<name>`. Windows: `mothdex-gui.exe`, `SDL2.dll` and `THIRD_PARTY/<name>`. `<name>` must match `^[A-Za-z0-9._-]+$`. After extraction, each file must be a regular file (not a link, not a dir), else abort. Never use a directory extract (`ExtractToDirectory`, `tar -x` without a member list) | Guindaste | PT-04 |
| IC-5 | Minor | `mothdex-licenses/`: refuse it if it is a symlink or reparse point. Write it as a temp dir plus rename, files `0644` and dir `0755`. Uninstall removes only the file names it wrote, then `rmdir`. Never delete recursively through a path with a trailing slash, and never follow a link | Guindaste | PT-05 |
| IC-6 | Minor | Drop argument forwarding from both templates (no `%*`, no `"$@"`). If a frontend turns out to need arguments (OQ-PRT-1), add them back in `.sh` only, with the GUI and TUI rejecting unknown positional arguments. Requires a ports.md §2.2 template change (Alicerce) | Alicerce, Guindaste | PT-06 |
| IC-7 | Minor | Give `install.ps1` the same rule `install.sh` already has: write `Mothdex.bat` only when `<RetroBat>\roms\windows` passes `Test-FolderTrusted` (owner and no write ACE for Users or Authenticated Users), else skip it with a warning. Never rewrite the RetroBat folder ACL. This is the condition for flipping the default on Windows | Guindaste | PT-07, PT-12 |
| IC-8 | Minor | `mothdex-gui.exe` calls `SetDefaultDllDirectories(LOAD_LIBRARY_SEARCH_APPLICATION_DIR|LOAD_LIBRARY_SEARCH_SYSTEM32)` before it loads `SDL2.dll`. A Windows spike case plants a garbage `vulkan-1.dll` (or another library SDL loads late) on `PATH` and expects it to be ignored | Vitral (via Regente) | PT-08 |
| IC-9 | Minor | Windows probe failure: do not install `mothdex-gui.exe` or `SDL2.dll` (the same rule as Linux "not eligible"), report the error, and finish the CLI install. No launcher is written, and a marked one is removed (D32, T-88; this replaced "the launcher then runs `tui`") | Alicerce (§5.2 wording), Guindaste | PT-09 |
| IC-10 | Suggestion | Print the probe's reason with control characters stripped and cut to 200 bytes. Bound the probe with `timeout 20` when available (POSIX; busybox `timeout` included), and with `WaitForExit(20000)` on Windows. A timeout means not eligible, with a note | Guindaste | PT-10 |
| IC-11 | Minor | The installer refresh follows TH-22. Use literal `http://127.0.0.1:1234/reloadgames`, `curl --noproxy '*' --max-redirs 0 --max-time 3 -o /dev/null`, `Invoke-WebRequest -NoProxy -MaximumRedirection 0 -TimeoutSec 3` on Windows. Never wget, never fatal, and only on profiles marked `http_get` ✅ | Guindaste | PT-11 |
| IC-12 | Suggestion | With the flip, the installer prints the launcher path and how to opt out (`--no-launcher`, `-NoLauncher`) on every run that writes or rewrites the launcher. The README says an upgrade recreates a deleted launcher unless `--no-launcher` is passed | Guindaste | PT-12 |
| IC-13 | Suggestion | The beta template (§8) asks testers to replace their user name in pasted paths. The diagnostics line logs no paths, and also reports `LD_PRELOAD` and `SDL_DYNAMIC_API` as names only (set or unset) | Guindaste, Vitral | PT-13, PT-01 |

**Tests** (added to ports.md §11, fake trees and no hardware):
- a probe run with `LD_PRELOAD` set in the installer environment does not see it;
- `/tmp` noexec still installs the GUI, because the probe runs in `bin_dir`;
- a probe exec failure is reported, not taken as "not eligible";
- an archive with an extra member, a symlink member or `../` aborts;
- a `mothdex-licenses` symlink is refused;
- `%*` and `"$@"` are absent from both templates;
- `install.ps1` skips the launcher in a folder where Users can write;
- the refresh ignores `http_proxy`.

## 6. D22 default flip: verdict

**No objection to on by default**, provided IC-7 holds on Windows and IC-12 holds everywhere. On appliances, an SMB
client can already drop any Port (PT-12), so the default launcher adds no new capability for X2. The real cost is
that more users run the GUI as root. That cost is bounded by the existing decode and buffer limits (cli#66, cli#69)
and is recorded as residual R1. No new owner decision is needed.

## 7. Residual risk

- R1: on Batocera, EmuELEC and Recalbox, the GUI runs as root and parses network data. The platform offers no
  privilege drop.
- R2: the frontend's environment is trusted unchanged (§2.3). If a frontend sets `LD_LIBRARY_PATH` to a writable
  share directory, every Port is exposed, ours included. The beta diagnostics (IC-13) make that visible.
- R3: on the TH-31 FAT exception, `bin_dir` and the launcher are writable by any SMB client. The installer warns but
  cannot prevent it (PT-15).
- R4: cosign is optional on devices, so without it only SHA-256 over TLS holds (T-16 TH-33). `--version` can install
  an older signed release on purpose.
- R5: a compromised release pipeline (X4) ships whatever it signs. IC-4 only limits what lands outside the expected
  files.
