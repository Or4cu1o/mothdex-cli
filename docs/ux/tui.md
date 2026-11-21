# UX: TUI (full screen)

Status: draft v1 (T-6) · Owner: Bússola · Implementer: Vitral · Risk: Medium
Services: [`app-service.md`](../contracts/cli/app-service.md) only (Catalog, Downloads, History, Config, Cache). Stack: Bubble Tea v2 + Bubbles +
Lip Gloss (PLAN §9). Wording, color roles, units and progress/Warming rules come from [`cli-conventions.md`](cli-conventions.md); link
ranking from [`prefer-syntax.md`](prefer-syntax.md). Gamepad GUI is out of scope here: see [`gui.md`](gui.md) (E-M3).

## 1. Layout and navigation model

Reference size 80x24; supported minimum 60x16 (§8). Every screen has the same frame:

```
┌ mothdex ─ Search ───────────────────────────── ↓ 2 active · 1 queued ┐
│                                                                        │
│                       (screen content)                                 │
│                                                                        │
├────────────────────────────────────────────────────────────────────────┤
│ / search  enter open  d download  Q queue  H history  S settings  ? help│
└────────────────────────────────────────────────────────────────────────┘
```

- **Title bar**: app name, current screen, and on the right the live queue summary (`↓ 2 active · 1 queued`, hidden when
  idle). It is visible on every screen, so downloads are never lost behind navigation.
- **Status bar** (bottom): the 6-8 most relevant keys for the current screen, from the same table as `?` help.
  Transient messages (toast) replace it for 3 s (`Queued 1 file`, `Copied path`), warn/error colors per conventions §4.
- Frame uses box characters; with a non-UTF-8 locale it falls back to `+-|`.
- Screens form a stack: `Esc` goes back one level; `Q`, `H`, `S` open queue/history/settings from anywhere (not while a
  text field has focus); `?` toggles help; `Ctrl+C` or `q` on the root screen asks `Quit? Downloads in progress will be
  canceled. [y/N]` only when jobs are active, else exits.

Navigation map:

```
Search ─Enter─▶ Results ─Enter─▶ Entry detail ─d / Enter on link─▶ (queued, toast)
   │               ▲                  └─ L ─▶ Link chooser (only when >1 link or to override)
   └ Q Queue · H History · S Settings (overlay screens, Esc returns)
```

## 2. Search screen (home)

```
┌ mothdex ─ Search ──────────────────────────────────────────────────────┐
│  Search   [ zelda_                                   ]                 │
│  Platform [ snes, nes        ▾ ]   Region [ any ▾ ]   Sort [ relevance ▾ ]
│                                                                        │
│  Recent   zelda · metroid · chrono trigger                             │
│  1,284,019 entries · profile: generic                      [ Random ]  │
└────────────────────────────────────────────────────────────────────────┘
```

- Focus starts in the Search field. `Tab`/`Shift+Tab` cycle fields; `Enter` in any field runs `Catalog.Search`.
- Platform and Region are pickers: a filterable list overlay (type to filter, `Space` toggles multi-select, `Enter`
  confirms). Values from `Platforms`/`Regions` (lazy, cached). Shows `(loading…)` then the list; on failure `Couldn't load the list. Enter the id manually.`
  and the field becomes free text.
- `Recent`: the last 5 query texts, UI-owned (`<paths.data>/tui_state`), `↓` from the field selects, `Enter` runs.
- `Random` (button, key `r` when focus is not in a text field) calls `Catalog.Random` with the current filters, Count 10,
  and opens Results with them (no paging; header says `Random picks`).
- Empty search text with no filters is allowed only with at least one platform or region; otherwise inline error
  `Type something or pick a platform.` (no network call).

## 3. Results screen

