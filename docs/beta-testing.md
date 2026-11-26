# Beta testing Mothdex

Thank you for helping. The command-line client, the shell and the terminal UI are tested in CI. The graphical frontend
(`mothdex-gui`) and the Ports launcher have **not been tried on real devices yet**, so your report is the most useful
thing you can give the project. One short report per system and device is plenty.

## What to test per system

On every system, first run `mothdex --version` and `mothdex-gui --probe` (GUI systems), and keep the output for the form.

| System | What to try |
| --- | --- |
| Batocera | Install; the **Mothdex** entry shows in the Ports menu (restart or update the game list if not); GUI starts from it with the pad; search and a download; uninstall. |
| Recalbox | Install; there is no Ports system, so run `mothdex-gui` or `mothdex tui` over SSH; the pad in the GUI; search and a download. |
| EmuELEC | Install; **Mothdex** shows in the ports scripts menu; the GUI starts with the pad; search and a download; uninstall. |
| Lakka | Install over SSH (no launcher on Lakka); `mothdex` search and a download. |
| RetroBat | Install with `install.ps1` (RetroBat is detected, or pass its path); **Mothdex** shows in the Windows system; GUI, pad, search, download; uninstall. |
| EmuDeck | Install; run `mothdex-gui` from Desktop Mode or `mothdex tui` in a terminal; Steam Deck controls; search and a download. |
| RetroDECK | Install; same as EmuDeck. |
| Linux | Install with the `generic` profile; `mothdex`, `mothdex tui`, `mothdex-gui` (on X11 and Wayland if you can); uninstall. |
| Windows | Install with `install.ps1`; `mothdex`, `mothdex-gui`; search and a download; uninstall. |
| macOS | Install; `mothdex` and `mothdex tui` (there is no GUI build for macOS yet); search and a download. |

For each system note: installed, launcher visible in the menu, GUI starts, controller works, search, download,
uninstall. These are the items of the [install report form](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=device_report.yml).

## Install from the release tag

Fetch the installer from the tag you are testing, so the installer and the release match (use the version the
announcement names; `v0.1.1` is only an example). A pre-release tag needs `--version` because the installer skips
pre-releases by default.

```sh
# Linux, macOS, Batocera, Recalbox, EmuELEC, Lakka, EmuDeck, RetroDECK (over SSH or a terminal)
curl -fsSLO https://raw.githubusercontent.com/Or4cu1o/mothdex-cli/v0.1.1/install.sh
less install.sh                      # read it first
sh install.sh --version v0.1.1 --dry-run    # downloads and verifies, changes nothing
sh install.sh --version v0.1.1
```

```powershell
# Windows and RetroBat
Invoke-WebRequest https://raw.githubusercontent.com/Or4cu1o/mothdex-cli/v0.1.1/install.ps1 -OutFile install.ps1
Get-Content install.ps1              # read it first
.\install.ps1 -Version v0.1.1 -DryRun
.\install.ps1 -Version v0.1.1
```

The installer picks the profile for your system. To force one, add `--profile batocera` (or `recalbox`, `emuelec`,
`lakka`, `emudeck`, `retrodeck`, `generic`), and `--no-gui` to install the command-line client only. To remove
everything it installed, run it again with `--uninstall`. Details: [docs/release.md](release.md#installing).

## How to send an error

Every error shows a code and a 6-character **ref**, and names the log file:

```text
error: Cannot reach the Mothdex server.
  Check your connection.
  code: network · ref: 7F3K2Q
  log: ~/.local/state/mothdex/logs/latest.log
```

1. Note the ref, and what you were doing.
2. Make the bundle. `mdex` is the short name of `mothdex`; use either.

   ```sh
   mdex logs bundle              # prints the path of a zip under the log folder
   mdex logs bundle -o .         # or write it to the current folder
   ```

   In the graphical frontend, use **Export logs** on the crash screen or in **Settings › About** (it can be
   reached with the gamepad). It says where the zip is.
3. Review the zip before sharing it, because an issue is public. It holds your search terms and file names.
   Tokens, the API address and your home folder are removed.
4. Open a [bug report](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=bug.yml), paste the **ref**,
   fill in the version (`mothdex --version`) and your system, and attach the zip.

To look yourself first: `mdex logs show --ref 7F3K2Q` prints the lines around that error, `mdex logs last` the
previous run, `mdex logs list` every run, and `mdex logs open` opens the folder. `mdex help logs` lists the files.

## How to report

Pick the form by what you tested:

- **The GUI, or the Port on a frontend** (Batocera, EmuELEC, RetroBat and similar): the
  [Ports beta report](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=ports-beta.yml).
- **Everything else** (the CLI, the shell, the TUI, the installer, on any system): the
  [install report](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=device_report.yml).

One report per system and device. Then:

1. Tick or answer each item of the form.
2. Paste the install command and the `mothdex-gui --probe` output.
3. **Replace your user name** in every path before pasting (`/home/USER`, `C:\Users\USER`).

Found a bug that is not about a device? Use the
[bug report form](https://github.com/Or4cu1o/mothdex-cli/issues/new?template=bug.yml). Questions go to
[Discussions](https://github.com/Or4cu1o/mothdex-cli/discussions/categories/q-a). The MCP server has its own
[issue forms](https://github.com/Or4cu1o/mothdex-mcp/issues/new/choose).

## Beta channel and rollback

Run `mdex config set update.channel beta` to include prereleases in version checks and upgrades, or use
`mdex upgrade --channel beta` for one invocation. To return to stable discovery, run
`mdex config set update.channel stable`. This does not downgrade an installed beta automatically.

To go back to a known release, run `mdex upgrade --version X.Y.Z` and confirm the downgrade (or add `-f`). The exact
release is downloaded and verified again; neither the local cache nor a previous backup supplies its binaries.
A failed post-update version check restores the previous binaries automatically. Exit and reopen all Mothdex
interfaces after a successful change. For a failed rollback, use the reinstall command shown in the error hint.
