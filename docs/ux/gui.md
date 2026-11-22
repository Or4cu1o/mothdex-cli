# UX: gamepad GUI (`mothdex-gui`, emulator mode)

Status: v1.5 (T-50; cli#71 review rules, PR-71-ux; Download anyway, T-56; GUI PR 3 rules and review, T-78; controller notices, T-80) · Owner: Bússola · Implementer: one engineer (E-M3) · Risk: Medium
Contract: [`gui.md`](../contracts/cli/gui.md) (Alicerce, T-51, PR #58), which owns the engineering detail behind this document.
Services: [`app-service.md`](../contracts/cli/app-service.md) only. Flows mirror [`tui.md`](tui.md); wording, units and the
Warming rules come from [`cli-conventions.md`](cli-conventions.md); link ranking from [`prefer-syntax.md`](prefer-syntax.md).
Platform facts: [T1e](../research/T1e-emulation-systems.md) (frontends, Ports), [T1b](../research/T1b-sdl2-purego.md) (SDL2,
gamepad, fonts), [`profiles.md`](../contracts/cli/profiles.md) (launcher, refresh). PLAN-v0 §2.6.

Visual direction note: the project has no `PRODUCT.md` or `DESIGN.md` yet. The color and type values in §1 are a
**functional baseline** (dark, high contrast, measured ratios) so the GUI can be built and verified. A later brand
direction pass with the owner may restyle them without changing any flow, size or rule in this document.

## 0. Principles

- **Everything with a gamepad.** Every screen and setting is reachable with the D-pad and four face buttons. A
  keyboard works too (§2.4); a mouse or touch screen is never needed.
- **One focused thing.** Exactly one element has focus at any time, and it is always visible (§3).
- **Same product as the TUI.** Same service calls, same states, same strings where the meaning is the same. The GUI
  adds covers, a virtual keyboard and a gamepad focus model.
- **Never lose a download behind navigation.** The queue badge is on every screen; quitting with active downloads
  always asks (§5.13).
- **No single press destroys anything.** Cancel, clear and reset go through an action sheet or a confirmation whose
  default focus is the safe option.
- **Calm screen.** No animation besides the loading spinner and the Warming counter; no sound. Redraw only on input,
  service events or the 1 s timer (handheld battery).

## 1. Display model

### 1.1 Size tiers

The layout uses the real window size (full screen, the display's current mode). Tiers come from the **logical
height** `H`:

| Tier | Logical height | Typical displays | Layout |
|---|---|---|---|
| `compact` | H < 600 | 640x480 handhelds, 854x480, 720x576 (PAL TV) | one column, no cover in Results |
| `standard` | 600 ≤ H < 900 | 1280x720 (reference), 1024x768, 1280x800 | side panel with cover |
| `large` | H ≥ 900 | 1920x1080, 1920x1200 | as standard, larger sizes |

- **Width class:** `narrow` when the logical width is below 800 (4:3 at 640x480, 720x720 square handhelds).
  Narrow changes the rules marked "narrow" below.
- **Above 1200 px tall** (1440p, 4K TVs): render the 1080 layout with an integer render scale `k = floor(H / 1080)`
  (`SDL_RenderSetScale`), so text stays sharp and the layout does not grow emptier.
- **Below 640x480** (for example 480x320 handhelds on EmuELEC or ROCKNIX): do not refuse. Draw the 640x480 `compact`
  layout and let SDL scale it down to the panel (`SDL_RenderSetLogicalSize`, letterboxed). Legibility at that size
  is **Unverified** and goes to the community beta (D4).
- A display change while running (HDMI plugged into a handheld dock) re-runs the tier choice; focus is kept by id.

### 1.2 Safe area (TVs)

All content, bars and dialogs live inside the **safe rectangle**; only the background fills the whole screen.

- Config `gui.safe_area`: `auto` (default) or an integer percent `0`–`10`, applied to every edge.
- `auto` = 5 % when the display is at least 1280x720 (usually HDMI to a TV), else 0 % (built-in handheld panels).
- Settings › Screen margin opens a **calibration view**: four corner brackets and a frame at the current margin.
  D-pad Left/Right changes it live in 1 % steps, A saves, B leaves without saving (the old value comes back).
  - Brackets: an L at each corner of the margin being tried, arms 4u long, with the focus ring's stroke and color.
  - The live percent is centered in the largest step of the type scale, in `text`; under it, in `secondary` and
    `text.dim`: `Adjust until you can see all four corners.`
  - The hint bar (`Change`, `Save`, `Cancel`) is drawn inside the margin being tried, so it moves with it.

### 1.3 Type scale (pixels at the logical height; one embedded font, Latin + Cyrillic)

| Role | compact | standard | large | Use |
|---|---|---|---|---|
| `title` | 26 | 34 | 50 | screen title in the top bar, entry title in detail |
| `body` | 20 | 26 | 38 | list rows, buttons, keyboard keys, dialog text |
| `secondary` | 17 | 21 | 31 | second line of rows, metadata, hints |
| `minimum` | 16 | 18 | 27 | the smallest text anywhere (badges, counters) |

- `gui.text_size`: `normal` (default) or `large`. Large multiplies every size and row height by 1.25 (rounded to
  an even pixel); lists then show fewer rows. Applies at once.
- One regular weight; emphasis by size and color only. Line height 1.25 x size.
- Titles longer than the row are cut with `…` at the end; the full title is always shown on the Entry screen,
  wrapped to at most 3 lines.
- Glyphs missing from the font (CJK, Devanagari in a title) draw the font's replacement box; the title stays
  selectable and downloadable.
- Interface languages the font cannot render (`zh_cn`, `ja_jp`, `hi_in`): the GUI uses `en_us` strings and shows once
  on Home `This language is not available in this screen mode yet. Showing English.` as a `warn` toast that stays
  6 s (§4; CJK and Devanagari: backlog).
- **Symbols are shapes.** The embedded font has no arrows, check marks or crosses, so `↓`, `✔` and `✘` are drawn as
  shapes in the text color, sized to the cap height of the text beside them, with a gap of one space. The D-pad
  arrows (`◀ ▶ ▲ ▼` in the hint bar and the Buttons overlay) are filled triangles drawn inside the button pill.
- **No string is cut except titles** (entry titles, and the screen title in the top bar as §4 allows). Other text
  wraps (at most 2 lines unless a screen says otherwise); a path that still does not fit is shortened in the middle,
  keeping the first and the last folder (`/home/someone/…/collection`).

### 1.4 Spacing and sizes

| Token | compact | standard | large |
|---|---|---|---|
| `space.unit` (`u`) | 8 | 10 | 16 |
| top bar height | 36 | 48 | 68 |
| hint bar height | 36 | 48 | 68 |
| list row, one line | 40 | 52 | 76 |
| list row, two lines | 52 | 72 | 104 |
| home tile (w x h) | 180 x 96 | 280 x 150 | 420 x 220 |
| keyboard key (min square) | 48 | 60 | 88 |
| focus ring width | 3 | 4 | 6 |
| progress bar height | 8 | 10 | 14 |
| corner radius (tiles, rows, keys, dialogs) | 6 | 8 | 12 |

Padding inside rows and tiles is `2u` horizontal and `1u` vertical. Gaps between tiles and keys are `1u`. The content
area has `2u` padding inside the safe rectangle.

### 1.5 Color tokens (functional baseline, dark)

| Token | Hex | Use |
|---|---|---|
| `color.bg` | `#0F1317` | full-screen background |
| `color.surface` | `#1A2027` | rows, tiles, keys, bars, dialogs |
| `color.focus.bg` | `#123447` | fill of the focused element |
| `color.focus.ring` | `#5CC8FF` | ring of the focused element (same hue as the CLI `accent`) |
| `color.text` | `#F1F4F6` | primary text |
| `color.text.dim` | `#A3AFBA` | secondary text, hints, disabled labels |
| `color.accent` | `#5CC8FF` | progress bar, selected checkmarks, the default (`*`) link |
| `color.ok` | `#5DD68C` | Done, `✔ Downloaded` |
| `color.warn` | `#F4B740` | Warming, RetryWait, stale, offline |
| `color.error` | `#FF7A7A` | Failed, error messages |
| `color.scrim` | `#0F1317` at 80 % | behind dialogs and the Start menu |

Measured contrast (WCAG 2.x formula):

| Foreground | on `bg` | on `surface` | on `focus.bg` |
|---|---|---|---|
| `text` | 16.9 | 14.9 | 11.8 |
| `text.dim` | 8.3 | 7.3 | 5.8 |
| `accent` / `focus.ring` | 9.9 | 8.7 | 6.9 |
| `ok` | 10.2 | 9.0 | 7.1 |
| `warn` | 10.4 | 9.1 | 7.3 |
| `error` | 7.4 | 6.5 | 5.2 |

Every text pair passes AA (4.5:1), and `text` passes AAA (7:1) on all three backgrounds. The focus ring passes the
3:1 non-text rule against both `bg` and `surface`. Color never carries meaning alone: every state also has a word or
symbol (`✔`, `✘`, `!`). Disabled elements use `text.dim` and a `(not available)` reason in the hint bar, not just a
grey tone.

Elements that sit on a `surface` bar or panel use `color.bg` as their fill: the button discs of the hint bar and the
unfocused dialog buttons (`text` on `bg`, 16.9:1). The `bg`/`surface` edge is decorative (about 1.1:1): each such
element is identified by its label, and the focused one also has the `focus.ring` (8.7:1 against `surface`).
A disc or pill drawn straight on `bg` (the `(A) Continue` hint of the exit screen, §5.13) uses `surface` instead, so
it always differs from what it sits on.

## 2. Input model

### 2.1 Buttons by position

SDL GameController buttons are read **by position** (SDL_GameControllerDB names: `a` = south, `b` = east,
`x` = west, `y` = north). PLAN §2.6 names them in Xbox style.

| Position (SDL) | Default role | Name in this doc |
|---|---|---|
| south (`a`) | confirm, open, toggle | **A** |
| east (`b`) | back, close, delete a character | **B** |
| west (`x`) | download (the screen's main action) | **X** |
| north (`y`) | info, details, secondary toggle | **Y** |
| `leftshoulder`, `rightshoulder` | page up / page down | **L**, **R** |
| `start` | Start menu | **Start** |
| `back` (Select) | open the Queue | **Select** |
| D-pad, left stick | move focus | **D-pad** |

- `gui.confirm_button`: `south` (default) or `east`. `east` swaps only A and B, for people used to the Japanese and
  Nintendo convention. There is no automatic swap from the controller type.
- **Button hints** show the label printed on the controller at that position, from `SDL_GameControllerGetType`:
  Xbox and unknown pads `A B X Y`; Nintendo pads `B A Y X` (south is B on a Nintendo pad); PlayStation pads the words
  `Cross`, `Circle`, `Square`, `Triangle` (no shape glyphs in the font). Shoulders `L`/`R` (`LB`/`RB` on Xbox,
  `L1`/`R1` on PlayStation). Start and Select show `Start`/`Menu` and `Select`/`View` per type.
- Triggers, stick clicks and the right stick are not used (many handhelds lack them).
- **Select acts on release**, and only when no other button was pressed while it was held. Frontends use Select as
  the hotkey modifier (Batocera hotkey + Start closes a Port), and this rule keeps a hotkey combo from also opening
  the Queue.

### 2.2 Movement, repeat and stick

- D-pad and stick move focus one step. Holding repeats: first repeat after 400 ms, then every 80 ms; after 2 s of
  holding, every 40 ms (long lists).
- Stick: a direction counts when its axis passes 50 % of the range and is released below 35 % (hysteresis), using
  the dominant axis only. No diagonal movement.
- L/R move one visible page of rows in lists (and the text cursor on the keyboard screen, §5.4). Holding repeats like
  the D-pad.
- Input arriving while a dialog is opening is ignored for 150 ms, so a held A cannot confirm a dialog it just opened.

### 2.3 Controllers

- Mappings: the embedded SDL_GameControllerDB, then a user file `<data dir>/gamecontrollerdb.txt`, then
  `SDL_GAMECONTROLLERCONFIG` from the frontend (later entries win, so the frontend's mapping for its Ports is kept).
  Details in the contract.
- **Any** connected controller drives the GUI (in a two-player TV setup, whoever holds a pad). Hot-plug works at any
  time.
- **No controller** (none after SDL's first device report at start, or the last one unplugged):
  - A `warn` toast for 6 s (§4), shown once each time the state appears: `Connect a controller. On a keyboard:
    arrows, Enter, Esc.` (`ui.gui.pad.none`).
  - While no pad is connected, the top bar also shows the `warn` badge `No controller` (`ui.gui.bar.no_pad`). It is
    never cut, like `Offline`, and goes away when a pad connects.
  - The screen underneath keeps working with a keyboard.
- **A joystick SDL cannot map:** a `warn` toast for 6 s, once per device per session: `Controller not recognized:
  {name}. See Settings › About.` (`ui.gui.pad.unmapped`).
  - The toast has no path: a path shortened to fit could not be typed.
  - The name may wrap; past 2 lines only the name is cut with `…`.
- **Settings › About**, for each connected pad:
  - `Controller: <name>`, then `GUID: <guid>`.
  - An unmapped pad adds, under its name, `Not recognized. Add a mapping to <data dir>/gamecontrollerdb.txt.` with
    the full path, wrapped and never shortened.
  - With no pad: `Controller: None connected`.
  - Where purego cannot read the 16-byte GUID (32-bit ARM and x86 Linux, 32-bit Windows): `GUID: Unavailable on this
    system`.
- Toasts are shown one at a time; a notice waits for the toast on screen to end.

### 2.4 Keyboard fallback (TV boxes with a keyboard, desktop testing)

| Key | Acts as |
|---|---|
| Arrows | D-pad |
| Enter | A |
| Esc | B |
| Backspace | delete a character (keyboard screen), else B |
| `x`, `y` | X, Y (outside the keyboard screen) |
| PgUp, PgDn | L, R |
| Tab | Start |
| `q` | Select (outside the keyboard screen) |
| typing | goes straight into the search field while the keyboard screen is open |

## 3. Focus model

- **Focus regions.** Each screen is a vertical stack of regions (for example Search: filter chips, then keyboard).
  Inside a region the D-pad moves between neighbors by geometry: the nearest element whose center lies in that
  direction. Leaving a region edge enters the adjacent region at the element closest to the current horizontal
  position.
- **Edges.** Lists do not wrap: at the top or bottom, nothing happens. Rows of tiles and keyboard rows wrap
  horizontally.
- **Initial focus** is defined per screen (§5). Returning to a screen restores its last focus by element id (slug,
  job id, setting key), not by row number. If that element is gone, focus goes to the nearest remaining row.
- **Back stack.** B goes back one level; the stack is at most Home › Search › Results › Entry. Queue, History and
  Settings open on top of whatever screen is showing, and B returns to it. B on Home asks to quit (§5.13).
- **Focus visibility.** The focused element has the `focus.bg` fill and a `focus.ring` border (§1.4 width), drawn
  outside the element so it never covers text. Lists scroll so the focused row keeps at least one row of context
  above and below (except at the ends).
- **Dialogs and the Start menu** trap focus until closed; B closes them (equal to the cancel option).

## 4. Frame

```
┌ safe area ─────────────────────────────────────────────────────────────┐
│ mothdex › Results "zelda"                     Offline   ↓ 2 · 1 queued │ top bar
├────────────────────────────────────────────────────────────────────────┤
│                                                                        │
│                          (screen content)                              │
│                                                                        │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu                  (A) Open  (X) Download  (Y) Info  (B) Back│ hint bar
└────────────────────────────────────────────────────────────────────────┘
```

- **Top bar:** `mothdex › <screen>` (and the query on Results), then on the right, in this order: `Offline` (warn,
  when the last catalog call failed with `network` and no later call succeeded), `Will quit when downloads finish`
  (§5.13), and the **queue badge** `↓ 2 · 1 queued` (hidden when the queue is idle; `↓ 1` alone when nothing is queued).
- **Top bar fit** (every tier, text size and language; nothing leaves the bar or the safe rectangle):
  1. The title keeps at least the width of the screen name (`mothdex` on Home, the last crumb elsewhere). Status
     items never start left of that width plus `2u`.
  2. When the status items do not fit, `Will quit when downloads finish` switches to its short form
     `ui.gui.bar.will_quit_short` (en `Quit pending`, pt_br `Saída pendente`, es_es `Salida pendiente`, ru_ru
     `Ожидается выход`).
  3. If they still do not fit, only that item is cut with `…`. `Offline`, `No controller` (§2.3) and the queue badge
     are never cut.
  4. Whatever room is left goes to the title, cut at the end with `…` (crumbs are dropped from the left first:
     `… › Results "zelda"`).
- **Hint bar:** the screen's actions, at most 5, right-aligned in the order A, X, Y, B, with `Start Menu` on the left.
  Each hint is the controller label on a `bg` disc (`text` letter, §1.5) plus the action word in `text.dim`. A label
  longer than one letter (`Start`, `Cross`, `Circle`) becomes a pill with `1u` padding on each side. `compact` shows
  at most 4 (Start keeps the pill and drops the word when Start means `Menu`). Actions that do nothing on the focused
  element are hidden, not greyed.
- **Hint bar fit** (every tier, text size and language; nothing overlaps the Start pill or leaves the safe
  rectangle; words are never cut):
  1. Where Start is the screen's main action (`Search` on Search, `Done` on the multi-select pickers), its word
     stays in every tier.
  2. When the hints do not fit, they are dropped in this order: `L`/`R`, then the Start word when it means `Menu`,
     then `Y`, then `X`. `A` and `B` are never dropped.
  3. A dropped action still works; Start › Buttons lists the whole map (§5.11).
- **Toast:** under the top bar, right-aligned, `surface` with a 4 px left stripe in the state color. Its width is at
  most 60 % of the safe rectangle; longer text wraps to 2 lines (never cut). When it would cover the focused element
  (with its ring), it moves above the hint bar instead, still right-aligned. It stays 3 s; the locale notice (§1.3)
  stays 6 s. At most one at a time; a new one replaces the old.
- **Loading:** a spinner (a ring, one turn per second) to the right of the text `Searching…`, `Loading…`, `Starting…`
  or `Updating the game list…`, `1u` away. It appears only after 200 ms, so cached answers never flash.

## 5. Screens

Navigation map:

```
Splash ─▶ Home ─A Search─▶ Search (keyboard) ─Start─▶ Results ─A─▶ Entry ─X / A on a link─▶ (queued, toast)
            ├─ A Platforms ─▶ Platform list ─A─▶ Results (platform, empty text)
            ├─ A Random ─▶ Results (Random picks)
            └─ A Queue / History / Settings          Start menu and Select (Queue) from any screen
```

### 5.1 Splash (start from the Port)

```
┌────────────────────────────────────────────────────────────────────────┐
│                                                                        │
│                               mothdex                                  │
│                            Starting…  (spinner)                        │
│                                                                        │
│                                                   0.3.0 · Batocera     │
└────────────────────────────────────────────────────────────────────────┘
```

- The first frame appears as soon as the window exists (target: within 1 s of process start on a Pi 4 class device;
  Unverified on hardware). It shows the version and `ActiveProfile().Name`.
- In the background: `DBInfo`, `History` (for the downloaded marks), `ConfigList`. Home opens when `DBInfo` answers
  or after 3 s, whichever comes first; a late `DBInfo` fills the Home footer when it arrives.
- `DBInfo` fails with `network`: Home opens with `Offline` in the top bar and the footer `Catalog unavailable. Cached
  searches still work.`

### 5.2 Home

```
┌ mothdex ────────────────────────────────────────────── ↓ 1 ────────────┐
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                     │
│  │   Search    │  │  Platforms  │  │   Random    │                     │
│  └─────────────┘  └─────────────┘  └─────────────┘                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                     │
│  │    Queue  1 │  │   History   │  │  Settings   │                     │
│  └─────────────┘  └─────────────┘  └─────────────┘                     │
│  Recent   [ zelda ]  [ metroid ]  [ chrono trigger ]                   │
│  1,284,019 entries · Batocera · /userdata/roms                         │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu                                        (A) Open  (B) Quit  │
└────────────────────────────────────────────────────────────────────────┘
```

- Tiles in a 3 x 2 grid; `narrow`: 2 x 3. Initial focus: Search. The grid is left-aligned in the content area (the
  right side stays empty on wide screens). A tile label wraps to 2 lines at its spaces; a single word wider than the
  tile (`Configurações` with large text) drops to `secondary`, then `minimum`, and is cut only if even that does not
  fit.
- **Recent:** the last 5 query texts, in the same file and format as the TUI (`<paths.data>/tui_state`), so both UIs
  share them. A on a chip runs that search with no filters. Hidden when empty, and in `compact` when the screen has
  fewer than 7 rows of space. Recent has priority over the full tile height: tiles shrink (down to 2 `body` lines)
  to keep it; chips that do not fit the row are left out.
- Footer: catalog size from `DBInfo`, profile name, ROM root. When the profile is `generic`, the footer reads
  `No emulation system detected. Games go to <path>.` (warn color), and Settings › System explains why
  (`ProfileInfo.Warnings`). The footer wraps to at most 2 lines (layout reserves both); a path that still does not
  fit is shortened in the middle (§1.3), so the folder is always readable.
- **Random:** `Random{Count: 10}` with no filters; the Results header reads `Random picks`.

### 5.3 Search

```
┌ mothdex › Search ──────────────────────────────────────────────────────┐
│  [ zelda link_                                              ]  10/80   │
│  ( Platform: snes, gbc )  ( Region: any )  ( Search )                  │
│  ┌──┬──┬──┬──┬──┬──┬──┬──┬──┬──┐                                       │
│  │1 │2 │3 │4 │5 │6 │7 │8 │9 │0 │                                       │
│  │q │w │e │r │t │y │u │i │o │p │                                       │
│  │a │s │d │f │g │h │j │k │l │' │                                       │
│  │z │x │c │v │b │n │m │- │. │& │                                       │
│  │ àé    │     space      │ ⌫ │ Search │                              │
│  └───────┴────────────────┴───┴────────┘                               │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Search  (L)(R) Cursor   (A) Type  (X) Space  (Y) Layout  (B) Del│
└────────────────────────────────────────────────────────────────────────┘
```

- Regions: **filter chips** (Platform, Region, Search button) above, **keyboard** below. The text field is not a focus
  target; the keyboard edits it. Initial focus: the `q` key when the field is empty, the `Search` key when it has
  text (re-editing from Results).
- Platform and Region chips open the pickers of §5.5 (multi-select). Their labels list the chosen ids, cut with `…`.
- **Run the search:** Start, the `Search` key, or the Search chip. It calls `Catalog.Search` (which writes the session,
  as in every UI) and opens Results.
- Empty text with no platform and no region: inline message under the field `Type something or pick a platform.`,
  and no call (same rule as the TUI).
- B on Results returns to the previous screen; Search with the query filled is reached with Y on Results.

### 5.4 Virtual keyboard

**Layout.** A 10 x 4 key grid plus a function row, lowercase only (search is case-insensitive):

| Row | Keys |
|---|---|
| 1 | `1 2 3 4 5 6 7 8 9 0` |
| 2 | `q w e r t y u i o p` |
| 3 | `a s d f g h j k l '` |
| 4 | `z x c v b n m - . &` |
| 5 | `layout` (2 keys wide) · `space` (4 wide) · `⌫` (1) · `Search` (3) |

**Layouts** (Y or the `layout` key cycles them; the key shows the name of the next one):

| Layout | Rows 1-4 |
|---|---|
| `abc` | the grid above |
| `àé` | `á à â ã ä å æ ç é è` / `ê ë í ì î ï ñ ó ò ô` / `õ ö ø œ ú ù û ü ý ß` / `: ! ? , ( ) / + # _` |
| `абв` | `й ц у к е н г ш щ з` / `х ъ ф ы в а п р о л` / `д ж э я ч с м и т ь` / `б ю ё . - ' & : ! ?` |

The Cyrillic layout is in the cycle only when the interface language is `ru_ru` or a Cyrillic letter is already in
the field; otherwise the cycle is `abc → àé`.

**Buttons on this screen:**

| Button | Action |
|---|---|
| D-pad | move on the keys (wraps left/right, not up/down; up from row 1 goes to the chips) |
| A | type the focused key (or run the function key) |
| B | delete the character before the cursor; when the field is empty, go back |
| X | space |
| Y | next layout |
| L / R | move the text cursor left / right |
| Start | run the search |

- Holding B repeats deletion (§2.2 timing). Holding A on a letter repeats it.
- Maximum 80 characters. At the limit, A on a character key does nothing and the hint bar shows `(not available)`.
  The counter `10/80` is `text.dim`, and turns `warn` at 70 and above.
- Focus stays on the key just typed (no auto-jump), so repeated letters are easy.
- A physical keyboard types straight into the field while this screen is open; Enter runs the search.
- The keyboard never sends the query while typing (no live search: each search is a network call).

### 5.5 Pickers: Platform and Region

```
┌ mothdex › Platform ─────────────────────────────── 2 selected ─────────┐
│  Nintendo                                                              │
│   [x] Super Nintendo                                   snes            │
│   [ ] Nintendo Entertainment System                    nes             │
│   [x] Game Boy Color                                   gbc             │
│  Sega                                                                  │
│   [ ] Mega Drive / Genesis                             md              │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Done  (L)(R) Group      (A) Select  (Y) Clear all  (B) Cancel   │
└────────────────────────────────────────────────────────────────────────┘
```

- Data: `Catalog.Platforms` grouped by `Brand` (groups sorted by name, platforms by name inside them), and
  `Catalog.Regions` sorted by name. Loaded on first open and kept for the process.
- In Search (multi-select): A toggles, Y clears all, Start applies, B cancels the changes. L/R jump to the previous
  or next brand group.
- From Home › Platforms (single select, no checkboxes): A opens Results for that platform with empty text.
- Load failure: `Couldn't load the list.` with `(A) Retry`, and the chip keeps its previous value. (The TUI falls
  back to typed ids; on a gamepad, retrying is easier than typing ids.)

### 5.6 Results

```
┌ mothdex › Results "zelda" · snes ──────────────────────── ↓ 1 ─────────┐
│ ┌ Legend of Zelda, The: A Link to the… ┐  ┌───────────────────┐        │
│ │ snes · us,eu · 1.0 MB      ✔ Downloaded│  │                   │        │
│ └──────────────────────────────────────┘  │      (cover)      │        │
│   Zelda no Densetsu: Kamigami no Trif…    │                   │        │
│   snes · jp · 1.0 MB                      └───────────────────┘        │
│   Link's Awakening DX                     Legend of Zelda, The:        │
│   gbc · us · 1.0 MB                       A Link to the Past           │
│                       37 results · cached 3h ago                       │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu  (L)(R) Page  (A) Open  (X) Download  (Y) Edit search  (B) │
└────────────────────────────────────────────────────────────────────────┘
```

- **Rows** (two lines): the title; then `platform · regions · size` of the top-ranked link in `text.dim`, and
  `✔ Downloaded` (ok) when the History has a `Done` record for that slug. Only the title is ever truncated.
- **One continuous list.** The GUI calls `Search` with `PageSize: 25` and appends the next service page when the focus
  reaches the last 5 loaded rows (a `Loading more…` row shows while it loads). It keeps at most 200 rows. Beyond
  that the first page drops off, and comes back from the cache if the person scrolls up. L/R move one visible page
  of rows. The `#` index is not shown: the GUI acts with `Ref{Slug}`, as the TUI does.
- **Cover panel** (`standard` and `large`, not `narrow`): 35 % of the content width on the right. It shows the focused
  row's cover fitted inside (aspect kept), with the title wrapped to 2 lines below it. The cover is requested through
  `Catalog.Cover` 250 ms after focus settles on a row, so there are no requests while scrolling fast. Covers decoded
  in this process stay in a memory cache of the last 50. No cover or a decode error: a `surface` placeholder with the
  platform id in `text.dim`. In `compact` or `narrow` there is no panel and the list takes the full width.
- **Buttons:** A opens Entry. X downloads the row with the default ranked link (`LinkSelector{Prefer}`), and the toast
  names the link: `Queued: Link's Awakening DX (link 1)`. Y goes to Search with the query and filters filled. B goes
  back.
- **Footer line** (inside the content, `text.dim`): `37 results` and `cached 3h ago`, or `stale: served from cache`
  in warn. When `SearchPage.Dropped > 0`, it adds `3 results were hidden because their slug has an unexpected form.`
- **States:** loading (spinner; B cancels the request context and goes back); empty
  `No results for "xyz". Try fewer words, or remove filters.` with `(Y) Edit search`; error (§6) with `(A) Retry`.
  `PastLastPage` cannot happen in the continuous list, because the GUI stops at `TotalPages`.
- **Random picks:** no paging; the header reads `Random picks`, and Y on this screen means `New picks` (another
  `Random` call).

### 5.7 Entry detail and link chooser

```
┌ mothdex › Entry ───────────────────────────────────────── ↓ 1 ─────────┐
│ ┌──────────────┐  Link's Awakening DX                                  │
│ │              │  gbc · Game Boy Color · us · serial -                 │
│ │   (cover)    │                                                       │
│ │              │  Links                                ranked: lang=En │
│ └──────────────┘ ┌ * Game  gbc   1.0 MB   En        Myrient      ┐     │
│                  └──────────────────────────────────────────────┘     │
│                     Game  gbc   1.0 MB   En,Rev A  Myrient            │
│                     Demo  gbc   0.2 MB   -         Archive            │
│                     Game  gbc   1.0 MB   archive.org  (not available) │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu (L)(R) Prev/next game (A) Download this (X) Download best  │
└────────────────────────────────────────────────────────────────────────┘
```

- Data: `Catalog.Entry(Ref{Slug})` and `Catalog.Links(ref, {Prefer})`. The platform name comes from the cached
  `Platforms` list.
- Cover: 30 % of the content width on the left (`narrow`: top-left, with the links below the metadata). Same loading
  rule and placeholder as §5.6. Showing a cover never saves a file.
- Focus starts on the default link (the `*` in `color.accent`). Unavailable links (`Available: false`) are listed last,
  dimmed, with `(not available)`, and the D-pad skips them.
- **Buttons:** A downloads the focused link (`LinkSelector{Index}`). X downloads the default link without moving
  focus. Y toggles the **Why** line under each link (`LinkChoice.Reasons`, `text.dim`). L/R move to the previous or
  next entry of the Results list. B goes back. The person stays on the screen after queueing, to queue more.
- Already downloaded (History `Done` for this slug and link index): the link shows `✔ Downloaded`, and A or X asks
  `Already downloaded. Download again?` with `[Cancel]` (default focus) and `[Download again]`. The file is then
  handled by `download.on_exists`, as in every UI.
- No links: `No links available for this entry.`, and A and X are hidden from the hint bar.
- **Confirmation** (`interface.confirm_downloads`): only for a link above 500 MB:
  `Download Metal Gear Solid (USA) (700 MB) to /userdata/roms/psx?` with `[Cancel]` and `[Download]`. Default focus is
  Download, since the person just pressed download; B cancels.
- The TUI's per-session preference tweak (`P`) is not in the GUI; preferences are set in Settings.

### 5.8 Download queue (Select, or Home › Queue)

```
┌ mothdex › Queue ───────────────────────────────── 2 active · 1 queued ─┐
│ ┌ Super Mario Bros. 3 (USA).nes                                     ┐ │
│ │ ████████████████░░░░░░░░░░  58%  222 KB / 384 KB  1.2 MB/s  0:01 │ │
│ └───────────────────────────────────────────────────────────────────┘ │
│   Metroid (USA).nes                                                    │
│   Warming up the server… 02:14                                         │
│   The server is fetching this file from peers. This is normal.         │
│   Zelda II (USA).nes                                          Queued   │
│   ── Finished (2) ──────────────────────────────────────────────────── │
│   ✔ Kirby's Adventure (USA).nes   Saved to /userdata/roms/nes   12s    │
│   ✘ Contra (USA).nes   The downloaded file has the wrong size          │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu                               (A) Actions  (Y) Details  (B)│
└────────────────────────────────────────────────────────────────────────┘
```

- Source: `Downloads.Jobs()` when opened, and one `Downloads.Events` subscription for the whole process (it also
  drives the queue badge). State texts, the Warming counter (its own 1 s UI timer) and its hint after 30 s come from
  cli-conventions §5 unchanged. Retry wait reads `Retrying in 8s (attempt 2/5)`.
- Order: active, then queued, then **Finished** (newest first, kept for the process; the History keeps them for good).
- **A opens an action sheet** for the focused job, never a direct cancel:
  - active or queued: `Cancel download` · `Details` · `Close`
  - failed or canceled: `Retry` (`Enqueue{Ref{Slug}, LinkSelector{Index: job.LinkIndex}}`) · `Details` · `Close`
  - stopped with `confirmation_required`: `Download anyway` · `Details` · `Close` (see **Download anyway** below)
  - done or skipped: `Details` · `Hide from list` · `Close`
- Y opens **Details** directly: file name, path, size, SHA-256 (first 16 hex digits, then `…`), crc state, attempts,
  duration, and the error message with its code.
- From the Start menu on this screen: `Cancel all downloads` (confirm `Cancel 3 downloads?` with `[Keep]` as default
  and `[Cancel all]`) and `Hide finished`.
- When the last job ends, a toast reads `3 downloaded, 1 skipped, 0 failed`.
- Empty: `No downloads. Press X on a game to download it.`
- With `download.boxart` on, the GUI calls `SaveJobBoxart` after each `Done` job, as the CLI does. A failure is a
  toast in warn, never a job failure.

**Download anyway** (`confirmation_required`, T-56). With `interface.confirm_downloads` on, a job stops before its
first body byte when the proxy's real size crosses 500 MiB and the catalog size was below it (downloader.md §3.2 step
6). Nothing failed: the job waits for a person.

- **Copy states facts only**: the real size (`{size}`, the error's `size`, else `Expected`), the catalog size
  (`{estimate}`, `CatalogSize`) and the folder (`{dir}`). Never "much larger": 499 MiB to 501 MiB also trips the rule.
  Keys are `ui.consent.*`; each text has an `_unknown` variant without the catalog part, for `CatalogSize` 0.
- **Row**: warn color with `!` (a shape, §1.3), never error with ✘: `Needs your OK: the file is 712.0 MB, listed as
  43.0 MB.` The sentence wraps to 2 lines (the row is 3 lines tall); only the title may be cut.
- **Action sheet**: `Download anyway` · `Details` · `Close`. `Retry` is not offered, since it would only stop again.
  The focus starts on `Download anyway`, which only opens the dialog.
- **Dialog** (§5.12): title `This file is 712.0 MB, not 43.0 MB.`, detail `The catalog listed a smaller size. It will
  be saved to {dir}.` with the folder shortened in the middle (§1.3). Buttons `[Don't download]` (left, initial focus,
  and B) and `[Download 712.0 MB]`. Don't download leaves the row as it is. Download queues the same link again with
  `SizeConsent` equal to the size shown, and the new job takes the stopped row's place in the list and the counts.
- **Nothing implies consent**: no bulk action queues these jobs; each needs its own OK. If the proxy then reports
  another size, the job stops again and the row shows the new size, with no extra text.
- **Toast** when a job stops outside the Queue: `Needs your OK: {title}` in warn. The title comes last, so a cut falls
  on it.
- **Counts**: these jobs are never counted as failed. The summary toast becomes `2 downloaded, 0 skipped, 0 failed, 1
  waiting for your OK` (`ui.tui.summary_waiting`, only when there is one) in warn; the exit screen adds `1 waiting for
  your OK`.
- **Details**: `Size` is the real size, and the line is `Status: <row text> [confirmation_required]`, not `Error`.

### 5.9 History (Home › History or Start menu)

```
┌ mothdex › History ─────────────────────────── All · 128 records ───────┐
│ ┌ ✔ Kirby's Adventure (USA)                          nes   384 KB    ┐ │
│ └   2026-11-20 14:03                                                 ┘ │
│   ✘ Contra (USA)                                     nes   -           │
│     2026-11-20 13:58 · wrong size                                      │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu  (L)(R) Page        (A) Details  (Y) Failed only  (B) Back │
└────────────────────────────────────────────────────────────────────────┘
```

- `History{Limit: 0}` loaded on open and after each terminal event, newest first. Rows are drawn lazily, with no
  spinner for local data unless it takes over 200 ms.
- Y toggles `All` and `Failed only` (client side, over the loaded list); its hint reads `Failed only` or `Show all`.
  The top bar shows the filter and the count: `All · 128 records`.
- The second line of a row is `{date} · {reason}`. The reason is a short text without placeholders, in the
  interface language:
  - `ui.gui.history.why.<code>` for the codes whose message needs values a record does not keep: `size_mismatch`,
    `disk_full`, `permission_denied`, `exists`, `path_rejected`, `unsafe_name`, `untrusted_url`,
    `untrusted_redirect`, `upstream`, `not_found` and `unmapped_platform`;
  - otherwise the localized `errors.<code>.message`;
  - for an unknown code, `Failed`.
- Every row is 3 lines tall: the fixed height keeps paging simple. The reason may wrap to 2 lines; only past that does
  it end in `…` at a word boundary. A record stopped with `confirmation_required` shows the Queue's consent row in warn with
  `!` (§5.8), with the sizes from the record's `content_length` and `catalog_size`.
- Details has the full text: the short reason, then the stored message and `[code]` on the next line. It has no
  `Size` line when nothing was written.
- A opens Details (the same panel as the Queue) with `[Download again]` (initial focus; re-queues with the original
  slug and link index) and `[Close]`; B closes. For a `confirmation_required` record the first button is
  `Download anyway`, which opens the dialog of §5.8 with the record's sizes. If that slug and link is already in the
  Queue, a toast says `Already queued again.` and nothing is queued.
- No export and no text filter in the GUI (the CLI and TUI have them).
- Empty: `No downloads yet. Search for a game to start.` With `Failed only` and no failed record:
  `No failed downloads.`, and Y still works.

### 5.10 Settings (Home › Settings or Start menu)

```
┌ mothdex › Settings ────────────────────────────────────────────────────┐
│ Downloads                                                              │
│ ┌ Concurrent downloads                          ‹ 2 ›        default ┐ │
│ └──────────────────────────────────────────────────────────────────── ┘ │
│   When a file exists                            Skip          file     │
│   Save cover art                                On            default  │
│   Preferred languages                           En, Pt        file     │
│   Preferred size                                Any           default  │
│ Screen                                                                 │
│   Confirm button                                Bottom (A)    default  │
│   Text size                                     Normal        default  │
│   Screen margin                                 Auto (5%)     default  │
│   Language                                      Auto          default  │
│   Confirm large downloads                       On            default  │
│ Storage and system                                                     │
│   Cache                                         84 MB  ( Clear )       │
│   System                                        Batocera · /userdata/roms │
│   About                                         0.3.0                  │
├────────────────────────────────────────────────────────────────────────┤
│ (Start) Menu              (◀▶) Change  (A) Edit  (X) Reset  (B) Back    │
└────────────────────────────────────────────────────────────────────────┘
```

Rows come from `ConfigList` (value and source). Keys:

| Row | Key | Editor |
|---|---|---|
| Concurrent downloads | `download.concurrency` | stepper, Left/Right `1`–`6`; applies on next start (F3) |
| When a file exists | `download.on_exists` | list `Skip`, `Rename`. `overwrite` is not offered (it needs `--force`); a file value of `overwrite` shows `Overwrite` read-only with `Change it from the command line.` |
| Save cover art | `download.boxart` | toggle (A), applies now |
| Preferred languages | `download.prefer.languages` | ordered list editor (below), applies now; an empty list shows `Any` |
| Preferred size | `download.prefer.size` | list `Any`, `Smallest`, `Largest`, applies now |
| Confirm button | `gui.confirm_button` | list `Bottom (A)`, `Right (B)` with the pad's own labels (config values stay `south` and `east`), applies now |
| Text size | `gui.text_size` | list `Normal`, `Large`, applies now |
| Screen margin | `gui.safe_area` | calibration view (§1.2), applies now |
| Language | `interface.language` | list `Auto` plus the four rendered locales by their own names (`English`, `Português (Brasil)`, `Español (España)`, `Русский`); applies on next start |
| Confirm large downloads | `interface.confirm_downloads` | toggle, applies now |
| Cache | — | `CacheStats` total; A opens `Clear cache (84 MB)?` with `[Keep]` as default and `[Clear]`, then `CacheClear()` |
| System | — | read-only: `ActiveProfile()` name, ROM root, `Verified` and each warning |
| About | — | read-only: version, catalog entries, controller name and GUID, log file path |

- The list scrolls like the other lists. Section headers (`Downloads`, `Screen`, `Storage and system`) are not
  focusable, stay with their first row and never sit alone at the bottom of the screen.
- The label column is as wide as the longest label of the language, between 44 % and 55 % of the row. A label that
  still does not fit wraps to 2 lines and its row grows; labels and values never touch.
- Lists opened from a row are titled with the row's label, and the sheet of one language with its token (`En`).
- A row set by `env` or `flag` shows its value without the stepper's `‹ ›`.
- A `ConfigSet` error under the row reads `Not saved: {message}`.
- The right column shows the source: `default`, `file`, `env` or `flag` (localized). At compact it is dropped with no
  other text, since the hint bar already tells: `Reset` for `file`, `Set by …` for `env` and `flag`, nothing for
  `default`.
- Percentages follow the locale: `5%` in en and pt_br, `5 %` in es_es and ru_ru.
- Left/Right change steppers and lists in place, without opening them (hint `Change`); A opens a list editor (hint
  `Edit`) or flips a toggle (hint `Switch`).
- X resets the focused row (`ConfigReset(key)`) when its source is `file`; the hint is hidden otherwise.
- Rows set by `env` or `flag` are read-only, with the reason in the hint bar: `Set by MOTHDEX_DOWNLOAD_CONCURRENCY`.
- `ConfigSet` errors show under the row in `error` color, and the old value stays.
- Rows that apply on next start show `Applies on next start` in `text.dim` after a change.
- **Ordered list editor** (Preferred languages): the chosen tokens in order, then `+ Add`. A on a token opens
  `Move up` · `Move down` · `Remove`. `+ Add` opens a static list of language tokens (`En`, `Fr`, `De`, `Es`, `It`,
  `Pt`, `Ja`, `Ru`, `Zh`, `Ko`, `Nl`, `Sv`).
- Formats, sources, folders, hosts and timeouts stay in the CLI (`mothdex config`), as with the TUI. The last row of
  the screen says `More settings: run "mothdex config list" over SSH.`

### 5.11 Start menu (overlay, from any screen)

```
           ┌──────────────────────────┐
           │  Home                     │
           │  Queue              ↓ 2   │
           │  History                  │
           │  Settings                 │
           │  Buttons                  │
           │  (screen actions, if any) │
           │  Quit                     │
           └──────────────────────────┘
```

- A centered panel over the `scrim`. Focus starts on the first item. Start or B closes it.
- **Screen actions** appear between Buttons and Quit when the current screen has them (Queue: `Cancel all
  downloads`, `Hide finished`; while quitting is pending: `Don't quit`).
- **Buttons** shows the button map of the screen underneath, with the pad's own labels (§2.1): one line per button
  the screen uses, the pad label and then the same words as that screen's hint bar, including the actions the hint
  bar fit rule drops (§4). Order: A, B, the rest in hint bar order, then L/R (`Page`), Select (`Queue`) and the
  D-pad (`Move`). No generic table.

### 5.12 Dialogs

- One pattern: a title (one sentence, `body`), an optional detail (`secondary`), then buttons in a row. The safe or
  non-destructive option is on the left and has the initial focus, except where §5 says otherwise.
- When the row does not fit the safe rectangle (large text, narrow screens, long translations), the buttons stack
  vertically in the same order, top to bottom, each as wide as the panel's inner width; D-pad Up/Down moves between
  them. Unfocused buttons are `bg` on the `surface` panel (§1.5).
- Never more than three buttons. B always equals the cancel option.
- Dialog text is the TUI question without `[y/N]` (for example `Already downloaded. Download again?`).

### 5.13 Quit and return to the frontend

**Asking to quit:** Start menu › Quit, or B on Home.

| Situation | Dialog |
|---|---|
| B on Home, no active downloads | `Quit and return to Batocera?` with `[Stay]` (default) and `[Quit]` |
| Start menu › Quit, no active downloads | no dialog; quits |
| Any quit with active or queued downloads | `2 downloads are still running.` with `[Keep downloading]` (default), `[Quit when finished]` and `[Cancel and quit]` |

- `Quit when finished`: the Queue opens and the top bar shows `Will quit when downloads finish`. Start menu ›
  `Don't quit` clears it. When the queue empties, the exit screen follows.
- `Cancel and quit`: `Cancel` each active and queued job, then the exit screen.

**Exit screen** (shown while the GUI closes the service):

```
┌────────────────────────────────────────────────────────────────────────┐
│                                                                        │
│                       3 downloaded · 1 failed                          │
│                     Updating the game list… (spinner)                  │
│                                                                        │
│                         Returning to Batocera                          │
└────────────────────────────────────────────────────────────────────────┘
```

- The GUI calls `Downloads.Wait`, which returns at once when the queue is idle, then `Close`. `Wait` runs the profile's
  refresh once if any job ended `Done` in this session, so the frontend's game list shows the new games
  (`reloadgames` on Batocera, EmuELEC and RetroBat). The contract bounds the whole exit to 2 s (Cancel, Wait with the
  refresh bounded, Close).
- The summary line counts this session's terminal jobs. With no downloads it is omitted, and so is the refresh line.
- When `BatchResult.RefreshWarning` is non-empty (manual refresh on Recalbox and ES-DE, or a failed hook), it replaces
  the refresh line, for example `Refresh the game list from the frontend menu to see the new games.` It stays for
  4 s after `Close` returns; A skips the wait. During those 4 s the screen shows one hint, `(A) Continue`, at the
  bottom of the safe rectangle (no hint bar otherwise).
- Otherwise the screen stays at least 1 s, so it can be read.
- The frontend name is `ActiveProfile().Name`; for `generic` the last line reads `Closing`.

**SIGTERM** (the frontend closes the Port, for example with its hotkey + Start): no dialog and no 4 s notice. The GUI
cancels active jobs (the service removes their `.part` files), runs `Wait` (refresh if anything is `Done`) and
`Close`, draws the exit screen if it can, and exits within the 2 s budget with code 0. SIGINT (Ctrl+C in a desktop
test) does the same.

## 6. Errors, empty and offline states

- Every service error renders **in place** on the screen that asked: never a crash, never a modal for a read. It shows
  the icon `!` in `error` color, `errors.<code>.message`, the GUI hint, and `(A) Retry` when the call can be retried.
  `code: <code>` follows in `text.dim`, for support.
- Messages come from `errors.<code>.message`. **Hints** come from `errors.<code>.hint`, except where the CLI hint names
  a flag or command the couch user cannot type; those codes use GUI hints `ui.gui.hints.<code>`:

| Code | GUI hint |
|---|---|
| `unmapped_platform` | `This system has no folder for {platform} yet. Set profile.folders.{platform} from the command line.` (folder picker: backlog G1) |
| `size_mismatch`, `integrity` | `Try again, or open the game and choose another link.` |
| `permission_denied` | `Check that the games folder is writable.` |
| `disk_full` | `Free some space on the games storage and try again.` |
| `exists` | `Change "When a file exists" in Settings to Rename.` |
| `path_rejected` | `Nothing was written. Check the folders in the system profile.` |
| `unsafe_name` | `Nothing was written. Open the game and choose another link.` |
| `invalid_config` | `Reset this setting in Settings, or run "mothdex config reset {key}".` |
| `untrusted_url` | `This link is not from a trusted host. Choose another link.` |

- `network` on a catalog read with a cached answer: the cached data shows with `stale: served from cache` (warn), and
  the top bar shows `Offline`. Without a cached answer: the in-place error with Retry.
- `rate_limited`: message and hint as in the CLI (`Wait {retry_after} and try again.`). Retry stays disabled until the
  time passes, with a countdown on the button.
- `internal`: the message plus `Details are in <log path>.`
- Download failures stay in the Queue rows (§5.8). A toast `Download failed: Contra (USA)` appears when the person is
  not on the Queue.
- Empty states are listed per screen in §5. Each one says what to do next, with a button that exists on that screen.
- All server text (titles, extras, source names, error details) goes through `app.SanitizeText` before drawing, as in
  the TUI.

## 7. Service operations used

| GUI action | Call |
|---|---|
| Splash, Home footer | `DBInfo`, `ActiveProfile`, `History{}`, `ConfigList` |
| Search, Results, continuous list | `Search` (PageSize 25, next page when needed) |
| Pickers, platform names | `Platforms`, `Regions` |
| Random | `Random{Count: 10}` |
| Covers in Results and Entry | `Cover(Ref{Slug})` |
| Entry, links, Why line | `Entry`, `Links(ref, {Prefer})` |
| Download (X, A on a link, Retry, Download again) | `Enqueue` with `Ref{Slug}` and `LinkSelector{Prefer}` or `{Index}` |
| Queue, badge, toasts | `Jobs`, `Events` (one subscription), `Cancel` |
| Cover art after a download | `SaveJobBoxart` (when `download.boxart` is on) |
| Downloaded marks, History | `History{Failed, Limit: 0}` |
| Settings | `ConfigList`, `ConfigSet`, `ConfigReset`, `CacheStats`, `CacheClear()` |
| Quit, SIGTERM | `Cancel` (each active job, when chosen), `Wait`, `Close` |

The only GUI-owned state is the recent queries in `tui_state` (shared with the TUI) and the in-memory caches (covers,
platforms). The GUI never reads `internal/*` packages.

## 8. Gaps and items for the contract (Alicerce, T-51)

Agreed with Alicerce on T-51; the contract (PR #58) records each item.

| Id | Need | Status / interim in v1 |
|---|---|---|
| G1 (backlog) | Folder choice for unmapped platforms (arcade): list the ROM root's subfolders minus launcher folders, and download into `<RomRoot>/<folder>` under the same root | backlog; v1 shows the `unmapped_platform` error with the GUI hint (§6) |
| G2 | Config keys `gui.confirm_button` (`south`\|`east`, default `south`), `gui.text_size` (`normal`\|`large`, default `normal`), `gui.safe_area` (`auto`\|`0`–`10`, default `auto`) | Forja adds them to the config schema; needed by PR 1. The GUI re-reads them after `ConfigSet` |
| G3 | `Wait` with an idle queue returns at once and still runs the refresh when a job ended `Done` since the last `Wait` | confirmed in `downloads.go`; exit budget 2 s |
| G4 | Exit codes for the launcher | `0` normal quit or SIGTERM, `1` SDL or display, `2` config, `3` internal; always written to the log |
| F1 (from ui.md) | Config schema metadata (ranges, enums) | static table for the curated keys, as in the TUI |
| F3 (from ui.md) | Live reload after `ConfigSet` | the GUI re-reads the keys it consumes; the rest say `Applies on next start` |

Downloads in the GUI always use the default destination (`DestDir: ""`), so there is one download root per session
and the per-root limiter of app-service §3 is enough.

## 9. Launcher (Port) behavior

The installer writes the Port file (`profiles.md` §4: opt-in per D22, on by default once E-M3 ships). From the GUI's
side:

- **On start** the Port runs `<bin_dir>/mothdex-gui`. The person sees the frontend's own launch screen, then the
  Splash (§5.1). If SDL or the display cannot open, nothing can be drawn: the GUI writes the reason to the log and
  exits with code 1 (G4), and the frontend comes back.
- **On exit** the person sees the exit screen (§5.13), then the frontend, whose game list was refreshed when the
  profile supports it.
- Profiles with `launcher: none` (Recalbox, Lakka, EmuDeck, RetroDECK) have no Port in v1. The GUI can still run as
  `mothdex-gui` from a desktop session or a terminal with a display. Nothing in the GUI depends on how it was
  launched.

## 10. Implementation plan (one engineer, three PRs)

| PR | Scope | Done when |
|---|---|---|
| 1 Foundation | SDL2 binding subset, window and tiers (§1.1), safe area, font atlas, input layer (§2: positions, swap, repeat, stick hysteresis, Select-on-release, keyboard), focus engine (§3), frame, toast, dialog, Start menu, Splash, Home, quit flow and exit screen, SIGTERM | Home works with a pad and a keyboard at 640x480, 1280x720 and 1920x1080; the quit flows of §5.13 work against a fake service |
| 2 Browse and download | Virtual keyboard (§5.4), Search, pickers, Results with the continuous list and cover panel, Entry and link chooser, Enqueue, Queue with action sheet and details, downloaded marks, error and empty states (§6) | a search-to-download path works with the pad only, including a Warming row and a failure |
| 3 History and Settings | History, Settings with every editor and the calibration view, `gui.text_size`, Buttons overlay, locale fallback notice, controller notices (§2.3) | every row of §5.10 changes and persists; large text keeps every screen usable at 640x480 |

Pure rules worth tests (per the testing rule): tier and width class, the safe rectangle, repeat timing with an
injected clock, stick hysteresis, Select-on-release, focus neighbor choice, the keyboard layout cycle and the
80-character limit, the continuous-list fetch trigger and 200-row window, the quit decision table (§5.13), and the
button label by controller type and swap.

## 11. Checks for Retina (conformance)

- At 640x480, 1280x720 and 1920x1080: text sizes, row heights and bar heights match §1.3-1.4 (±1 px), and nothing is
  drawn outside the safe rectangle with `gui.safe_area` at 0, 5 and 10. Include the worst top bar (§4): `Offline`, a
  pending quit and the queue badge together, with large text, in pt_br and ru_ru.
- At 480x320 the compact layout is scaled and letterboxed, not cropped.
- Every screen in §5 can be entered, used and left with the pad only. The focused element is always visible and is
  restored by id after Back.
- Each hint bar matches the screen's real actions. With a Nintendo pad the labels are `B A Y X` at south, east, west
  and north. With `gui.confirm_button: east`, the A and B roles swap everywhere, the keyboard screen included.
- Quitting with active downloads always shows the three-option dialog with `Keep downloading` focused; SIGTERM exits
  within 2 s without a dialog.
- The contrast pairs of §1.5 hold in the build (sample the rendered pixels).
- No string is cut except titles, and every state has a word or symbol besides its color.
