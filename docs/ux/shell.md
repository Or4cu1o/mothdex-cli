# UX: interactive Shell (REPL)

Status: draft v1 (T-6) · Owner: Bússola · Implementer: Vitral · Risk: Medium
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
and `n` the count on the page (dim). The prompt is plain text; color only when allowed (`--no-color` rules apply).

Banner data comes from `DBInfo` and `Profiles.Active`; if offline it shows `offline` instead of the entry count, no error.

Commands: every CLI command except `shell`, `tui`, `gui`. Meta commands: `help [cmd]`, `clear`, `exit`/`quit`
(Ctrl+D also exits), `last` (reprint the last search page). Global flags (`--json`, `-q`, `--no-color`) work per command.
`--config` and `--lang` are start-up only and are rejected inside the Shell with a usage message.

## 3. Search flow and paging

```
mothdex> search mario
 ... table ...
Page 1/4 · 37 results · n next · p prev · <#> info · d <#> download
mothdex[all:10]> n
```

- `n`, `p` (bare words) go to the next/previous page of the **last search** (re-calls `Search` with the page changed,
  same query). At the ends: `This is the first page.` / `This is the last page.`
- `page <n>` jumps. `sort title|size|relevance` re-sorts (current page only, per service rule).
- A new `search` replaces the session; the old indexes stop being valid (the table header says so on the first search
  after a change: `Indexes now refer to this search.`).
- A bare number `<#>` is shorthand for `info <#>`. `d <#>` is shorthand for `download <#>`.

## 4. Index-based commands

`info`, `download`, `boxart` accept `<#>`, several indexes (`download 1 3 5`), and ranges (`download 2-4`). They resolve
through `Ref{Index}` against the session. Errors: `no_session`, `index_out_of_range` with the CLI messages.
Because the Shell persists the session (service saves it), indexes also work after restarting the Shell and in the CLI
(`mothdex download 1`), until the next search.

Link choice on `download <#>`:
1. `--link n` or `--prefer ...` given, or `download.prefer` configured: take the winner, print the
   `Link 2 chosen (...)` line (prefer-syntax §5), then download.
2. Otherwise, if the entry has more than one link, open the **link chooser** (inline, no full screen):

```
mothdex> download 1
Zelda: Link's Awakening DX · 3 links
 1*  Game  gbc    1.0 MB  En        Myrient   <- default
 2   Game  gbc    1.0 MB  En,Rev A  Myrient
 3   Demo  gbc    0.2 MB  -         Archive
Link [1]: _
```
Enter takes the default (the top-ranked link, marked `*`); a number picks that link; `q` or empty-after-Ctrl+C cancels.
With one link there is no chooser. In scripted input (stdin not a TTY) the chooser never appears: the default is used.

3. Confirmation follows cli-conventions §6; then progress rows render in place. The prompt returns when the queue is
   empty. Ctrl+C during a download cancels the queue (`Cancel`) and returns to the prompt, it does not exit the Shell;
   Ctrl+C at an empty prompt shows `Type exit or press Ctrl+D to leave.`; a second one within 2 s exits.

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
and two examples, the same text as `mothdex <cmd> --help`.

## 8. Edge cases

| Case | Behavior |
|---|---|
| Unknown command | `Unknown command "serch". Did you mean "search"?` (edit distance ≤ 2) plus `Type help.` |
| Terminal narrower than 60 | same truncation rules as the CLI |
| EOF on stdin (piped script) | exit after the last line, exit code of the last command |
| Offline | commands fail with `network`; the prompt keeps working; cached searches still answer (stale note) |
| Resize | the next table uses the new width; no redraw of old output |
| Locale | strings in the active language; commands and flags are never translated |
