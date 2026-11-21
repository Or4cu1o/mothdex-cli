# T-43: Shell acceptance (`mothdex shell`, T-41)

Owner: Ensaio. Run at main `b45d433`. References: `docs/ux/shell.md`, `docs/contracts/cli/ui.md` (section 2, with the D4 rule
for per-line flags). The text of decision **D24** is not in the repository, so it was not checked (listed under "Not verified").

## Method

- Binary: GoReleaser snapshot build of `b45d433`, linux amd64 (`mothdex 0.0.1-snapshot (commit b45d433)`).
- API: a local fixture on 127.0.0.1 (25 entries over 3 pages of 10, one entry with 3 links, platforms, regions). **No live call.**
  Download links point at the reserved name `files.example.test` (allowed through `download.allowed_hosts` in an isolated
  config), so the link chooser can be exercised; the transfer itself fails offline and follows the built-in retry policy.
- Interactive checks run on a pseudo-terminal (30 rows by 100 columns) with isolated `HOME` and `XDG_*` directories.
  Script mode runs on pipes. Package tests: `go test -race ./internal/ui/... ./cmd/...` green on `b45d433`.
- Items marked PASS were seen in the output of the binary; the exit code is read from the process.

## Results

| Item | Contract | Result |
|---|---|---|
| Banner | shell.md 2: at most 3 lines, entries and profile | PASS: one line `mothdex <ver> · 1,234 entries · profile: generic · type "help" or Tab`; with the API down it shows `offline` and the prompt still works |
| Prompt | `mothdex> `, then `mothdex[<platform\|all>:<n>]> ` | PASS: `mothdex[all:10]>`, `mothdex[ps2:10]>` after `search -p ps2`, `[all:5]` on the short last page |
| `n`, `p`, `page`, `sort`, `last` | shell.md 3 | PASS: next and previous page, `This is the last page.` and `This is the first page.` at the ends, `page 1`, `sort title`, `last` reprints from cache (`cached 1m ago`); with no search, `No search yet in this shell. Run search first.` |
| New search | `Indexes now refer to this search.` | PASS (shown when a search replaces an earlier one) |
| Bare index | `<#>` is `info <#>` | PASS (`2` shows the entry); `11` gives `index_out_of_range`, `0` gives `usage` |
| Overflow inputs | `bad_range` and `range_too_big`, no crash | PASS: `info/download/boxart 99999999999999999999-99999999999999999999` prints `"..." is not a valid range; write it as 2-4.` (`ui.shell.bad_range`); `info/boxart/download 1-9223372036854775807` prints `A line can name at most 100 entries.` (`ui.shell.range_too_big`); `0-3` and `4-2` are `bad_range`. A bare `99999999999999999999-99999999999999999999` is an unknown command with a hint, no crash |
| `d` with the link chooser | shell.md 4 | PASS: `d 3` on a 3-link entry lists the links with `*` and `<- default`, `Link [1]:`; `q` prints `Canceled.`, `9` reprompts with `Pick one of the numbers above, or q to cancel.`, `2` starts the download of link 2 with the CLI progress lines. One link: no chooser |
| Per-line flags | ui.md 2.1 (D4) | PASS: `-j`, `--allow-executable`, `--config`, `--lang` give `usage`: `<flag> is not available in the Shell; run the command outside it.` `-f` is **accepted** (it only skips the confirmation, ui.md 2.1); the task text listed `-f` as rejected, the contract does not (see note N1) |
| Tab | shell.md 6 | PASS: `sea` completes the command, `hel` completes `help`, `search -p <Tab>` lists `gba ps2 snes`, `search -r <Tab>` lists `eur jpn usa`, `--sort <Tab>` lists `relevance size title`, `config set interface.p<Tab>` completes `interface.page_size`, `cache clear <Tab>` lists the namespaces |
| History file | shell.md 5 | PASS: `shell_history` has mode `0600`; a line starting with a space and a `config set ... .token` line are not recorded; the rest are |
| Ctrl+D | exit 0 | PASS: exit 0 |
| Ctrl+C | message, then exit 130 on the second within 2 s | PASS: first press prints `Type exit or press Ctrl+D to leave.`; two presses within 2 s exit 130; two presses 2.6 s apart keep the Shell alive |
| Ctrl+C during a download | cancels the queue, back to the prompt | PASS: the download stops, the prompt returns and the next command runs; Ctrl+D then exits 0 |
| Piped script | exit code of the last command | PASS: no banner; last line `info 99` exits 3; a script that ends well exits 0; the chooser never appears (`d 3` uses the default) |
| SIGINT in script mode | ends the script, 130 | PASS: SIGINT during a download exits 130 and the next scripted line is not executed |
| stdout redirected | `confirmation_required`, no hang | PASS: `d 1 2` with stdout to a file (piped stdin, and also a terminal stdin) prints `confirmation_required`, exit 2 for the command, and the Shell stays usable |
| Locale | strings in the active language, fallback to en_us | PASS: `--lang pt_br` translates the banner, hints and errors; `--lang ja_jp` (no `shell` keys) and an unknown locale fall back to English with no raw key shown, so the "nested keys" open question of ui.md is closed for these paths |
| Unknown command | `Did you mean "search"?` | PASS |

## Findings (for Vitral, ref T-41)

- **S1 (Minor, spec vs behavior):** shell.md 4 and ui.md 2.1 say `info`, `download` and `boxart` take several indexes and ranges.
  `download` does; `info` and `boxart` take exactly one argument in the CLI, so `info 2-3`, `info 1 3` and `boxart 1-2` end with
  `Invalid usage: accepts 1 arg(s), received 2` (and `info 1-100` with `received 100`). Either fix the two documents or let
  the Runner run `info` and `boxart` once per index. Repro: `search game`, then `info 2-3`.
- **S2 (Suggestion):** the double Ctrl+C rule is spelled "at an empty prompt", but a Ctrl+C on a line that has text also
  starts the 2 s window: `search -r <Ctrl+C><Ctrl+C>` within 2 s exits 130. Decide which is intended and align shell.md 4.
- **S3 (Suggestion):** when the terminal reports 0 columns (a `pty` without a window size, some ssh or CI wrappers), liner
  silently falls back to plain line reading (`tooNarrow`): no Tab, no history keys, `^C` echoed. shell.md 1 promises it
  works on a dumb terminal, and it does, but the user gets no hint. Real terminals report a size, so this is low impact.
- **S4 (Suggestion):** with a terminal on stdin and stdout redirected to a file, Ctrl+D exits with the last command's code (2 after
  a `confirmation_required`), not 0, because the Shell treats that session as non-interactive. Contract is silent.
- **S5 (Suggestion, CLI, Forja):** with no `download.dir` and no `-o`, a missing default ROM folder makes `download` fail with
  `invalid_request` and the hint `Check the slug, filters and values you passed.` The same happens in the CLI. `-o` and
  `download.dir` create the folder, the default one does not. A first-run user will meet this.

Note N1: the task listed `-f` among the flags rejected per line; ui.md 2.1 says `-f` is accepted and only skips the confirmation.
The binary follows ui.md. Tell me if the contract should change.

## Not verified

- Decision D24 (text not in the repository).
- Windows console behavior of liner (no Windows host); macOS.
- Ctrl+R reverse search (ui.md itself says it is assumed from liner), terminal resize, width below 60.
- A completed download through the Shell (the fixture host is not reachable; the download path is covered by the T-22 live run and
  the package tests), and the in-place progress redraw on a real terminal (progress was line-per-state in the pty).
- Completion of `--prefer` values from the last viewed entry.