```
┌ mothdex ─ Results ─ "zelda" · snes ──────────────── ↓ 1 active ────────┐
│ [x] 1  Legend of Zelda, The: A Link to the Past   snes   us,eu   1.0 MB │
│ [ ] 2  Zelda no Densetsu: Kamigami no Triforce    snes   jp      1.0 MB │
│>[x] 3  Link's Awakening DX                        gbc    us      1.0 MB │
│ [ ] 4  Zelda II: The Adventure of Link            nes    us      128 KB │
│  ...                                                                   │
├ Page 1/4 · 37 results · 10 per page · cached 3h ago ───────────────────┤
│ ↑↓ move  space mark  enter open  d download marked  n/p page  s sort    │
└────────────────────────────────────────────────────────────────────────┘
```

- Columns and truncation exactly as `search` in cli-conventions §2-3; the leading `#` is the same index the CLI and
  Shell use (the TUI search also writes the session, so `mothdex download 3` works after quitting).
- `>` marks the cursor row; `[x]` marked rows. The footer counts marks: `2 marked`.
- `Enter` opens Entry detail. `d` downloads: the marked rows, or the cursor row when nothing is marked. A row with
  several links uses the default ranked link (config `download.prefer`) and the toast names it; to choose, open the
  entry and press `L`.
- `n`/`p` or `PgDn`/`PgUp` page via `Search` with the new page (cache first, so paging back is instant); marks are
  kept per page and cleared on a new search. `s` cycles sort `relevance → title → size` (current page only, labeled so).
- States: **loading** (`Searching…` spinner, input still responsive, `Esc` cancels the context); **empty**
  (`No results for "xyz". Try fewer words, or remove filters.` with `/` to edit); **error** (message + hint from
  `errors.<code>`, `r` retries); **stale** (`stale: served from cache` in warn color).
- `PastLastPage` shows a toast `Page 9 does not exist; showing page 4.`
- `/` returns to the Search screen with fields filled (edit and re-run).

## 4. Entry detail and link chooser

```
┌ mothdex ─ Entry ───────────────────────────────────────────────────────┐
│ Link's Awakening DX                                                    │
│ Platform gbc (Game Boy Color) · Regions us · Serial -                  │
│                                                                        │
│ Links                                         ranked by: lang=En       │
│ >1* Game  gbc   1.0 MB  En        Myrient   lang=En, format=gbc        │
│  2  Game  gbc   1.0 MB  En,Rev A  Myrient                              │
│  3  Demo  gbc   0.2 MB  -         Archive                              │
├────────────────────────────────────────────────────────────────────────┤
│ ↑↓ link  enter/d download this link  D download best  c cover  esc back │
└────────────────────────────────────────────────────────────────────────┘
```

- Data: `Catalog.Entry` for the metadata and `Catalog.Links` (ranked with the active preference) for the list; `*` is
  the default winner, the last column is `Reasons` (dim). Cursor starts on the winner.
- `Enter` or `d` downloads the **highlighted** link (`LinkSelector{Index}`); `D` downloads the default winner without
  moving. The toast confirms `Queued: Link's Awakening DX (link 1)`; the user stays on the screen (to queue more).
- `Left/Right` or `[`/`]` move to the previous/next entry in the Results list without going back.
- Preference tweak: `P` opens a small overlay to set `lang`, `format`, `source`, `size` for this session only
  (same keys as `--prefer`, prefer-syntax §1); persisting is done in Settings. The list re-ranks immediately.
- Cover: terminals cannot show images reliably, so the cover is not rendered. `c` calls `Catalog.Cover` and shows the
  cached file path in a toast (`Cover saved: <path>`), usable by external viewers. (Revisit with Kitty/Sixel only after v1.0.)
- Already downloaded (history says `Done` for this slug and link): the row shows `✔ downloaded` (dim) and download asks
  `Already downloaded. Download again? [y/N]` (respects `download.on_exists`).
- An entry with no links: `No links available for this entry.` and download keys are disabled.

## 5. Download queue (`Q`)

