# T-22: M1 acceptance checklist (`mothdex-cli v0.1.0`)

Owner: Ensaio. Sources: ROADMAP M1 (T-10 to T-21 done criteria), PLAN section 2.3 and 2.7, the T-16 catalog.
The Acceptance Criteria note is still empty; the ids below (A1..) replace it until Regente copies them.
Evidence kinds: **CI** (job on the commit), **TEST** (test name), **RUN** (command run by Ensaio), **MAN** (manual run on the
target). `T-20b` marks items that wait for the command layer (PR #46). Windows evidence comes from the CI windows jobs or a
Windows runner, never from the Linux host.

State of this file: prepared at main `de7e263` (+ #41 `f82922a`). Items below are `[x]` only when evidence was seen.

## A. Build and release (T-21)
- [x] A1 GoReleaser snapshot builds 9 targets, archives reproducible, `verify-dist.sh` passes (linux binaries run, arm under qemu). CI: `goreleaser snapshot` green on #31 (`1d2f3ce`).
- [ ] A2 Same on the release commit, with `--skip=sign` removed only in the real release (keyless cosign, SBOM, provenance). CI on the tag; MAN review of the release assets.
- [ ] A3 Binary smoke from the snapshot archive: `mothdex --version` prints version, commit and date matching the snapshot (verify-dist.sh does this on linux; windows amd64 via the `installer (windows)` jobs). T-20b for the command surface.
- [x] A4 `install.sh` in containers with simulated Batocera, EmuELEC and Recalbox filesystems: 160 checks plus a non-root pass. CI: `installer (shellcheck + containers)` green. Windows `install.ps1`: Pester, CI `installer (windows, powershell|pwsh)` green.
- [ ] A5 Installer against the snapshot archive (not the fake release): RUN on a clean container with the snapshot `dist/` served over file or loopback. Not run yet: needs the pinned images and no network; first run after T-20b.

## B. Core packages (T-10 to T-19)
- [x] B1 `pkg/mothdex` contract and retry rule, `internal/config` precedence, `internal/cache` TTL and eviction, `internal/history`: RUN `go test -count=1 ./pkg/... ./internal/config ./internal/cache ./internal/history` on `de7e263`: all ok.
- [x] B2 Downloader risk paths: T-16 catalog, 51 of 52 cases Covered (PR #45 flips 51 and 52). Case 49 waits for T-20b.
- [ ] B3 Race-free suite on main: **blocked by a flaky test** (finding F1 below). CI on `f82922a` failed once in `test (ubuntu-latest)`.

## C. Commands, PLAN 2.3 (all T-20b)
- [ ] C1 `search` (`-p -r --serial -m --page --sort -e`), `info`, `random`, `platforms`, `regions`, `db-info`: TEST per command with the fake API; MAN smoke on linux and windows binaries against fixtures only.
- [ ] C2 `download` (`--link`, `--prefer`, `-o` becomes the absolute root, `-j`, `--no-boxart`), `boxart`, `history` (`--failed`, `-e`), `cache`, `config`, `profile`.
- [ ] C3 `shell`, `tui`, `gui` print the "coming soon" message with the right exit code.
- [ ] C4 Globals `--json` (stable, versioned), `-q`, `--no-color`, `--lang`, `--config`; exit codes 0, 2, 3, 4, 5, 7, 130 each reachable by a test.
- [ ] C5 Case 49: every printed error text and title has no ESC or control byte in human output, and `--json` escapes it. TEST name goes in the catalog when it lands.
- [ ] C6 CSV export formula guard (case 50, already Covered at the history layer) also holds through the command.

## D. Release gates (not tests)
- [ ] D1 Required reviews on each PR, CI green, gitleaks clean on both repos.
- [ ] D2 Bot identity replaces the owner identity before the tag (D14).
- [ ] D3 Tag `v0.1.0` needs the owner OK; Regente tags, never Ensaio.

## E. mcp live-catalog slug charset sample (T-22, M1')
Goal: confirm the `Slug` schema (`string`, 1 to 256 chars, `src/server/schemas.ts`) does not reject real slugs.
Plan: a throwaway script, run once by Ensaio, that calls `/search` for a spread of platforms at the courteous rate
(about 5 req/s, 1 request at a time, generic User-Agent), reads only `slug` fields and prints **counts only**: total slugs,
count outside `[a-z0-9-]`, count of length 0 or above 256, the set of distinct offending character classes (not the slugs).
No API text is printed or stored; no fixture is updated. Needs the owner OK for live calls (project rule: fixtures only), so
it stays PENDING until Regente approves.

## Findings so far
- **F1 (Major, test defect, flaky):** `TestCoverFromCacheThenFetchSingleflightAndNegativeCache` (`internal/app/catalog_test.go`, #41) fails about 1 in 40 runs with `-race` ("2 or 3 fetches for one URL"): it opens the gate as soon as the first fetch starts, so a goroutine that has not entered `Cover` yet starts a second fetch. It also polls with a real `time.Sleep`. Failed CI on `f82922a`. Fix belongs to Forja (a start barrier with all callers inside the singleflight before the gate opens).
