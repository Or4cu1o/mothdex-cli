# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- T-64: when the catalog size is already above 500 MiB and was confirmed, a `Content-Length` over 2x that size asks for
  confirmation again (`confirmation_required`); `-f` still skips it and the exact-length pin across retries stays
  (downloader.md 3.2 step 6).
- T-24: `release.yml` `publish` checks that the tag still resolves to the commit that was built (annotated tags followed)
  and verifies attestations with `--source-digest` and `--deny-self-hosted-runners`, as mothdex-mcp does; the tag check is the
  last step before the release is created, and `publish` refuses to upload files that `checksums.txt` does not list.

### Added

- PR-76 review rounds (Crivo, Bússola): a page answer that arrives late is taken only if its page is still the one next to the window, never
  drops the rows that hold the focus, and the focus is always clamped (it used to panic and leave the focus outside the list); B on a wait
  cancels the call itself (`context`); a cover that failed for a reason that may pass (network, timeout) is asked again after 30 s while a
  missing one is not; the row size follows the first listed link like the service's own sort; query ids are sanitized. UX: the hint bar fits
  in every language (drops L/R, then the Start word where Start is the menu, then Y, then X; A and B stay), compact keeps the Start word on
  Search and the multi pickers, finished jobs show their bytes alone in Details, the Entry shows the platform name (the list is loaded once),
  a transferring Queue row is two lines, failed rows keep only the message, the badge says `1 queued`, the Queue's state texts use the
  ellipsis (`ui.gui.queue.*`), an error in place is marked with a `!`, and the Downloaded mark has a gap before its word.
- T-55 (GUI PR 2, Medium): browse and download with the pad. Search with the virtual keyboard (abc, àé and, for Russian or Cyrillic text,
  абв layouts, an 80-character limit with a counter, cursor with L and R, a held B that deletes and only leaves at an empty field, the
  physical keyboard through `SDL_TEXTINPUT`), the Platform and Region pickers (multi-select from Search, single-select from Home ›
  Platforms), Results as one continuous list over service pages of 25 (next page in the last five rows, at most 200 rows, earlier pages come
  back from the service cache), the cover panel (asked 250 ms after the focus settles, decoded one at a time with `app.DecodeCover`,
  scaled to 512 px, a memory cache of 50, a texture cache of 48 in the backend, a placeholder when there is none), Entry with the link
  chooser (unavailable links skipped, Why line, L/R to the neighbouring result), downloads from Results and Entry with the
  already-downloaded and over-500-MB confirmations, `download.boxart` after each finished job, and the Queue (active, queued and
  finished jobs with the Warming counter, the action sheet by state, details, Cancel all and Hide finished from the Start menu, a summary
  toast, Quit when finished now opens the Queue). Errors and empty states are drawn in place with Retry, using the GUI hints of
  docs/ux/gui.md section 6. Recent searches are saved to `<paths.data>/tui_state`. New: `internal/ui/gui/guitest` (a shared fake
  service for the GUI tests), the image draw op, `SDL_StartTextInput`/`SDL_StopTextInput` and `SDL_TEXTINPUT` in the binding, and
  `ui.gui.*` strings in en_us, pt_br, es_es and ru_ru.
- PR-71 review rounds (Crivo, Bússola): Quit when finished quits at once when the last job ended under the dialog; `SDL_GAMECONTROLLERCONFIG`
  and `SDL_GAMECONTROLLERCONFIG_FILE` are read in Go and added after the embedded database and the user file so they win (SDL applies them
  at `SDL_Init`, before our lines, which would have replaced them; `gui.md` and the contract say so now); when the exit sequence runs out of
  its 2 s budget the process leaves without calling `Close` again; closing the window asks like Quit; window shown/exposed/restored redraw;
  `-h` exits 0; a progress event no longer copies the job tally. UX (gui.md v1.1): the top bar keeps its title and uses the short pending-quit
  text (`ui.gui.bar.will_quit_short`) when the full one does not fit, Offline and the badge are never cut; the Home footer wraps to two lines
  and a long ROM folder is shortened in the middle; tile labels wrap and a long single word drops to a smaller size instead of being split;
  toasts take at most 60 % of the width, wrap to two lines, stay off the focused element, and the language notice stays 6 s; stacked dialog
  buttons fill the panel; hint pills have air around words; the Splash spinner is on the right; the exit notice shows `(A) Continue`.
- T-54 (GUI PR 1, Medium; the SDL binding stays under the High review of the spike): `mothdex-gui` now runs. Foundation, Splash,
  Home, the Start menu, the quit dialogs of docs/ux/gui.md section 5.13, the exit screen and SIGTERM, with the screens behind the
  tiles (Search, Platforms, Random, Queue, History, Settings) saying "not available yet" until the next pull requests.
  - `internal/ui/gui` (pure Go, no SDL): size tiers and the safe rectangle (section 1), focus engine by geometry (section 3), frame
    with top bar, hint bar, toast and queue badge, the model as a value with `Update`/`Render`, the quit decision table and the exit
    sequence (Cancel, Wait, Close inside the 2 s budget, with an injectable clock). `internal/ui/gui/input`: button positions, the
    confirm/back swap, repeat, stick hysteresis, Select on release, the keyboard fallback, hint labels by controller type, and the
    controller database loader (embedded, then `<paths.data>/gamecontrollerdb.txt`, foreign `platform:` lines dropped before SDL).
    `internal/ui/gui/text`: measuring, truncation, wrapping and a glyph atlas on `golang.org/x/image` v0.48.0.
    `internal/ui/gui/draw`: the draw list. `internal/ui/gui/loop`: the run loop over a `Backend` interface, tested with a scripted
    backend.
  - `internal/ui/gui/assets`: Noto Sans Regular (subset to Latin and Cyrillic, OFL-1.1) and `gamecontrollerdb.txt` (commit
    `4e87e3c6c257`, zlib), embedded and pinned by SHA-256 in `PINS.md` and `assets_test.go`; licences in `THIRD_PARTY/`. The font has no
    arrows, check marks or crosses, so the queue arrow is drawn as a shape.
  - `internal/ui/gui/sdl`: 17 more SDL2 functions (renderer size and logical size, blend and color mods, `SDL_WaitEventTimeout`,
    game controllers; `SDL_GameControllerGetType` is optional and only bound on SDL 2.0.12 or later, so the minimum stays 2.0.0),
    the event views and key and button maps, and `Backend`, which executes a draw list and turns SDL events into loop events.
    `scripts/check-sdl-constants.sh` covers every new constant and event layout.
  - `cmd/mothdex-gui`: composes the service through `internal/compose`, opens SDL and runs the loop; exit codes 0, 1 (SDL or
    display), 2 (configuration), 3 (internal), every failure also in the log file. `-probe` (or an executable named
    `mothdex-gui-probe`, which is how the CI legs run it) keeps the spike's headless check; `-window WxH` opens a window on a desktop.
    Interface languages the font cannot draw (`zh_cn`, `ja_jp`, `hi_in`) show `en_us` and one notice on Home.
  - Locales: `ui.gui.*` in en_us, pt_br, es_es and ru_ru.