```
┌ mothdex ─ Queue ───────────────────────────────── 2 active · 1 queued ──┐
│>Super Mario Bros. 3 (USA).nes                                           │
│   ██████████████░░░░░░░░░░  58%  222 KB / 384 KB  1.2 MB/s  eta 00:01   │
│ Metroid (USA).nes                                                       │
│   Warming up the server… 02:14                                          │
│   The server is fetching this file from peers. This is normal.          │
│ Zelda II (USA).nes                                          Queued      │
│ ── finished (3) ──────────────────────────────────────────────────────  │
│ ✔ Kirby's Adventure (USA).nes   Saved to /roms/nes  crc ok  12s         │
│ ✘ Contra (USA).nes   The downloaded file has the wrong size  [size_mismatch]│
├─────────────────────────────────────────────────────────────────────────┤
│ ↑↓ move  x cancel  r retry failed  c clear finished  esc back            │
└─────────────────────────────────────────────────────────────────────────┘
```

- Source: `Downloads.Jobs()` for the initial list and `Downloads.Events()` for updates (≤ 4/s per job). The TUI keeps
  finished jobs for the session in a collapsed "finished" section (newest first); `c` hides them from view only (history keeps them).
- State rendering follows cli-conventions §5 (same strings, same Warming counter with its own UI timer, hint after 30 s).
- `x` cancels the highlighted job (`Cancel`; asks nothing, since the `.part` is disposable); `X` cancels all with a `[y/N]` prompt.
- `r` re-queues failed jobs from their original request (`Enqueue` again; no new service method).
- Enter on a finished job shows its details (path, SHA-256, attempts, duration) in a panel; `y` copies the path
  (OSC 52 when available, else shows it in a toast).
- When the last job finishes, a toast shows the summary `3 downloaded, 1 skipped, 0 failed`.
- Non-empty queue and quit: the quit confirmation of §1.

## 6. History (`H`)

```
┌ mothdex ─ History ────────────────────────── filter: all · 128 records ─┐
│ Date              State   Title                          Plat  Size     │
│ 2026-11-20 14:03  ✔       Kirby's Adventure (USA)        nes   384 KB   │
│ 2026-11-20 13:58  ✘ size  Contra (USA)                   nes   -        │
│  ...                                                                    │
├─────────────────────────────────────────────────────────────────────────┤
│ ↑↓ move  f failed only  / filter  e export  enter details  esc back      │
└─────────────────────────────────────────────────────────────────────────┘
```

- `History.List` with the filter, newest first, loaded in pages of 100 as the cursor nears the end (no spinner for
  local data; `(loading…)` only if over 200 ms).
- `f` toggles failed only (`HistoryFilter` failed). `/` filters by text on title (client side over the loaded rows).
- `e` opens a prompt `Export to: [./mothdex-history-<ts>.csv]`; format inferred from the extension (txt/json/csv, as in
  the CLI export contract); an existing file asks `Overwrite? [y/N]`.
- `Enter` details: filename, link, path, SHA-256, crc state, attempts, duration, error code and its localized message.
  `d` on a failed row re-queues it (same as retry).
- Empty: `No downloads yet. Press / to search.`

## 7. Settings (`S`)

```
┌ mothdex ─ Settings ─────────────────────────────────────────────────────┐
│ Download                                                                │
│>Folder                    (profile default)                     default │
│ Concurrent downloads      2                                     default │
│ When file exists          skip  ▸                                file   │
│ Cover art                 on                                    default │
│ Preferred languages       En, Pt                                 file   │
│ Interface                                                               │
│ Language                  auto                                  default │
│ Color                     auto                                  default │
│ Results per page          10                                    default │
│ Confirm downloads         on                                    default │
│ Cache                     84 MB of 200 MB     [ Clear cache ]           │
├─────────────────────────────────────────────────────────────────────────┤
│ ↑↓ move  enter edit  space toggle  backspace reset  esc back             │
└─────────────────────────────────────────────────────────────────────────┘
```

