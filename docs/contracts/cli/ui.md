# Contract: Shell and TUI (E-M2)

Status: draft v1 (T-40) · Owner: Alicerce · Implementer: Vitral (T-41) · Risk: Medium
UX source of truth: [`shell.md`](../../ux/shell.md), [`tui.md`](../../ux/tui.md), [`cli-conventions.md`](../../ux/cli-conventions.md).
This contract adds only what the UX docs leave to engineering: packages, imports, service calls, libraries, tests.
Where it differs from the UX docs, the difference is listed in §5.

## 1. Common rules (both surfaces)

**Packages.** `internal/ui/shell` and `internal/ui/tui` replace today's `doc.go` placeholders.

**Imports.** Each package declares the `app` subset it uses as its own interface, the way `cli.Service` does, so tests can pass fakes. It may import:
- `app`;
- `pkg/mothdex` types;
- `internal/i18n` and `internal/buildinfo`;
- the exported API of `internal/ui/cli` (§2.1).

`internal/ui/cli` never imports `shell` or `tui`. The composition root (`cmd/mothdex/main.go`) injects them as launchers in `cli.Deps`:
```go
Shell func(ctx context.Context, env ShellEnv) int // nil in tests that do not need it
TUI   func(ctx context.Context, env TUIEnv) int
```
`Deps.Shell` and `Deps.TUI` are the only cli-to-UI edges, so the import graph stays acyclic. This extends the import rule in the intro of `app-service.md`.

**Cobra commands.** `shell` and `tui` replace the `soonCmd` stubs; `gui` launches the installed sibling binary (T-112, beta adjustments §3). The Shell and TUI each:
- takes `cobra.NoArgs`;
- sets `Globals.Downloads = true`, opens the session once with `r.open()`, and calls its launcher;
- returns the launcher's exit code through `exitError`, with `silent` set.

Failure cases:
- `--json` on `shell` or `tui` is a `usage` error.
- `tui` without a terminal on both stdin and stdout is a `usage` error: `The TUI needs an interactive terminal.`

The root command with no arguments starts the Shell when stdin and stdout are both terminals. Otherwise it prints help, as it does today.

**Session data the UIs need.** `cli.Session` gains:

| Field | Meaning |
|---|---|
| `DataDir string` | resolved `paths.data`, set by `Open` |
| `NoColor bool` | the final color decision (cli-conventions §4) |
| `UTF8 bool` | whether to use box and symbol glyphs or their ASCII fallback |

UIs do not call `ConfigGet("paths.data")`, which returns `""` when the default is in effect. UI-owned files, written atomically (temp file + rename, mode 0600):
- `<DataDir>/shell_history`
- `<DataDir>/tui_state`, holding the recent queries as JSON `{"recent": [...]}`, at most 5

A write failure logs at warn and the UI keeps going. It is never an error.

**Index semantics.** Shell and TUI share the CLI session through the service:
- `Search` writes the session; `Ref{Index}` resolves against it.
- The TUI shows `#` for parity but acts with `Ref{Slug}` from the rows it holds. A search run in another terminal then cannot retarget a TUI action.
- The Shell passes indexes through, like the CLI does.
- Neither UI reads `internal/session`.

**Interrupts.** Today `main.go` cancels the root context on the first SIGINT. In Shell or TUI mode that would end the whole program, so `cli.Deps` gains:
```go
Interrupts func() (sig <-chan os.Signal, release func())
```
Calling it hands SIGINT to the caller until `release` runs, and the root-cancel goroutine ignores SIGINT meanwhile. SIGTERM keeps cancelling the root.
- The Shell derives one context per command and cancels it on SIGINT (shell §4).
- The TUI gets Ctrl+C as a key in raw mode and calls `Interrupts` only to swallow a stray signal.

**i18n.** New strings go under `ui.shell.*` and `ui.tui.*`. Existing groups are reused:
- `errors.<code>` for message and hint;
- `ui.status.*`, `ui.units.*` and `ui.labels.*`;
- `ui.cli.*` for output shared with commands.

Each new key goes into `en_us.yaml`. Other locales fall back to en_us (Bundle fallback, not verified for nested keys: check in T-41). Command names and flags are never translated.

