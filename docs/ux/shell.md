# UX: interactive Shell (REPL)

Status: v1.1 (T-6; E-M2 deviations recorded in UX-sync, Decisions D24) · Owner: Bússola · Implementer: Vitral · Risk: Medium
Services: [`app-service.md`](../contracts/cli/app-service.md) only. Human output rules come from [`cli-conventions.md`](cli-conventions.md); syntax of
`--prefer` from [`prefer-syntax.md`](prefer-syntax.md). Library: `peterh/liner` (PLAN §9), so history and completion are line-based.

## 1. Principles

- Same commands, same flags, same output as the CLI (the Shell is the CLI with a session). No Shell-only grammar
  except the index shortcuts in §4 and the meta commands in §2.
- Never prompt unless a CLI command would (cli-conventions §6). Prompts use the same wording.
- Works on a dumb terminal: no full-screen redraws; progress uses the CLI renderer.

## 2. Entry and layout

`mothdex shell` (or `mothdex` with no arguments on a TTY). A banner of at most 3 lines, then the prompt.

```
mothdex 0.2.0 · 1,284,019 entries · profile: generic · type "help" or Tab
mothdex> search zelda -p snes
 #  Title                           Platform  Regions  Links  Size
 1  Legend of Zelda, The: A Link..  snes      us,eu    2      1.0 MB
 2  Zelda no Densetsu               snes      jp       1      1.0 MB
Page 1/1 · 2 results · cached 3h ago
mothdex[snes:2]> download 1
```

Prompt: `mothdex> ` with no session; `mothdex[<platform>:<n>]> ` after a search, where platform is the `-p` filter (if any)
and `n` the count on the page. The prompt is plain text with no color or dim style, because the line editor refuses
control characters in a prompt (D24).

Banner data comes from `DBInfo` and `Profiles.Active`; if offline it shows `offline` instead of the entry count, no error.

Commands: every CLI command except `shell`, `tui`, `gui`. Meta commands: `help [cmd]`, `clear`, `exit`/`quit`
(Ctrl+D also exits), `last` (reprint the last search page). Global flags (`--json`, `-q`, `--no-color`) work per command.
`--config`, `--lang`, `--allow-executable` and `-j`/`--jobs` take effect only when the service opens, so they are
rejected inside the Shell with a usage message (`not available in the Shell; run the command outside it`). Inside the
Shell `-f` only skips the confirmation; overwriting still follows `download.on_exists`. `mothdex -f shell` and
`--allow-executable` at start-up are not applied to the session (D24, ui.md D4).

## 3. Search flow and paging

```
mothdex> search mario
 ... table ...
Page 1/4 · 37 results · n next · p prev · <#> info · d <#> download
mothdex[all:10]> n
```

- The footer is one line: `Page 1/4 · 37 results · n next · p prev · <#> info · d <#> download`. With a single page
  the paging shortcuts are left out; the `{size} per page` part of the CLI footer is not shown (D24).
- `n`, `p` (bare words) go to the next/previous page of the **last search** (re-calls `Search` with the page changed,
  same query). At the ends: `This is the first page.` / `This is the last page.`
- `n`, `p`, `page`, `sort` and `last` work on the last page shown **in this Shell process**. After a restart they print
  `No search yet in this shell. Run search first.`; indexes (`download 1`) still resolve through the stored session
  (D24, ui.md D3).
- `page <n>` jumps. `sort title|size|relevance` re-sorts (current page only, per service rule).
- A new `search` replaces the session; the old indexes stop being valid (the table header says so on the first search
  after a change: `Indexes now refer to this search.`).
- A bare number `<#>` is shorthand for `info <#>`. `d <#>` is shorthand for `download <#>`.

## 4. Index-based commands

`info`, `download`, `boxart` accept `<#>`, several indexes (`download 1 3 5`), and ranges (`download 2-4`). `download` gets
the whole list in one call; `info` and `boxart` take one entry, so the Shell runs them once for each entry with the same
flags (`info 2-3` is `info 2` then `info 3`; the exit code is the first failure, and the later entries still run). They
resolve through `Ref{Index}` against the session. Errors: `no_session`, `index_out_of_range` with the CLI messages.
Because the Shell persists the session (service saves it), indexes also work after restarting the Shell and in the CLI
(`mothdex download 1`), until the next search.

Link choice on `download <#>`:
1. `--link n` or `--prefer ...` given, or `download.prefer` configured: take the winner, print the
   `Link 2 chosen (...)` line (prefer-syntax §5), then download.
