# Ports beta: what each system still has to confirm

Owner: Guindaste (installers) · Contract: [ports.md](../contracts/cli/ports.md) section 8 · Report form:
[`ports-beta.yml`](../../.github/ISSUE_TEMPLATE/ports-beta.yml)

There is no hardware in the project (ROADMAP risks). Every cell below is **Unverified** until a community beta report
confirms it on a named system and device (D28 9a). The release notes ship the same labels. A cell turns into a
confirmed fact only through a report, never from research or inference.

| Item | What a report confirms | Batocera | EmuELEC | Recalbox | RetroBat |
|---|---|---|---|---|---|
| B1 | The Port appears, starts the GUI, and the frontend comes back on exit | Unverified | Unverified | n/a (no Port) | Unverified |
| B2 | The GUI takes the display and pad; the `tui` fallback has a terminal (OQ-PRT-1) | Unverified | Unverified | n/a | Unverified |
| B3 | The video driver SDL picks; legibility at 640x480 and 480x320 | Unverified | Unverified | n/a | n/a |
| B4 | Pads map from the embedded database; frontend mapping variables reach the GUI (OQ-PRT-2) | Unverified | Unverified | Unverified | Unverified |
| B5 | The armhf interpreter path and real ARMv6 | Unverified | n/a | Unverified | n/a |
| B6 | `bin_dir` persists across reboots and upgrades; `HOME` is persistent (OQ-PRT-9) | Unverified | Unverified | Unverified | n/a |
| B7 | The installer's `--probe` answer on each image, including the EmuELEC 32-bit userland (OQ-PRT-7) | Unverified | Unverified | Unverified | n/a |
| B8 | A `.sh` Port on an exFAT share (OQ-PRT-5) | Unverified | n/a | n/a | n/a |
| B9 | The refresh after install shows the Port | Unverified | Unverified | n/a | Unverified |
| B10 | Upgrade from a `v1` launcher; uninstall leaves ROMs and config intact | Unverified | Unverified | Unverified | Unverified |

`n/a` means the item does not apply to that system as ports.md scopes it: Recalbox has no Port (OQ-PRT-8 asks whether
11.0 will), and the items that need a frontend, a Linux video stack or a `.sh` Port do not apply to a system that has
none of them. RetroBat is a Windows install with a per-user folder, so the ARM, `HOME` and exFAT questions do not apply.

Everything the installers cannot prove in a container, they print: the eligibility result of `--probe`, the launcher
path, and how to opt out. A tester pastes that output into the report.