- Rows come from `Config.List` (key, effective value, source `default|file|env|flag`); the last column shows the source,
  and `env`/`flag` rows are read-only with the reason in the status bar (`Set by MOTHDEX_DOWNLOAD_CONCURRENCY`).
- A curated subset is shown (the ones above); advanced keys (timeouts, hosts, log, cache TTLs) are CLI-only
  (`mothdex config`) to keep the screen short. A footer link `Advanced: run "mothdex config list"`.
- Editing: toggles flip on `Space`; enums open a small list; numbers and text open an inline field with the schema's
  range shown (`1-6`). `Config.Set` validates; an invalid value shows the error message inline and keeps the field open.
  Changes apply immediately (and persist, because `Set` writes the file); settings that need a restart
  (`interface.language`, `interface.color`) show `applies on next start`.
- `Backspace` on a row calls `Config.Reset(key)`.
- `Clear cache` asks `Clear cache (84 MB)? [y/N]`, then `Cache.Clear()` (service keeps `session`/`state`).
- Preferred languages/formats/sources open a tag editor (type, `Enter` adds, `Backspace` removes the last; order = priority);
  size preference is an enum `none|smallest|largest`.

## 8. Keyboard map

Global (when no text field is focused unless noted):

| Key | Action |
|---|---|
| `↑` `↓` / `k` `j` | move cursor |
| `←` `→` / `h` `l` | previous/next tab-like item or entry (Entry detail), collapse/expand (Queue) |
| `PgUp` `PgDn` / `Ctrl+B` `Ctrl+F` | page (list scroll or results page) |
| `Home` `End` / `g` `G` | first / last row |
| `Enter` | open / confirm |
| `Esc` | back, close overlay, cancel loading |
| `Space` | mark / toggle |
| `/` | go to Search (or filter in History) |
| `d` | download (marked, or highlighted) · `D` download best link |
| `Q` `H` `S` | queue, history, settings |
| `r` | random (Search), retry (Queue, error states) |
| `s` | cycle sort (Results) |
| `x` | cancel job · `X` cancel all |
| `?` | help overlay listing every key for the screen |
| `Ctrl+C`, `q` (root only) | quit (with confirmation when jobs are active) |
| `Ctrl+L` | redraw |
| In text fields | `Ctrl+A/E` start/end, `Ctrl+U` clear, `Ctrl+W` delete word, `Enter` submit, `Esc` leave field |

Mouse: optional enhancement (click to focus/select, wheel to scroll); nothing requires it. Vim keys are aliases only,
the arrow keys always work. No single-letter key is destructive; destructive actions need `X`, `Clear`, or a prompt.

## 9. Small terminals and resilience

| Size | Behavior |
|---|---|
| ≥ 80x24 | reference layout |
| 60-79 cols | drop columns Links, Regions, Size in that order (same as CLI); status bar shows 4 keys + `? more` |
| rows < 20 | hide the Search screen's `Recent` section and the Entry detail's `Reasons` column; Results shows the page footer in the title bar |
| rows < 16 or cols < 60 | replace the screen with `Terminal too small (need 60x16, have 54x14). Resize or press q.`; state is kept, the screen returns when resized |

- Layout recalculates on every resize message; focus and cursor are preserved by id, not row number.
- Wide characters are measured in cells; long titles truncate with `...`.
- Monochrome terminals / `--no-color`: use reverse video for the cursor row and `>` marker; marks `[x]`; states have
  text labels, so nothing depends on color. ASCII fallback frame and symbols per cli-conventions §4.
- Over SSH or slow links: redraws are diff-based by the framework; progress refresh stays ≤ 4/s.
- If stdout is not a TTY, `mothdex tui` exits with `usage`: `The TUI needs an interactive terminal.`
- Errors never crash the screen: any service error becomes the in-place error state with the localized message,
  hint and `r` to retry; `internal` shows the log path.
- Accessibility: every action has a visible label in the status bar or help; no motion beyond the spinner and the
  Warming counter; the cursor row is marked by `>` in addition to color/reverse video.