**Color and glyphs.** One style table per UI, built from `Session.NoColor` and `UTF8`, with the roles of cli-conventions §4.
- With color off, only bold, faint and reverse are used; the TUI marks the cursor row with reverse plus `>`.
- With `UTF8` false, the box frame becomes `+-|` and symbols take their fallbacks.

## 2. Shell (`internal/ui/shell`)

### 2.1 Reuse of the CLI

Q1 from Vitral: the cli package exports a runner. The Shell does not call `Execute` with a faked `Open`: that path re-parses globals per run and closes the session.
```go
type Runner struct{ /* Deps + the open *Session, built by cli */ }
func NewRunner(d Deps, s *Session, interactive bool) *Runner
func (r *Runner) Run(ctx context.Context, args []string) int                     // one command line, same tree and output as the CLI
func (r *Runner) RunSearch(ctx context.Context, q app.SearchQuery) int          // renders like `search`; used by n, p, page, sort, last
func (r *Runner) LastPage() (app.SearchPage, bool)                               // last page shown in this process
func (r *Runner) Commands() []CommandInfo                                        // name, short help, flags: for help and completion
```
- `Run` builds a fresh cobra tree per line, so flag state never leaks between lines. It does not reopen or close the Session.
- Some flags take effect only when the service opens (`compose.go` passes them once, in `EnableDownloads`). Inside the Shell they would be accepted and then silently ignored, so the Runner rejects them with a `usage` error, `not available in the Shell; run the command outside it` (`ui.shell.flag_startup_only`):
  - `--config`, `--lang` (shell §2);
  - `--allow-executable`, `-j`/`--jobs` (T-41, D4).
- `-f` inside the Shell only skips the confirmation (cli-conventions §6). Overwriting follows `download.on_exists`, and the per-job `skipped (exists)` line shows when a file was not overwritten.
- The `shell` and `tui` commands open the service with `Force` and `AllowExecutable` false even when `-f` is given at start-up. Otherwise one `mothdex -f shell` would allow overwriting for every download of the session. `-j` at start-up is honored.
- Per-request `Force`, `AllowExecutable` and `Concurrency` in `DownloadRequest` are a backlog item (F5). That is a downloader-policy change and needs Sentinela first.
- With `interactive` set (stdin is a terminal), `download` uses the inline link chooser of shell §4. Confirmations follow cli-conventions §6, and the chooser renders through the cli printer.
- The chooser and `Runner` live in `internal/ui/cli` because they are CLI output.
- `ShellEnv` is `{Runner *Runner; Session *Session; Stdin io.Reader; Stdout, Stderr io.Writer; IsTerminal bool}`.

### 2.2 Paging, `last` and the prompt

Q2 from Vitral: in-process only, with no new service getter.
- `n`, `p`, `page <n>` and `sort <k>` take `LastPage().Query`, change `Page` or `Sort`, and call `RunSearch`.
- `last` calls `RunSearch` with the same query. The cache makes that instant, and it is never a new network call while the cache is fresh.
- After a Shell restart there is no in-process page. `n`, `p`, `page`, `sort` and `last` then print `ui.shell.no_page`: `No search yet in this shell. Run search first.`
- Indexes (`download 1`) still resolve through the stored session.
- The prompt is `mothdex[<platform|all>:<n>]> ` built from `LastPage()`. With no page it is `mothdex> `.

### 2.3 Service calls

| Shell feature | Call |
|---|---|
| banner | `DBInfo` (offline: `offline`), `ActiveProfile` |
| every command | `Runner.Run` → the CLI command's own calls |
| completion `-p`, `-r` | `Platforms`, `Regions`: lazy on first Tab, kept in memory for the process; failure gives no candidates |
| completion config keys and values | `ConfigList`; enum values come from gap F1, and are static until it lands |
| completion `cache clear` | `CacheStats().Namespaces` |
| completion `--prefer` values | links of the last `info` the Shell rendered (in-memory, best effort) |

### 2.4 Readline library

