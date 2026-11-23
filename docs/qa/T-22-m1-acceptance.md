# T-22: M1 acceptance checklist (`mothdex-cli v0.1.0`)

Owner: Ensaio. Sources: ROADMAP M1 (T-10 to T-21 done criteria), PLAN section 2.3 and 2.7, the T-16 catalog.
The Acceptance Criteria note is still empty; the ids below (A1..) replace it until Regente copies them.
Evidence kinds: **CI** (job on the commit), **TEST** (test name), **RUN** (command run by Ensaio), **MAN** (manual run on the
target). `T-20b` marks items that wait for the command layer (PR #46). Windows evidence comes from the CI windows jobs or a
Windows runner, never from the Linux host.

State of this file: prepared at main `de7e263`, run at main `ebe6a54` (T-20b #46, #51 and #52 merged). Items below are `[x]` only when evidence was seen.

## A. Build and release (T-21)
- [x] A1 GoReleaser snapshot builds 9 targets, archives reproducible, `verify-dist.sh` passes (linux binaries run, arm under qemu). CI: `goreleaser snapshot` green on #31 (`1d2f3ce`).
- [ ] A2 Same on the release commit, with `--skip=sign` removed only in the real release (keyless cosign, SBOM, provenance). CI on the tag; MAN review of the release assets.
- [x] A3 (linux amd64 only; Windows not verified) Binary smoke from the snapshot archive: `mothdex --version` prints version, commit and date matching the snapshot (RUN on `ebe6a54`: `mothdex 0.0.1-snapshot (commit ebe6a54, 2026-11-21)`, and `--json version` agrees) (verify-dist.sh does this on linux; windows amd64 via the `installer (windows)` jobs). T-20b for the command surface.
- [x] A4 `install.sh` in containers with simulated Batocera, EmuELEC and Recalbox filesystems: 160 checks plus a non-root pass. CI: `installer (shellcheck + containers)` green. Windows `install.ps1`: Pester, CI `installer (windows, powershell|pwsh)` green.
- [x] A5 Installer against the snapshot archive (RUN at snapshot `e47d79b`, offline, curl shim and minimal PATH): installs the linux_amd64 archive, the installed binary is byte-identical to the archive's, a tampered archive is refused, and the binaries run on amd64, armv7 and arm64 (qemu). **Not verified:** the full `verify-dist.sh` (needs SBOMs, syft not available offline), cosign, the Windows binary.

## B. Core packages (T-10 to T-19)
- [x] B1 `pkg/mothdex` contract and retry rule, `internal/config` precedence, `internal/cache` TTL and eviction, `internal/history`: RUN `go test -count=1 ./pkg/... ./internal/config ./internal/cache ./internal/history` on `de7e263`: all ok.
- [x] B2 Downloader risk paths: T-16 catalog, 52 of 52 cases Covered (`grep -c 'Status: Covered'`; case 49 flipped by #46).
- [x] B3 Race-free suite on main: RUN `go test -race -count=1 ./...` on `ebe6a54`: 0 FAIL; `go test -race -count=100 -run TestCoverFromCacheThenFetchSingleflightAndNegativeCache ./internal/app/` ok (F1 fixed by #49).

## C. Commands, PLAN 2.3 (T-20b, run at `ebe6a54`)
Method: TEST = package tests (`go test -race ./...` green). RUN = the real binary (GoReleaser snapshot build, linux amd64) against a
local fixture API on 127.0.0.1 with isolated HOME and XDG dirs; no live API call. Exit codes were read from the process.
- [x] C1 `search` (`-p -r --serial --page-size --page --sort -e`), `info`, `random`, `platforms`, `regions`, `db-info`: RUN, all exit 0 with the expected tables; `search` with a rate-limited answer exits 4, an unknown flag exits 2, an unknown slug exits 3. The documented page-size option is `--page-size` (F4 corrected in T-100). **Not verified:** the same smoke on a Windows binary (no Windows host; the Windows targets build and vet: `GOOS=windows go build`/`go vet` ok).
- [x] C2 (download and boxart live) `config`, `cache`, `history` RUN ok (see above). Live run on `ebe6a54` (snapshot binary, default API base and pool, one homebrew entry, its smallest file, no retries beyond the built-in policy, stop on 429 or non-200): `search` exit 0; `download 5 --link 1 --no-boxart` exit 0, state done, 6538 bytes written = 6538 expected, 1 attempt; the sha256 recorded in the history equals the sha256 of the file on disk (match: true). The file and the search data were deleted afterwards; no API text is kept. `boxart` on that entry exit 3 (`not_found`, "no usable cover"): correct, the API data has no `cover_url` for it. A second entry that has a cover (approved by Regente; its lookup came from the cached search, one cover GET): `boxart 3` exit 0, one JPEG of 8751 bytes saved, deleted afterwards. `-o` becoming the absolute root: TEST `TestRelativeOutputBecomesTheAbsoluteRoot`; `--prefer`, `-j` by TEST only.
- [x] C3 `shell`, `tui`, `gui`: RUN prints "<name> is not available yet." and exits 0. The contract fixes no code for the placeholder (ui.md only says the launcher's code is returned), so 0 is recorded as observed, not as a requirement. Note: `shell` became a real REPL on main after this run (T-41, merged); it was not re-tested here, only `tui` and `gui` still print the placeholder.
- [x] C4 Globals: `--json` envelope has `schema_version: 1` and `meta.cli_version` equals `data.version` (RUN on the release build); `-q`, `--no-color`, `--lang pt_br` (translated text), `--config` with a missing file (allowed by config.md section 1) RUN ok. Exit codes by RUN: 0, 2 (usage, invalid_config, confirmation_required with a piped stdin), 3, 4 (rate_limited, untrusted_url), 130. By TEST only: 5 and 7 (`TestBatchExitCodes`, `TestInterruptedBatchWinsAndPartialBatchHasNoSecondBlock`, `TestEnvelopeCarriesThePartialExitCodeAndWarnings`) because they need a real transfer.
- [x] C5 Case 49: RUN with an entry whose title is `Ev ESC]52;c;...BEL il ESC[2J RLO ZWSP X`: `search` and `info` print no ESC, no BEL and no bidi or zero-width character (checked with `cat -v`); `--json` has no raw ESC and escapes it as `\u001b`. TEST: `TestHostileTextIsStrippedFromHumanOutputButKeptInJSON`, `TestHostileErrorTextIsCleaned`, `TestCase49DownloadAndInfoPaths`. Note: only control and format characters are removed, so the harmless text of an OSC payload (`]52;c;ZXZpbA==`) stays visible. U+202E and U+200B stay raw inside `--json` strings (valid JSON, machine reader).
- [x] C6 CSV guard through the command: RUN `search csv -e out.csv --format csv` writes `'=HYPERLINK(...)` for the title, and `history -e h.csv --format csv` guards title (`'=cmd|...`) and filename (`'+f.bin`). TEST `TestSearchExportCSVGuardsFormulasAndRefusesToOverwrite`.

## D. Release gates (not tests)
- [ ] D1 Required reviews on each PR, CI green, gitleaks clean on both repos.
- [ ] D2 Bot identity replaces the owner identity before the tag (D14).
- [ ] D3 Tag `v0.1.0` needs the owner OK; Regente tags, never Ensaio.

## E. mcp live-catalog slug charset sample (T-22, M1')
Goal: confirm the `Slug` schema (`string`, 1 to 256 chars, `src/server/schemas.ts`) does not reject real slugs.
Plan: a throwaway script, run once by Ensaio, that calls `/search` for a spread of platforms at the courteous rate
(about 5 req/s, 1 request at a time, generic User-Agent), reads only `slug` fields and prints **counts only**: total slugs,
count outside `[a-z0-9-]`, count of length 0 or above 256, the set of distinct offending character classes (not the slugs).
No API text is printed or stored; no fixture is updated. Regente approved it (read-only, 3 s pause, stop on 429 or non-200). Result in the status section below.

## Findings so far
- **F1 (Major, test defect, flaky):** `TestCoverFromCacheThenFetchSingleflightAndNegativeCache` (`internal/app/catalog_test.go`, #41) fails about 1 in 40 runs with `-race` ("2 or 3 fetches for one URL"): it opens the gate as soon as the first fetch starts, so a goroutine that has not entered `Cover` yet starts a second fetch. It also polls with a real `time.Sleep`. Failed CI on `f82922a`. Fix belongs to Forja (a start barrier with all callers inside the singleflight before the gate opens).

- **F3 (Minor, product defect, closed by `4c9b278`, T-20c):** `/dev/null` was treated as a terminal because it is a character device, causing a prompt and exit 130 instead of non-interactive `confirmation_required` (exit 2). The command now uses `term.IsTerminal`; `TestIsTerminalRejectsTheNullDevice` covers `os.DevNull` and a non-file reader.
- **F4 (Suggestion, docs, closed in T-100):** the supported search option is `--page-size`, with no `-m` shorthand. The checklist and canonical PLAN command example now use `--page-size`.
- **F5 (Suggestion, UX, to Forja in T-20c):** `info <unknown>` prints `error: not_found` (the message repeats the code, the hint line is useful). Other errors have a full sentence.
- **F1 status:** fixed by #49; `-race -count=100` ok at `ebe6a54`.
- **F2 status:** Windows cache flake fixed by #51 (CI windows jobs on main are the evidence; not re-run locally).

## M1 acceptance status (Ensaio, at `ebe6a54`)
Met with evidence: A1, A3 (linux), A4, A5 (offline subset), B1 to B3, C1, C3 to C6, E (slug sample: 2400 slugs from 12 of 67 platforms, 0 outside `[a-z0-9-]`, 0 empty or above 256, lengths 7 to 151; counts only).
Partly met: C2 (live download and live cover save ok), A2.
Open or not verified: A2 (needs the tag), full `verify-dist.sh`, cosign, Windows binary smoke, D1 to D3 (release gates, Regente), `--prefer` through the binary.

T-100 update: F3 is closed by `4c9b278`; F4 is corrected in the docs. Other historical observations above remain scoped to the commits on which they were recorded.