- PR-69 review (Ensaio V1/S1): a test with a fake `api` proves `ReadPixels` and `UpdateTexture` refuse short buffers, a pitch below the row
  and a rect outside the texture before SDL is called; `GUI_REQUIRE_SDL=1` turns the missing-SDL skip into a failure and is set in the
  amd64 and Windows test steps.
- T-52b: CI for the GUI spike (`.github/workflows/gui-spike.yml`, read-only, actions pinned by SHA): the probe and `go test -tags gui`
  on linux amd64 with the distro libsdl2 (dummy driver), on windows amd64 with `SDL2.dll` 2.32.10 fetched by the exact release URL and verified against a
  pinned SHA-256 (`scripts/sdl2-windows.pin`); `readelf` per Linux target, the isolation check and a size report. The arm64, armv7 and armv6 legs (qemu-user, pinned
  Debian images) are in `gui-spike-qemu.yml` (qemu-user from the runner's apt and the pinned Debian images through docker, because the
  repository does not allow `docker/setup-qemu-action`; if binfmt does not work in containers it falls back to `qemu-<arch>`
  with a sysroot). The spike CI also runs negative library-planting tests (a garbage `libSDL2-2.0.so.0` or `SDL2.dll` in the
  working directory, next to the probe or on `PATH` must not load), requires a program interpreter and no RPATH or RUNPATH
  on every Linux probe, and reports the ARMv6 CPU-model run (attempted, exit code, `/bin/true` and ld.so control runs) in the job
  summary and as a warning. The result of the spike is recorded in `docs/qa/T-52-gui-spike-report.md`. New
  `scripts/check-sdl-constants.sh` compiles a C program against the libsdl2-dev headers and fails when a binding constant
  or struct layout differs.
- `internal/ui/gui/sdl` and `cmd/mothdex-gui` (T-52, spike): our own thin purego binding of 23 SDL2 functions (video, renderer,
  textures, events, `SDL_GameControllerAddMapping`), loaded from the system SDL2 at run time. Pointers, integers and floats
  only; `SDL_Event` and `SDL_Rect` go by pointer and their layout is tested per architecture. Linux opens `libSDL2-2.0.so.0` by
  soname only; Windows opens `SDL2.dll` from the executable folder or System32 only, never the current directory. The probe
  (`mothdex-gui`, tag `gui`, `CGO_ENABLED=0`) runs the headless check of the contract in order (load, version, mapping, window and
  renderer, atlas texture, draw, pixel read-back, render scale, event round trip), prints the SDL version and exits 0, or 1 when
  SDL2 is missing, too old or has no display, or 3 for a failed check. `scripts/gui-probe.sh` builds and inspects it for one
  target (ELF interpreter and `NEEDED`, the armhf `-I` flag) and runs it on the host; `scripts/check-gui-isolation.sh` fails
  if `mothdex` ever depends on purego or a GUI package. `purego` v0.11.3 is pinned in `go.mod`; `mothdex` does not contain it. `ReadPixels` and `UpdateTexture` check that the buffer holds every byte SDL will touch (pitch*(h-1)+w*bytes) before the call and refuse a shorter one; SDL2 is opened with `RTLD_LOCAL`.
- `docs/qa/T-43-shell-acceptance.md` (T-43): Shell acceptance at `b45d433` against shell.md and ui.md with a snapshot binary and a
  local fixture API (banner, paging, ranges and overflow inputs, link chooser, per-line flags, Tab, history mode, Ctrl+C and Ctrl+D,
  script mode and SIGINT), the findings S1 to S5 and the list of what was not verified.