Q4 from Vitral: keep **`github.com/peterh/liner` v1.2.3**, as PLAN §9 says.
- Rechecked 2026-11-21 with `gh api`: tag v1.2.3 is the latest commit (2026-10-08). The license is MIT, and the only dependencies are `mattn/go-runewidth` and `x/sys`, with no cgo (T1a).
- Its maintenance is low-churn with a single maintainer, which is acceptable for a line editor. The fallback, if a blocker shows up, is `github.com/ergochat/readline` v0.1.3 (MIT), behind the same small `lineEditor` interface the Shell uses, so tests do not need a terminal.
- Settings: `SetCtrlCAborts(true)`, `SetTabCompletionStyle(liner.TabPrints)` and `SetWordCompleter`.
- History is read from and written to `shell_history` with liner's `ReadHistory`/`WriteHistory`, after the filters of shell §5. Ctrl+R reverse search is assumed from liner's docs and not verified.
- No vi mode (T1a open question 1): none is needed.

### 2.5 Exit

- `exit`, `quit` and Ctrl+D exit with 0.
- Piped input exits with the last command's code.
- Ctrl+C never exits the Shell (UX-sync, shell §4): it cancels the running command or clears the line and shows
  `Type exit or press Ctrl+D to leave.` Only `exit`, `quit` and Ctrl+D leave.
- On exit the launcher returns, and cli closes the Session (`Service.Close` drains or cancels the jobs).

## 3. TUI (`internal/ui/tui`)

**Stack.** Pinned in go.mod by T-41, and rechecked 2026-11-21 as the latest releases:

| Module | Version |
|---|---|
| `charm.land/bubbletea/v2` | v2.1.2 |
| `charm.land/bubbles/v2` | v2.2.2 |
| `charm.land/lipgloss/v2` | v2.1.1 |

- These are v2 import paths only; never `github.com/charmbracelet/...` v1.
- `ultraviolet` arrives at a pseudo-version, and go.sum pins it (T1a risk).
- The 386 and armv7 cross-builds are unverified: the CI build matrix must prove them in the T-41 PR.

**Layout.** One root model owns the screen stack, the frame, the toast and the queue summary. Each screen is its own model:

| File | Contents |
|---|---|
| `search.go` | Search screen |
| `results.go` | Results screen |
| `entry.go` | Entry detail and link chooser |
| `queue.go` | Download queue |
| `history.go` | History |
| `settings.go` | Settings |
| `keys.go` | key map and reducer |
| `layout.go` | size tiers and column drop |
| `styles.go` | style table |

- Service calls run as `tea.Cmd` with a context per screen action. `Esc` cancels that context.
- One `Events(ctx)` subscription lives for the whole TUI and feeds the queue and the title-bar summary.

**Service calls.**

| Screen / action | Call |
|---|---|
| Search: run | `Search` (writes the session) |
| Search: pickers | `Platforms`, `Regions` (lazy; on failure, free text) |
| Search: Random | `Random{Filters, Count: 10}` |
| Search: footer | `DBInfo`, `ActiveProfile` |
| Results: page or sort | `Search` with the page or sort changed |
| Results: `d` | `Enqueue` with `Ref{Slug}` and `LinkSelector{Prefer}` |
| Entry detail | `Entry`, then `Links(ref, {Prefer})` (re-run on a `P` change) |
| Entry: `d` / `D` | `Enqueue` with `LinkSelector{Index}` for the highlighted link (`d`) or `{Prefer}` for the best link (`D`) |
| Entry: `c` | `SaveBoxart(ref, "")` (§5 D1) |
| Entry: already downloaded | the in-memory history list (below), matched on slug and link index |
| Queue | `Jobs()` at open, then `Events`; `x` is `Cancel(id)`; `X` cancels each active job |
| Queue: `r` | `Enqueue{Ref{Slug}, LinkSelector{Index: job.LinkIndex}}` |
| History | `History{Failed, Limit: 0}`, loaded when the screen opens and after each terminal event |
| History: paging, `/` filter, `d` re-queue | client-side, over the loaded list |
| History: `e` | `ExportTarget`, `WriteExport`, `ExportHistory` (app export API) |
| Settings | `ConfigList`, `ConfigSet`, `ConfigReset`, `CacheStats`, `CacheClear()` |
| Quit with active jobs | after `y`: `Cancel` each job, then return; cli closes the Session |

**Preferences.** `Prefer` is the `download.prefer` baseline from `Session.Prefer`. The `P` overlay replaces it for this TUI run only.

**Settings that apply now.** Keys the TUI itself consumes take effect at once, because the TUI re-reads them after a successful `ConfigSet`:
- `interface.page_size`
- `interface.confirm_downloads`
- `download.prefer.*`
- `download.boxart`

