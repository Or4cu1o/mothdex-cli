# ADR-0003: One-line install through a pipe

Status: **Accepted** (owner decision D34, 2026-11-26) · Author: Guindaste · Threat model:
[T-110](../security/T-110-threat-model-beta-adjustments.md), surface 1 · Supersedes the "never pipe the installer"
part of TH-33 in [T-16](../security/T-16-threat-model-downloader.md).

## Context

Until 0.1.x the installers had to be downloaded, read and run, and `curl | sh` or `irm | iex` were forbidden. Beta
testers need one command, and then `mothdex` working in the same terminal where the system allows it.

## Decision

- The installers may be run through a pipe. The commands are in the [README](../../README.md#install), the only place
  that spells the installer URL, so a short domain can replace it later.
- They are **release assets**, served by the `latest/download` path of the GitHub release. Each published installer
  carries the version of its own release and installs that version by default (`scripts/stamp-installers.sh`), so the
  script and the archive rules it knows always match. `install.cmd` fetches the `install.ps1` of its own release.
- They are listed in `checksums.txt`, so they are signed (cosign) and attested like every other asset, and only the
  gated publish job uploads them.
- Download, verify, read and run stays documented for anyone who wants to audit the script first.

## What still protects users

Inside the script, on every path:

- downloads only from the GitHub release of `Or4cu1o/mothdex-cli`, over HTTPS, with redirects to anything else refused;
- the archive SHA-256 is always checked against `checksums.txt` of the same release, before anything runs; the cosign
  signature or GitHub attestation is checked when `cosign` or a logged-in `gh` is present;
- a truncated download runs nothing (TM-1); no program started by the script can read the script from its input (TM-2);
  under `Invoke-Expression` no setting, function or variable of the user changes (TM-3);
- `install.cmd` passes options to PowerShell as data, never as code (TM-4);
- PATH and shortcut changes are per user, only after the installed program runs, and uninstall removes only them
  (TM-7 to TM-13).

None of these checks protects the **script itself**: with a pipe it runs before anything has verified it.

## Residual risk (accepted)

| Ref | Risk |
|---|---|
| R1 | Script authenticity is not checked before it runs. Whoever can change what `latest/download/install.*` serves (owner account, release workflow before publish, GitHub) runs code as the user on every new install, and the checks inside the script then prove nothing. Reduced by immutable releases, the `v*` tag ruleset, release environment reviewers, installers inside the signed and attested `checksums.txt` (after-the-fact audit) and owner 2FA. |
| R2 | TLS interception by a certificate authority the machine trusts (corporate proxy, malware) can swap the script; the pipe adds no check. |
| R3 | A short domain, if added, brings its DNS, registrar and host into the trusted base. It may only redirect to the URL in the README (HTTPS, HSTS, registrar lock) and never through HTTP: the one-line fetch itself (`curl -fsSL`, `irm` on Windows PowerShell 5.1, and the `irm` inside `install.cmd`) does not force HTTPS across redirects. The check inside the script covers only what the script downloads. |
| R4 | Copy-and-paste lures (lookalike URLs, edited commands in issues or chats). One canonical URL in one place only reduces it. |
| R5 | The line run in an elevated PowerShell runs the installer as administrator; it warns and still changes only the user's PATH. On Linux and macOS, piping it to `sudo sh` runs it as root: it installs as root and leaves the user's startup files alone (TM-9). |
| R6 | Without `cosign` or a logged-in `gh` (most users), an installer run (first install or reinstall) is SHA-256 only: transit integrity, not origin. `mothdex upgrade` is to verify the release signature itself (D35, required before 0.2.0), which does not cover the installer runs. |
| R7 | The shortcut conflict check sees only what the installer process sees: aliases and functions of the user's interactive shell are missed, and its PATH may differ. |
| R8 | A malicious release produced by the real workflow passes every check: signatures prove origin, not intent. `mothdex upgrade` would carry it to the installations that run it. |
| R9 | Log bundle redaction is pattern-based: new secret formats or user content can remain in a bundle posted to a public issue; the tester reviews it first. |
| R10 | The daily update check reveals the IP address and timing of use to GitHub; it can be turned off. |
| R11 | Malware running as the same user can rewrite the install folder, state file, startup files and PATH whatever the installer does. |

R8 to R10 belong to the logs and update surfaces of T-110 and are listed here so the accepted set is in one place.

## Notes on the controls

- **TM-3 and parameters.** `Invoke-Expression` binds a top-level `param` block into the caller's scope, where `$Version`
  or `$BinDir` overwrote the user's variables. The script parameters therefore have internal names (`$Mothdex*`) with the
  public names as aliases. The review of the first change (R-139s S2) asked to record those internal variables as left in
  the session; the Windows PowerShell 5.1 and PowerShell 7 tests assert that no `Mothdex*` variable exists after
  `Invoke-Expression`, and they pass.
- **Exit status.** `install.ps1` never calls `exit`, which would close the window of a user who piped it. It sets
  `$LASTEXITCODE`, and a thrown failure also ends `powershell` with status 1. With `powershell -File install.ps1`, a
  failure of the GUI step after the CLI was installed leaves the process status at 0; `install.cmd` ends its command with
  `exit $LASTEXITCODE`, so CMD sees it.
- **TM-15.** The documented CMD line saves `install.cmd` in `%TEMP%`, not in the current folder.