- T-53 (E-M3 service items, gui.md section 8): `internal/compose` is the composition root both binaries build the service with (`compose.Open(ctx, Options) (*Session, error)`; a session's stored `on_exists=overwrite` is reported at start-up; `cmd/mothdex` wraps it in `cli.Session`). `app.DecodeCover` decodes a cover with the fetch limits (5 MiB, type allowlist agreeing with the content type, 4096 pixels per side checked from the header, the first GIF frame checked too), one frame, scaled down to `maxSide` (4 megapixel budget from the header, no full copy while scaling, a decoder panic becomes an error); WebP through `golang.org/x/image` v0.48.0 (BSD-3, Go team). Config keys `gui.confirm_button` (`south|east`), `gui.text_size` (`normal|large`) and `gui.safe_area` (`auto` or 0..10) with their `ConfigSchema` metadata. A regression test covers G3 (`Wait` on idle returns at once and refreshes only after a download).
- E-M2 polish (T-44), from the UX review: a Shell `download` of several entries prints `Link n chosen` for every entry with more
  than one link, with or without a preference (one entry still gets the chooser); the Shell no longer exits on a second Ctrl+C,
  only `exit`, `quit` and Ctrl+D leave; the TUI Entry screen shows the platform id with its name from the cached platform list
  (`gbc (Game Boy Color)`).
- `internal/ui/tui` (T-42, part 2 of 2): History and Settings, and the downloads that depend on them. History (`H`): every record
  newest first, `f` for failed only (the service filter), `/` to filter by title, details with the localized error, `d` to
  queue a failed download again, `e` to export (txt, json or csv by the extension, with an `Overwrite? [y/N]` question), and a
  reload when a job ends. Settings (`S`): the curated table of tui.md section 7 from `ConfigList` with a static kind and range
  per key until the app layer exposes the schema (gap F1); space toggles, Enter edits (an enum opens a list, a number shows its
  range and an invalid value is refused in place), Backspace resets, a row set by the environment or a flag is read-only and
  says which, and `Clear cache` asks first. The page size, the confirmation, the cover and the link preference apply at once;
  the others say "applies on next start". With `interface.confirm_downloads` on, a batch or a file over 500 MB asks before it
  is queued (cli-conventions section 6). With `download.boxart` on, the cover of a finished file is saved after its `Done`
  event (`SaveJobBoxart`; a failure is a warning). Review of part 1: the "past the last page" and "hidden results" notes share
  one toast, and a failed retry puts the jobs back in the Queue.
- `internal/ui/tui` (T-42, part 1 of 2): the full-screen terminal UI on Bubble Tea v2.1.2, Bubbles v2.2.2 and Lip Gloss
  v2.1.1. `mothdex tui` opens the frame of tui.md section 1 (title bar with the live queue summary, status bar with the keys
  of the screen, a toast for 3 s, `?` help) and the screens Search (query, platform and region pickers that fall back to typed
  ids, sort, the last 5 queries in `<paths.data>/tui_state`, Random), Results (marks per page, `n`/`p` paging, `s` sort,
  loading with Esc to cancel, empty and error states with `r` retry, `/` back to Search with the fields filled), Entry (the
  ranked link list is the chooser, `d` the highlighted link, `D` the best one, an "already downloaded" question, `P` for a
  session preference, `c` for the cover, left and right for the neighbours in the list) and Queue (`Q`: live states with the
  Warming counter on a UI timer, `x`/`X` cancel, `r` retry a failed job from its request, `c` hide finished, details with `y`
  to copy the path). Small terminals follow section 9: columns drop below 80, the secondary parts hide below 20 rows, and
  below 60x16 a notice replaces the screen until it grows back. `--no-color` leaves only bold, faint and reverse, and a
  non-UTF-8 locale gets the `+-|` frame. A `[y/N]` question accepts the localized "yes" initial. Every server string is cleaned
  before it is drawn (case 49). The key map is a pure reducer, tested as a table. History and Settings (`H`, `S`) are part 2 and
  say "not available yet" for now. New strings are `ui.tui.*` (`en_us`, `pt_br`, `es_es`, `ru_ru`). `cli` gains `Deps.TUI`,
  `TUIEnv` and `Service.Jobs`; `internal/ui/uistate` holds the atomic 0600 file writer shared with the Shell history. The
  Charm v2 build is checked for linux 386, armv6 and armv7 by the `goreleaser snapshot` job, which builds those targets.
- `docs/ux/gui.md` (T-50): UX of the gamepad GUI (`mothdex-gui`, E-M3): size tiers from 640x480 to 1080p (scaled
  below and above), TV safe area with a calibration view, type scale, spacing and color tokens with measured contrast,
  positional buttons with an A/B swap and per-controller labels, focus model, every screen with its button map
  (Home, Search with the virtual keyboard, pickers, Results with covers, Entry and link chooser, Queue, History,
  Settings, Start menu), errors and empty states, quit and the return to the frontend, the Port behavior, the
  service calls used, the gaps agreed with the T-51 contract, and a three-PR plan.
- `app.Service.ConfigSchema() []SettingSpec{Key, Kind, Enum, Min, Max, Env}` (F1, ui.md section 6): the metadata of every configuration key (kind string, enum, int, float, bool, duration, size or list; the allowed values or range; the `MOTHDEX_*` variable) for the TUI Settings screen and Shell completion. It reads no file, so it works with a broken configuration. `config.Specs()` builds it from the schema, and a test checks that every enum value and range bound agrees with the parser.
- `docs/contracts/cli/gui.md` (T-51): the E-M3 GUI contract, covering the separate `mothdex-gui` binary and its package layout, SDL2 through an own purego binding (with what the spike must prove and the decision rule), Noto Sans with the x/image raster, the pinned GameControllerDB, a single download root (no shared limiter), the exit budget, cover decode caps, the packaging impact, the test plan and the gaps for Forja.
- `docs/release-v0.1.0-checklist.md` (T-23): the ordered owner steps for the first release (open gates A2 and D1 to D3,
  environment and tag settings, tag, what `release.yml` publishes, verification of the published assets, rollback) and what
  was and was not verified beforehand, including a gap found in `publish` (no tag-to-commit check, no `--source-digest`).
- `docs/qa/T-22-m1-acceptance.md` (T-22): M1 acceptance run at `ebe6a54` with evidence per item (installer against the
  snapshot archive, mcp slug charset sample, command section C through the real binary and the package tests), the open
  items, the live download check, and findings F3 (`/dev/null` stdin is treated as a terminal), F4 and F5.
- `internal/ui/shell` (T-41): the interactive Shell. `mothdex shell`, or `mothdex` with no arguments on a terminal, opens a
  REPL over the same command tree as the CLI (`cli.Runner`: a fresh cobra tree per line, one open session), with a
  banner (entries, or `offline`, and the profile), the `mothdex[<platform|all>:<n>]> ` prompt, `n`, `p`, `page`, `sort`
  and `last` over the last search, a bare number as `info`, `d <#>` as `download`, index ranges (`download 2-4`, at most
  100), `help`, `clear`, `exit`/`quit`, did-you-mean for a mistyped command, Tab completion (commands, flags, platform
  and region ids, `--prefer`, config keys and values, cache namespaces, export paths, entry numbers), a history file
  `<paths.data>/shell_history` (mode 0600, written atomically, last writer wins when two Shells run at once, no lines with a
  leading space or a `config set` of a key ending in `token`, `key` or `url`, or whose value has credentials in an address), and an inline link chooser for `download <#>` when an entry has several downloadable
  links. Ctrl+C cancels the running command and not the Shell; at the prompt it only clears the line and says how to leave
  (`exit`, `quit` or Ctrl+D); a piped script still ends with 130 on SIGINT.
  `--config`, `--lang`, `--allow-executable` and `-j` are refused inside the Shell, and `mothdex -f shell` opens the
  service with overwrites and executables off. `cmd/mothdex/signals.go` routes SIGINT to the Shell while it runs;
  `cli.Session` gains `DataDir`, `NoColor`, `UTF8` and `Log`, `cli.Deps` gains `Shell` and `Interrupts`. New strings
  are `ui.shell.*` (`en_us`, `pt_br`, `es_es`, `ru_ru`; the other locales fall back). Line editing is
  `peterh/liner` v1.2.3 (MIT) behind a small `lineEditor` interface. The prompt is plain text, because liner refuses
  control characters in it. `tui` and `gui` still print "not available yet".
- `internal/ui/cli` and `cmd/mothdex` (T-20b): the command line. cobra commands `search`, `info`, `random`, `download`,
  `boxart`, `platforms`, `regions`, `db-info`, `history`, `cache stats|clear`, `config list|get|set|reset`,
  `profile show|detect` and `version` (`shell`, `tui`, `gui` print "not available yet"). Human tables with the 80/60
  column rules, the `--json` envelope v1 (partial data on a batch), the localized error block from
  `errors.<code>.message|hint` (a placeholder with no value falls back to the service text), the exit codes of
  cli-output section 4 (7 for a partial batch, 130 for Ctrl+C; a second Ctrl+C within 2 s exits at once), `--prefer`,
  `--link`, `-o` (becomes the absolute root), `-e/--export` with the CSV formula guard, and the confirmation prompt rules
  (never with `--json`, `-q` or a piped stdin). Every API string passes `SanitizeText` before a terminal (case 49);
  `--json` keeps raw values. `cmd/mothdex/compose.go` is the composition root (config, log, i18n with `LoadDir`, cache,
  API client with the persisted 429 cooldown, cover fetcher, history, profile, app). `app.Service.SaveBoxart` writes
  covers through the confined destination root (O_EXCL, launcher folders refused, an existing file is kept). Progress is
  one line per state change on stderr; a live redrawn bar is not done yet. Review round: `--json` escapes C1 controls
  (CSI/OSC), `error.exit_code` is the process code and `warnings[]` is filled, failure messages carry their named values
  (`path`, `name`, `host`, ...) and never show a raw `{placeholder}`, export write failures are exit 6 with a `--force`
  hint, the cover of a downloaded file is named after the file (`SaveJobBoxart`) and is staged then published, a new
  cover fetch leader re-checks the cache, the persisted 429 cooldown seed is clamped to 10 minutes, `CSVCell` also strips
  U+2028/2029, and the UI imports only `app` (export writers and type aliases moved behind it). Catalog case 49 is
  now covered.
- `docs/contracts/cli/ui.md` (T-40): the E-M2 Shell and TUI contract: packages and imports, `cli.Runner` for the Shell, launchers and an interrupt hand-off in `cli.Deps`, service calls per screen, pinned liner v1.2.3 and Charm v2 versions, test plan, and the app gaps for Forja.
- CI job `release tools build` compiles the pinned release tools (goreleaser, cosign, syft) from `tools/*` on every PR, the
  way `release.yml` installs them, so a dependabot bump there that no longer builds is caught before a tag (PR-31-sec).
- T-21c: installer hardening from the #31 reviews. `install.sh`: no `latest` through wget (a redirect could roll it back; pass `--version`), `dir_safe` also refuses a bin_dir with a world-writable non-sticky ancestor (physical path), the FAT exception resolves symlinked parents and stays inside the profile tree. `install.ps1`: a download whose final address is not https is refused (PowerShell 5.1 follows https to http). Tests: parent, sticky, symlinked parent and wget-latest cases; Pester cases for an inherit-only generic ACE and a real http response.
- `docs/qa/T-22-m1-acceptance.md`: the M1 acceptance checklist (T-22) with the evidence source per item, what waits for
  T-20b, and the plan for the mcp slug charset sample.
- `internal/app` (T-20a): the app service behind the four UIs. Catalog: search with a saved session, entry, random,
  platforms, regions, db-info, link ranking with the downloader URL policy applied first (case 28), covers through
  the hardened fetcher (singleflight, per-session negative cache), stale cache fallback (7 d). Downloads: `Enqueue`
  validates every request before queueing any, a Downloader per destination root (`-o` or `download.dir` becomes the
  absolute root; otherwise the profile ROM root), startup `Cleanup`, journal, `Confiner` with the profile launcher
  paths, `Jobs`/`Events`/`Cancel`/`Wait`, the frontend refresh once per batch and only after a Done job (case 46),
  and the history record with title, link index, expected size and error object. Config read path that works on a
  file holding `on_exists=overwrite`; the overwrite-without-force refusal is `invalid_config` (exit 2). `*app.Error`
  has its whole text cleaned of terminal escapes and maps to the exit codes (case 49, service half).
- `internal/i18n` and `locales/`: embedded locales plus an optional external folder (per-key override), automatic
  detection (`LC_ALL`, `LC_MESSAGES`, `LANG`; the Windows preferred UI languages), `interface.language` resolution
  (exact, same language, en_us), named `{placeholders}`, CLDR plural forms, and a per-key fallback to en_us. `en_us`,
  `pt_br`, `es_es` and `ru_ru` are complete (the `errors.<code>.message` and `.hint` table of cli-output section 4 and
  the UI basics); `zh_cn`, `ja_jp` and `hi_in` translate the UI basics and fall back for the rest. A test fails when a
  locale lacks keys it promises, defines keys en_us lacks, or changes placeholders.
- `internal/log`: the slog logger (T-14). A size-rotated file `mothdex.log` under `paths.logs` (`log.max_size_mb`,
  `log.max_files` in total, created with mode 0700/0600) and an optional stderr copy (`log.console`), from the `log.*`
  settings. Standard library only. A log file that cannot be opened never stops the program: `log.New` returns a
  usable logger plus an error to show once as a warning.
- `internal/history`: JSONL transfer history (`Append`, `List` newest first with a `--failed` filter, damaged lines
  skipped) and export to txt (120 columns), json (the `--json` envelope) and csv (RFC 4180, control and format
  characters stripped, formula guard); `ExportTarget` and `WriteExport` give directory naming, format inference and an
  atomic no-overwrite write. `internal/session`: the last search in the `session` namespace, `Resolve(index)` with
  `no_session` and `index_out_of_range`. Case 50 covered.

- `pkg/mothdex`: `KindUntrustedRedirect` (`untrusted_redirect`, exit 4) and `IsUntrustedRedirect`. A redirect to another
  origin (host, scheme or effective port), a fourth hop, or a 3xx without a usable `Location` is refused, not retried,
  does not open the rate-limit circuit and is never `KindNetwork`; the message names the target host only (T-10c).

### Changed

- `docs/ux/gui.md` v1.1 (PR-71-ux, from the UX review of cli#71): top bar fit rule (the title keeps the screen name,
  `ui.gui.bar.will_quit_short`, `Offline` and the badge are never cut), toast width and 2-line wrap with a 6 s locale
  notice, `bg` discs and dialog buttons on `surface`, symbols drawn as shapes, no cut text except titles (wrap, then a
  middle-shortened path), Home tile and footer wrapping, stacked dialog buttons, spinner on the right, and an
  `(A) Continue` hint on the exit notice. After the cli#71 recheck (PR-71-ux-2): a toast that would cover the focus
  moves above the hint bar, a single long word in a tile label shrinks before it is cut, and a disc on `bg` uses `surface`. From the cli#76 review (PR-76-ux): the hint bar fit rule, and the Start word kept
  where Start is the screen's main action.
- Downloader size authority (T-28, implements T-27 / downloader.md 3.2, T-16 cases 31, 32b-32d, 35, 36): the proxy's `Content-Length` decides a transfer. It must be present, ASCII digits and at least 1 (missing, chunked, `0` or malformed is `size_mismatch`, not retried, no body byte read), is pinned across retries (a change is `size_mismatch`), is held to `download.max_file_size`, and replaces the `link.size` space claim in one atomic ledger swap (`disk_full` before the body, a smaller length shrinks the claim). When only the real size crosses 500 MiB (`interface.confirm_downloads` on, `link.size` below it) the job ends `confirmation_required` before the first body byte, carrying the size and a hint; `DownloadRequest.SizeConsent` is `0` (default rule), `app.ConsentAny` (the `-f` of that command, never a session's start-up flag) or the exact Content-Length accepted through Download anyway. `link.size` stays a preflight estimate and a drift warning beyond 1.5x or 0.5x (`JobSnapshot.SizeDrift`, worded by the UI in its language with sizes, also in `warnings[]`); a Content-Length with a leading zero is refused like mothdex-mcp does. By design (downloader.md 3.2 step 6) a catalog size already above 500 MiB skips the real-size confirmation, so the cap and the free space are its only bounds. History records `catalog_size` and `content_length`; the download JSON jobs carry `catalog_size`. The CLI does not offer Download anyway interactively: it fails with the hint to add `-f` (the TUI, Shell and GUI wire the re-queue).
- Downloader contract (T-27, downloader.md §3.2, T-16): the proxy's `Content-Length` is now the size authority for a
  transfer, because the maintainer states `link.size` is scraped and only a rough indicator (cavv-dev/mothdex-api#1).
  The length is required and strict (missing, chunked or `0` is `size_mismatch`), pinned across retries, and the
  file cap, the space ledger and the 500 MB confirmation re-run on it before the first body byte (`confirmation_required`,
  then Download anyway with `DownloadRequest.SizeConsent`). `link.size` stays a preflight estimate and a drift warning.
- `internal/proxy/testdata/sanitize-vectors.json` (T-61 F-1): the `Sanitize` vectors as a shared fixture (`input`, optional `fallback` and `allow_executable`, and an `output` or a stable `error`: `unsafe_name` or `empty_name`). `TestSanitizeVectors` reads it; mothdex-mcp copies the file and checks it for divergence (mcp `download_file`).
- `docs/ux/shell.md` and `docs/ux/tui.md` (UX-sync): record the E-M2 deviations accepted in Decisions D24, and
  three changes from the D24 review: `download` with several indexes prints the `Link n chosen` line for each entry
  that has more than one link; Ctrl+C never exits the Shell (only `exit`, `quit` and Ctrl+D leave); the TUI Entry
  screen shows the platform id and name.
- App service contract (T-20b): `SaveJobBoxart` (cover named after the downloaded file) and `SaveBoxart` (`boxart`
  command) replace `DownloadRequest.Boxart`; exports go through `app`; UIs may also import `internal/i18n` and
  `internal/buildinfo`.
- Contracts: one slug rule for both repos (`^[a-z0-9][a-z0-9-]{0,255}$`, `mothdex.ValidSlug`). `Entry` refuses other
  slugs locally, search drops entries with an unsafe slug before numbering (`dropped` in `search` JSON, a warning), and a
  single-entry answer with one is `api_incompatible`.
- T-16 catalog: cases 51 and 52 (installer launcher path injection and permissions) are now Covered by
  `tests/installer/cases.sh`, `nonroot.sh` and `install.Tests.ps1`; 51 of 52 cases are Covered, only 49 (terminal
  escapes in the UI output) is Pending.
- Contracts follow the T-20a fixes and the T-21 installer: `Events` is never lossy (per-subscriber queue), the
  journal entry carries its destination root and cleanup is per root, a failed session save deletes the stored
  session, and the installer launcher is opt-in (D22) with the root-only FAT share exception (TH-31).
- App service contract (T-20a review): capability method names as implemented, UI packages import only `app`
  (aliases for internal types), one downloader per destination root, and the service writes boxart.
- Release and profile contracts follow the T-21b installer: `checksums.txt` also lists SBOMs, the exact-line
  checksum rule, the `install.ps1` architecture mapping, and the RetroBat `bin_dir` in `%LocalAppData%`.
- CLI conventions §7: English error strings for `invalid_request`, `unmapped_platform`, `untrusted_redirect` and
  `unsafe_name`; `path_rejected` now describes a refused destination, not a file name (that is `unsafe_name`).
- Contracts: `Service.Cover` returns the cached reader and content type (not a path), `internal/cover` owns the
  cover DNS port (the downloader `Resolver` port is dropped from downloader §8), and cache §4 says how T-20 wires it.
- `internal/config`: `download.on_exists=overwrite` from the config file or `MOTHDEX_DOWNLOAD_ON_EXISTS` is rejected
  (exit 2) unless `Options.Force` (`--force`) is set; case 22 is covered.
- `internal/proxy`: `finalize` and `Cleanup` split into smaller steps, `FS.Open` returns a `ReadFile` with `Stat`
  (no type assertion), `Result.Path` documented as absolute; test helpers use `bytes.Reader`, the memory-backed
  directory test asserts on `/dev/shm` when it is tmpfs, and stale `dest_outside_root` wording is gone.
- Downloader contract (T-17 architecture pass): same-device staging inside the destdir (no double write), the
  batch keeps finalized names so `overwrite` never replaces them, `Retry-After` capped at 15 min, absolute
  `Record.Path`, and what the app service needs from the downloader (ids at submission, per-job cancel).
- Downloader contract: within one batch, a name collision under `on_exists: overwrite` is resolved by rename,
  never by replacing another job's file; the injected ports (`FS.Open`, `FS.SyncDir`, `Journal`, `History`) and the
  history `Record` are documented. `download` JSON gains `raw_filename` and `hash` (additive).
- System profiles map 40 to 48 Mothdex platform ids per frontend (Batocera, EmuELEC, RetroBat, EmuDeck, RetroDECK,
  Recalbox) from the T15b research; arcade ids (D18) and `pc` (D19) stay unmapped on purpose. Contracts document the folder
  rules and that the launcher-folder check is lexical and is repeated after path resolution.
- API client contract: a refused redirect (another origin, a scheme change or more than 3 hops) is the
  non-retryable kind `KindUntrustedRedirect` (error code `untrusted_redirect`, exit 4), never a network error.
- Profiles contract: probe type `files`, the `${MATCH}` variable and the `folders_verified` flag (T-15).
- Cache contract documents the `AllowStale` and `WithTTL` options and the best-effort rename retry on Windows.
- API client and config contracts: `http` base URLs are accepted only for loopback IP literals, not `localhost`.
- Contracts: `version` output shape (`version`, `commit`, commit `date`, `go`, `os`, `arch`; empty in dev builds)
  and the release-artifact contract for `install.sh` (archive names, 9 targets including armv6, `uname` mapping,
  checksum and optional cosign verification).
- Config contract records the T-11 decisions: portable paths apply with any config file, config errors carry no
  `file:line`, `config reset` on a missing file writes only `version: 1`, and `mothdex.ValidateBaseURL` is the only
  rule for `api.base_url`.
- Downloader and related contracts updated with the 14 change requests from the T-16 threat model: proxy-only URL
  policy, `Sanitize` spec and executable deny-list, no-replace finalize, space reservation ledger, zip caps, cover fetch
  SSRF guard, refresh URL policy, platform-id and folder validation, Windows junction-safe confinement. New config key
  `download.max_file_size`, new error code `unsafe_name`, new flag `--allow-executable`.

### Added

- T-21b: `install.sh` (POSIX sh; Batocera, EmuELEC, Recalbox, Lakka, generic Linux, macOS) and `install.ps1`
  (Windows, RetroBat). Downloads only from the GitHub release of `Or4cu1o/mothdex-cli` over https, always verifies
  SHA-256 from `checksums.txt`, and also `cosign verify-blob` or `gh attestation verify` when the tool is present
  (a present tool that fails aborts). `--version` pinning, `--dry-run`, `--uninstall`, profile `bin_dir`, a fixed
  launcher template written only to the profile launcher folder (path allow-list, marker, never overwrites a
  user's file). Tests: shellcheck, 193 container checks (Alpine and Debian, no network, simulated Batocera and
  EmuELEC trees, fake release, a second pass as a non-root user) and Pester-style checks for `install.ps1` in CI (`installer.yml`). The launcher is opt-in (`--launcher`, `-Launcher`) in v0.1 because `mothdex tui` does not exist yet;
  wget is used only if it is GNU Wget and a signature tool (cosign or a logged-in gh) is present, because wget can follow an https to http redirect (busybox wget does not verify TLS and is refused); on exFAT/FAT/NTFS share partitions of Batocera
  and Recalbox, root installs get a documented, warned exception to the world-writable refusal.
- `internal/cover`: cover fetch hardening (T-16 CR-9, cache.md section 4): https only, no connection to loopback,
  private, link-local, CGNAT, multicast or special addresses (checked after DNS and again in the dialer Control hook),
  at most 3 redirects each re-checked, 5 MiB cap, Content-Type must agree with the magic bytes (jpeg, png, webp, gif;
  SVG refused), header-only size check of at most 4096 x 4096. Violations are a cache miss. Cases 47 and 48 covered.

- T-18: tests for T-16 cases 14, 15 (loader side), 22 (configuration half), 44 and 45 in `internal/config` and `internal/library`; the catalog marks 14, 44 and 45 as covered.
- `internal/library` and `profiles/*.yaml`: system profiles for Batocera, EmuELEC, Recalbox, Lakka, RetroBat, EmuDeck,
  RetroDECK and generic. Detection is a pure function over an injected file tree (priority, ambiguity and any doubt fall
  back to generic with a warning), `DestDir` with platform-id and folder confinement, folder overrides, unverified-item
  reporting, and the refresh hook restricted to `http://127.0.0.1:1234/reloadgames` or `[::1]` (3 s, no proxy, no
  redirects, non-fatal). Only the 15 platform ids confirmed in the T1e research are mapped to folders; the rest stay
  unmapped until sourced. A `profile.rom_root` (or any `DestDir` result) inside a launcher folder is refused
  (`Profile.IsLauncherPath`, per component and look-alike aware).
- `internal/testutil`: `fakeresolver` (scripted DNS), `zipfix` (zip bomb, overlapping-member, declared-size and member-count
  fixtures) and `realfs` (real-directory helpers: links and Windows junctions, tree snapshot, case-sensitivity probe) for
  the downloader tests (T-18). gosec is not run on `internal/testutil`.
- `internal/proxy`: the downloader (T-17, proxy links only, D16). `Sanitize` for server-supplied names (same rules on every
  OS, executable deny-list, fallback name), URL policy (https, exact allowed host, no user info, port 443, no IP or IDN),
  confinement of the destination directory per component and after the OS resolved links (`GetFinalPathNameByHandle` on
  Windows), a real FS over `os.Root`, a no-replace finalize per OS (Linux `renameat2`, macOS `renameatx_np`, Windows
  `MoveFileExW`) with link and placeholder fallbacks and an EXDEV copy, `on_exists` (skip, rename up to 99, overwrite
  only with `--force`), a per-filesystem space ledger, name reservation, in-flight journal and startup cleanup, retry
  policy with injected clock and sleeper, zip CRC and upstream hash checks with caps, fsync of file and directory before
  `Done`, and one history record per job. Adds `golang.org/x/text` and `golang.org/x/sys`.
- T-18 audit round: case 39 ages the symlink target (a following cleanup would delete it), case 24 asserts casefold collisions on every file system, a finalize fallback chain test (hard link, O_EXCL placeholder, second writer in each step) and a launcher-behind-a-link test on a real detected profile. The rename-policy subtest runs in both the hard link and the placeholder step, with titles that name the step.
- T-18: `fakefs` counts reads (`OpRead`, `BytesRead`); the case 31, 32, 41 and 42 tests now prove abort at size+1, the exact size limit, bounded verification reads and a stop right after a cancel.
- T-18: tests for 40 of the 52 T-16 threat-model cases against the downloader (`internal/proxy/cases_*_test.go`):
  URL policy, redirects, gzip, sizes, TLS, platform injection, symlink and junction destinations, launcher folders,
  on_exists races, disk full, cross-device copy, cancel, startup cleanup, zip bombs and the Sanitize invariants
  (property and fuzz). The catalog marks each case with its test; the other 12 belong to other packages.
- `internal/testutil/fakefs`: `proxy.FS` and `proxy.Finalizer` doubles over a confined temp root with fault injection
  (ENOSPC mid-write, EXDEV, unsupported no-replace rename and hard link), scripted free space and device ids, hooks
  between steps, a call log and a Sync spy, for the downloader disk, partial-file and race cases (T-18).
- `internal/cache`: on-disk cache (search, entry, meta, info, covers, session, state) with canonical request keys,
  per-namespace TTL, sidecar metadata, stale reads, a 200 MB cap with expiry-first then LRU eviction, atomic writes,
  an injected clock and private file modes (0700/0600).
- `internal/testutil/fakeproxy` and `internal/testutil/fakeclock`: scripted TLS fake of the download proxy (truncate,
  stalls, transient 404, 429, Content-Length mismatch, gzip, redirects, malicious Content-Disposition, oversized body)
  driven by a fake clock, plus a catalog of the 52 downloader test cases from the T-16 threat model, all pending until
  the downloader exists.
- `internal/config`: typed configuration with the precedence defaults < YAML file < `MOTHDEX_*` environment < flags
  (per key, with the source of each value), closed schema v1 validation (types, ranges, `download.max_file_size`,
  `download.prefer.size`, `profile.*`), OS default paths and portable mode, and atomic `set`/`reset` of the YAML file.
- `mothdex` binary embeds fallback CA roots (`x509roots/fallback`) for systems without a CA bundle; `pkg/mothdex` has
  no option to disable certificate verification, and `WithHTTPClient` callers own their TLS policy.
  `info.error` messages lose Unicode control and format characters at parse time (defense in depth).
- `pkg/mothdex`: client for the Mothdex API v1 (search, entry, random, platforms, regions, info) with typed
  errors (HTTP 200 `info.error` included), client-side rate limit (5 req/s), 429 handling, a breach circuit,
  bounded retries with backoff, per-attempt timeouts and context cancellation. Recorded API fixtures live in
  `pkg/mothdex/testdata/api-v1/`; `tools/record-fixtures` re-records them (developer tool, not shipped).
- Design documentation in `docs/`: contracts for the API client, app service, downloader, cache, config,
  system profiles and machine output; ADR-0001 (stack) and ADR-0002 (downloader without `Range`); UX
  conventions (Shell, TUI, `--prefer`); research notes. Adds the config key `download.prefer.size`.
- Release pipeline: GoReleaser OSS configuration for 9 targets (linux
  amd64/arm64/armv7/armv6/386, windows amd64/arm64, darwin amd64/arm64; static,
  trimpath, reproducible timestamps), `checksums.txt`, SBOMs, keyless cosign
  signature, GitHub artifact attestations, a tag-only release workflow
  (semantic-version tags on `main`, per-job permissions, no dependency cache,
  signature and attestation verification before publishing) and a snapshot check
  on pull requests touching it (`scripts/verify-dist.sh`, double build). Release
  tools are pinned in `tools/*/go.mod`. Nothing is published to package managers.
  Artifact naming, verification and owner-side settings are in `docs/release.md`.
- `internal/buildinfo`: version, commit and commit date of the binary
  (ldflags, with a `runtime/debug` fallback for `go install` builds);
  `mothdex` prints them.
- Project skeleton: Go module, package layout with placeholders (including `internal/app`), minimal
  `mothdex` binary that prints its version.
- GPL-3.0 license, governance documents, issue and pull request templates.
- CI for pull requests and `main` (golangci-lint, `go test -race` on Linux,
  Windows and macOS, govulncheck, gitleaks) with actions pinned by commit SHA.
- Dependabot for Go modules and GitHub Actions.

### Fixed

- `download` with no `-o` and no `download.dir`, when the profile's default ROM folder does not exist, now fails with `path_rejected` (exit 6) and a hint that names the folder and the two ways out (`-o <folder>` or `mothdex config set download.dir <folder>`), instead of a generic `invalid_request` with an unrelated hint (T-26, from Ensaio's T-43 S5). The folder is not created for the user.
- Shell (T-41-fix, from the T-43 acceptance, S1): `info` and `boxart` with a list or a range (`info 2-3`, `boxart 1-2`) now run once for each entry with the same flags,
  as shell.md section 4 always said, instead of failing with "accepts 1 arg(s)"; the exit code is the first failure and the later
  entries still run. shell.md also records that Ctrl+D with stdout redirected ends with the last command's code (S4) and the 0-column
  terminal limit (S3). The double Ctrl+C of S2 is already gone from main (#64: Ctrl+C never leaves the Shell).
- T-25 backlog sweep: `Enqueue` removes the directories it created for earlier `-o` roots when a later root cannot be opened; `config.Set` itself refuses a stored `download.on_exists=overwrite` (the check moved from `app.ConfigSet`); `SaveJobBoxart` looks the job up by `JobID` in what the service recorded (bounded to 1000), so a forged snapshot cannot choose the folder or the entry; `history -e json` goes through the same C1 escaper as `--json` (`app.EscapeC1`); `SearchPage.SessionStale` and a warning when the search session can be neither saved nor cleared; a page whose results were all dropped says so instead of advising fewer words. Already done earlier and kept: journal entries carry their root (one root's Cleanup leaves the others), U+2028/2029 are stripped.
- T-20c and T-22 follow-ups: `mothdex.ValidSlug` (`^[a-z0-9][a-z0-9-]{0,255}$`, one rule with mothdex-mcp) and the inbound policy of api-client section 2. `Search` drops results with an unsafe slug before numbering and before the session is saved (`SearchPage.Dropped`, the `dropped` field of the search JSON, and a warning on stderr and in `warnings[]`), `Entry` returns `api_incompatible` for such an answer, `Random` skips it, and `Entry` refuses an unsafe slug locally. `isTerminal` is a real TTY check (`golang.org/x/term`, BSD-3, maintained by the Go team): `/dev/null` and `NUL` are character devices, so `mothdex download a b </dev/null` prompted, read EOF and exited 130 instead of `confirmation_required` (2) (F3; the Shell uses the same check). `info` on an unknown slug says `No entry found for <slug>.` instead of repeating the code (F5). Crivo's suggestions: byte counts in error messages are sizes (`1.5 MB`), `info` without an argument points at `mothdex info --help`, and a single download cancelled with Ctrl+C prints nothing besides the exit code 130.
- `internal/cache` on Windows: readers open cache files with `FILE_SHARE_DELETE`, and the replace uses `FileRenameInfoEx` with `FILE_RENAME_POSIX_SEMANTICS` (Windows 10 1809, NTFS; `os.Rename` falls back where unsupported), because `MoveFileEx` still refuses a target with an open handle. A `Put` renaming a new version over an item that a reader holds open no longer fails with "Access is denied". The bounded rename retry could not beat a busy reader (`TestConcurrentGetAndPutKeepTheLatestItem` failed after 59 s on windows-latest); the retry stays as a second line of defence. Regression test `TestPutReplacesAnItemThatAReaderHoldsOpen` holds a reader open across the Put, with no waits.
- `internal/app` test `TestCoverFromCacheThenFetchSingleflightAndNegativeCache` no longer fails about 1 run in 40 under `-race`: the gate opens only after all five callers joined the single fetch (a join counter on the in-flight call replaces the sleep polling).
- Config `profile.folders` values reject launcher-folder look-alikes (`portſ`, `wındows`) and the full set of Windows
  device names (`COM0`, `LPT0`, superscript digits); `api.base_url` rejection messages (load and `set`, client validation) never echo the URL userinfo, and a URL that does not parse is
  replaced by a placeholder; the API client
  accepts `http` only for loopback IP literals, not `localhost`.

[Unreleased]: https://github.com/Or4cu1o/mothdex-cli/commits/main