Every other key shows `applies on next start`, because the running service keeps its startup config (backlog item F3).

**Small terminals.** `layout.go` implements tui §9:
- `tier(w, h)` returns one of `full`, `narrow`, `short` or `tooSmall`.
- `columns(tier, w)` follows the CLI drop order.
- The cursor is kept by slug or job ID, not by row.

## 4. Test plan (testing rule: pure rules plus a few model tests, no golden snapshots, no real clock)

**Pure rules** (table-driven, one parametrized test each):

| Package | Rule | Cases |
|---|---|---|
| shell | index args | `1 3 5`, `2-4`, `4-2`, `0`, `a`, a range past the page; expansion is capped at 100 |
| shell | history filter | leading space, `config set x.token`, consecutive duplicates, the 1000-line cap |
| shell | did-you-mean | edit distance ≤ 2 |
| shell | completion candidates | position → candidates with fake sources; a source error gives an empty list |
| shell | prompt text | no page, `platform:n`, no color |
| tui | key reducer | `(screen, focus, key) → action`: letters inside a text field go to the field; `Q`, `H`, `S` are blocked while a field has focus; `q` quits only on the root screen |
| tui | layout | `tier`, `columns` |
| tui | quit decision | no active jobs versus active jobs |
| tui | marks | marks per page; cleared by a new search |
| tui | sort | sort cycle |

**Model tests** (a handful in `tui`, fake service, injected clock, assertions on model state and never on full frames):
- Search → Results with a fake page, and the empty and error states with `r` retry;
- an `Events` sequence updates queue rows and the title summary, and the last terminal event shows the summary toast;
- a resize to 54x14 gives `tooSmall`, and resizing back keeps the screen and cursor;
- `Esc` during loading cancels the context the fake sees.

**Runner tests** (in `cli`): `RunSearch` updates `LastPage`, `--lang` is rejected inside the Shell, and two lines do not share flag state.

There is no test against a real terminal or real liner: the `lineEditor` interface is faked.

## 5. Gaps and deviations

Items for Forja (app layer). None of them blocks T-41.

| Item | Need | Interim in T-41 |
|---|---|---|
| F1 | Config schema metadata for completion and Settings, through `ConfigSchema() []SettingSpec{Key, Kind, Enum []string, Min, Max string, Env string}` (`schema.go` already has kind, range and enum) | static table in the UI for the curated keys |
| F2 | `SearchPage.Dropped` (SLUG-RULE, already assigned) | show the warning once the field exists |
| F3 (backlog) | live reload of config in the running service after `ConfigSet` | `applies on next start` label |
| F5 (backlog) | per-request `Force`, `AllowExecutable` and `Concurrency` in `DownloadRequest` (Sentinela review first) | the Shell rejects the flags (§2.1) |
| F4 (backlog) | `HistoryFilter.Slug` / `Offset`, if `history.jsonl` grows large | load all records, filter client-side |

Items for the cli layer, done in T-41 (Vitral, coordinated with Forja because they own `internal/ui/cli`):
- `Runner` (§2.1);
- the inline link chooser;
- the new `Session` fields;
- the `Shell`, `TUI` and `Interrupts` fields in `Deps`, with their wiring in `main.go`.

Deviations from the UX docs, to be confirmed by Bússola via Regente:
- **D1.** TUI `c` saves the cover with `SaveBoxart` into the download destination and toasts that path. The UX doc wants the cache path, but the cache layout is not exposed to UIs.
- **D2.** Shell `help` text is the cobra help (English, the same as `--help`) until command help is translated. The UX doc names i18n keys `help.<cmd>`.
- **D3.** Shell `n`, `p` and `last` after a restart print `ui.shell.no_page` (§2.2). Indexes keep working.
- **D4.** Inside the Shell, `--allow-executable` and `-j` are a usage error and `-f` only skips prompts; start-up `-f` is not applied to the session (§2.1).

## GUI command launcher (T-112)

`mothdex gui` executes `mothdex-gui` (`mothdex-gui.exe` on Windows) from the CLI executable directory, with inherited input/output and context cancellation. It forwards `--config`, `--profile` and `--lang`, and preserves the child exit code. It does not open an app session in the parent. If absent, it returns `usage` with a hint to reinstall without `-NoGui` / `--no-gui` or run `mothdex tui`.