2. Otherwise, for a **single** index (`download 1`) whose entry has more than one link, open the **link chooser**
   (inline, no full screen). With several indexes (`download 1 3 5`, `download 2-4`) there is no chooser: each entry
   takes its default link, and every entry with more than one link prints the `Link 2 chosen (...)` line before the
   confirmation, so the person sees what was picked (D24-review).

```
mothdex> download 1
Zelda: Link's Awakening DX · 3 links
 1*  Game  gbc    1.0 MB  En        Myrient   <- default
 2   Game  gbc    1.0 MB  En,Rev A  Myrient
 3   Demo  gbc    0.2 MB  -         Archive
Link [1]: _
```
Enter takes the default (the top-ranked link, marked `*`); a number picks that link; `q` or empty-after-Ctrl+C cancels.
With one link there is no chooser. The chooser and the confirmation need both stdin and stdout on a terminal (the line
editor writes to stdout); otherwise the default is used, as in scripted input (D24).

3. Confirmation follows cli-conventions §6; then progress rows render in place. The prompt returns when the queue is
   empty. Ctrl+C during a download cancels the queue (`Cancel`) and returns to the prompt; it does not exit the Shell.

**Ctrl+C never exits the Shell** (D24-review). At the prompt it clears the typed line and shows
`Type exit or press Ctrl+D to leave.` Only `exit`, `quit` and Ctrl+D leave (exit code 0); piped input ends after the
last line with that command's code. A terminal on stdin with stdout redirected counts as piped input (no banner, prompt or
history), so Ctrl+D there ends the session with the last command's code, not 0. The line editor cannot tell an empty line from a typed one, so a "press twice to
leave" rule would also quit when someone clears a line or presses Ctrl+C twice to stop a download.

Mixed usage: `download <#> --prefer lang=En -o ~/roms` accepts all CLI flags.

## 5. History

- Command history is owned by the Shell UI: `<paths.data>/shell_history`, one command per line, last 1000 kept,
  consecutive duplicates collapsed. Up/Down, Ctrl+R reverse search (liner defaults).
- Lines are never recorded when they start with a space, or match `config set` of a key ending in `token` or `key`
  (none exist today; kept as a guard for future keys).
- Download history (`history` command) is a separate service feature (`History.List`); `history --failed`, `-e` as in the CLI.
- If the file cannot be written: continue silently with in-memory history and log at warn.

## 6. Autocomplete (Tab)

Context-aware, first word then arguments. Candidates are sorted, case-insensitive prefix match. One candidate
completes inline; several show a list (liner behavior) after the second Tab.

Known limit: when the terminal reports 0 columns (a pty without a window size, some ssh or CI wrappers) the line editor
falls back to plain line reading, with no Tab completion and no history keys, and `^C` is echoed.

| Position | Candidates | Source |
|---|---|---|
| command name | all commands + meta commands | static |
| flag name (`-`/`--`) | flags of that command | static |
| `-p`/`--platform` value | platform ids | `Catalog.Platforms` (cached, `meta` ttl) |
| `-r`/`--region` value | region ids | `Catalog.Regions` |
| `--prefer` key | `lang=`, `format=`, `source=`, `size=` | static |
| `--prefer size=` | `smallest`, `largest` | static |
| `--prefer format=` / `lang=` / `source=` | values seen in the last viewed entry's links, if any | session entry (best effort) |
| `config get/set/reset` key | config keys; `set` also suggests enum values (`on_exists=`, `color=`) | `Config.List` |
| `cache clear` | cache namespaces | `Cache.Stats` |
| `--sort` | `relevance`, `title`, `size` | static |
| `-e/--export` | file paths | filesystem |
| `info`/`download`/`boxart` first arg | `1`..`n` of the last page | session |

Platform and region lists are fetched lazily on first Tab (one request, then cached). If the request fails, completion
silently offers nothing for that position; it never prints errors while typing.

## 7. Help

`help` lists commands in one column with a one-line summary each (i18n `help.<cmd>`); `help <cmd>` prints usage, flags
and two examples, the same text as `mothdex <cmd> --help`. Until command help is translated, both are the English
cobra help (D24, ui.md D2; translation is backlog).

## 8. Edge cases

| Case | Behavior |
|---|---|
| Unknown command | `Unknown command "serch". Did you mean "search"?` (edit distance ≤ 2) plus `Type help.` |
| Terminal narrower than 60 | same truncation rules as the CLI |
| EOF on stdin (piped script) | exit after the last line, exit code of the last command |
| Ctrl+C | cancels the running command, or clears the typed line; never exits (§4) |
| Offline | commands fail with `network`; the prompt keeps working; cached searches still answer (stale note) |
| Resize | the next table uses the new width; no redraw of old output |
| Locale | strings in the active language; commands and flags are never translated |
