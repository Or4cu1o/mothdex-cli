# UX: CLI human output conventions

Status: draft v1 (T-6) · Owner: Bússola · Implementer: Forja (T-20), Vitral reuses for Shell/TUI · Risk: Medium
Scope: what a person sees in the terminal. Machine output, JSON, export and exit codes are in
[`cli-output.md`](../contracts/cli/cli-output.md) and are not repeated here. Services used: only [`app-service.md`](../contracts/cli/app-service.md).

## 1. Streams and modes

| Mode | stdout | stderr | Color | Progress |
|---|---|---|---|---|
| default, TTY | result (table) | progress, footers, warnings, errors | yes | live bars |
| default, piped | result (plain table, no truncation) | warnings, errors | no | one line per state change |
| `-q/--quiet` | result only | errors only | per §4 | none |
| `--json` | one JSON document | warnings, errors | no | none |

TTY is judged per stream: stdout piped but stderr on a TTY still shows live progress on stderr.

## 2. Tables

Plain aligned columns, two spaces between columns, no box-drawing (copy/paste friendly; identical to the `txt`
export). Header is bold when color is on. Numbers right-aligned, text left-aligned. Empty value renders as `-`.

`search` (columns: `#`, Title, Platform, Regions, Links, Size):

```
 #  Title                                   Platform  Regions     Links  Size
 1  Super Mario Bros. 3                     nes       us          3      384 KB
 2  Super Mario Bros. 3 (Rev A)             nes       us,eu       2      384 KB
 3  Super Mario Advance 4: Super Mario...   gba       us,jp,eu    4      8.0 MB
```
stderr footer:
```
Page 1/4 · 37 results · 10 per page · cached 3h ago
Next: mothdex search "mario" --page 2  |  download <#>  |  info <#>
```

Rules:
- `#` is the 1-based index used by `info`, `download`, `boxart` (valid until the next search; see [shell.md](shell.md) §4).
- The footer goes to **stderr** so stdout stays pipe-clean. The hint line is omitted with `-q`.
- `Size` is the top-ranked link's `size_str`; `Links` is the link count.
- Regions are API ids, lower-case, comma-joined without spaces; long names are never used in tables.
- `Meta.Stale`: footer adds `stale: served from cache` in the warn color.
- `PastLastPage`: warning `Page 9 does not exist; showing page 4.`
- Zero results: `No results for "xyz". Try fewer words, or remove -p/-r.` Exit 0, nothing on stdout.

`info` is a key/value block, then a link list:

```
Super Mario Bros. 3
  Slug      super-mario-bros-3-nes
  Platform  nes (Nintendo Entertainment System)
  Regions   us
  Serial    -
Links
  1  Game  nes     384 KB  En        Myrient
  2  Game  nes     384 KB  En,Rev A  Myrient
  3  Demo  nes     120 KB  -         Archive
```

The link `#` is the value for `--link <n>`. When `--prefer` (or `download.prefer`) is active, the winner is marked
`*` and a `Why` column appears from `LinkChoice.Reasons` (e.g. `lang=En, format=nes`). See [prefer-syntax.md](prefer-syntax.md).

`platforms` / `regions`: columns `id`, `name`, (`brand`), sorted by id.
`history`: `Date` (local), `State`, `Title`, `Platform`, `Size`, `Time`, `Tries`; failed rows show `failed:<code>`.

## 3. Truncation

- Width = terminal columns (`COLUMNS`/ioctl), fallback 80, minimum supported 60. Piped output is never truncated.
- Only the **Title** column shrinks, cut with a trailing `...` (three ASCII dots, safe on legacy consoles).
- Below 80 columns drop columns in this order: `Links`, `Regions`, `Size`. Below 60: `#` and Title only.
- Width is measured in display cells (wide CJK counts 2) so zh/ja locales stay aligned.
- `info`, errors and prompts wrap at the terminal width; filenames and paths are never truncated there.

## 4. Color and `--no-color`

Color is a hint, never the only carrier of meaning: every colored word also has a label or symbol.

On when `interface.color=always`, or `auto` with the stream a TTY, `NO_COLOR` unset and `TERM` not `dumb`.
Off with `--no-color`, `NO_COLOR` (non-empty), `interface.color=never`. `--no-color` wins over everything.
Only the 16 ANSI colors are used (the user's terminal theme owns the hue and contrast).

| Role | Style | Used for |
|---|---|---|
| heading | bold | table header, section titles |
| dim | faint (SGR 2) | footers, hints, `-` cells, secondary extras |
| accent | cyan | winning/selected item, key names |
| ok | green | `Done`, `crc ok` |
| warn | yellow | `Warming`, `RetryWait`, stale, past-last-page |
| error | bold red | the `error:` prefix and `✘` |

Symbols with ASCII fallback when the locale is not UTF-8 or `TERM=linux`: `✔`→`[ok]`, `✘`→`[x]`, `█░`→`#-`, `…`→`...`.

## 5. Progress

On stderr, for `download` (and `boxart` with more than one). One row per active job, queued jobs one line each.
Redrawn in place at most 4 times/s (the service already throttles events). Non-TTY: no redraw, one line per state change.

```
Downloading 3 files (2 at a time)
 [1/3] Super Mario Bros. 3 (USA).nes
       ██████████████░░░░░░░░░░  58%  222 KB / 384 KB  1.2 MB/s  eta 00:01
 [2/3] Metroid (USA).nes
       Warming up the server… 02:14
       The server is fetching this file from peers. This is normal; Ctrl+C cancels.
 [3/3] Zelda II (USA).nes
       Queued
```

| State | Display | Style |
|---|---|---|
| Queued | `Queued` | dim |
| Preflight, Connecting | `Connecting…` | default |
| **Warming** | `Warming up the server… mm:ss` | warn |
| Transferring | bar, %, bytes, rate, ETA | accent bar |
| Verifying / Finalizing | `Verifying…` / `Saving…` | default |
| RetryWait | `Retrying in 8s (attempt 2/5): connection reset` | warn |
| Done | `✔ Saved to <path> (384 KB, crc ok) in 12s` | ok |
| Skipped | `Skipped: already exists (<path>)` | dim |
| Failed | `✘ <localized message> [size_mismatch]` | error |

**Warming** (the proxy fetches from torrent peers on demand; first byte can take minutes, `first_byte_timeout` 10 m):
1. Show `Warming up the server…` with an elapsed `mm:ss` counter driven by a UI timer from the state-change event.
   No percentage and no fake bar. The counter keeps ticking without events so it never looks frozen.
2. After 30 s add the hint line shown above.
3. Ctrl+C cancels and removes the `.part`; a second Ctrl+C within 2 s exits at once (exit 130).
4. At the first byte the row switches to the bar (clear the line, then draw).
5. ETA appears after 3 s of transfer (10 s moving average); before that `eta --:--`.
Sizes are binary-based with `KB/MB/GB` labels, one decimal above 1 MB, matching `size_str`.

Final summary on stderr: `3 downloaded, 1 skipped, 0 failed in 01:42` (i18n plurals). With `-q` shown only on failure.

## 6. Prompts

`interface.confirm_downloads=true` asks only when a command queues more than 1 file or more than 500 MB (`524 288 000` bytes; the downloader's header-time
re-confirmation uses the same constant, downloader.md §3.2):

```
Download 5 files (1.2 GB) to /userdata/roms/nes ?  [y/N]
```

Default No. Accepts `y`/`n` and the localized initials. With `--json`, `-q` or non-TTY stdin it never appears: it
fails with `confirmation_required` unless `-f/--force` (contract).

## 7. Errors

stderr, one block, no stack traces, no raw Go text:

```
error: <localized message>
  <what to do next, one sentence>
  code: not_found
```

- Message = i18n `errors.<code>.message` with named placeholders; hint = `errors.<code>.hint`. Both exist for every
  code in cli-output §4; missing translations fall back to `en_us` per key.
- Hints are concrete and actionable, no blame, no exclamation marks, no emoji. Offending values are quoted; paths in full.
- Never print the API URL or tokens. `Cause` appears only with `-v` and is always in the log file.

English source strings (Forja owns the YAML):

| Key | message | hint |
|---|---|---|
| `errors.usage` | `Invalid usage: {detail}` | `Run "mothdex {command} --help".` |
| `errors.invalid_config` | `Invalid setting {key}={value} (from {source}).` | `Run "mothdex config reset {key}".` |
| `errors.invalid_request` | `Invalid request: {detail}.` | `Check the slug, filters and values you passed.` |
| `errors.unmapped_platform` | `No folder is set for platform {platform}.` | `Pass -o <dir>, or set profile.folders.{platform} in the configuration.` |
| `errors.confirmation_required` | `This command needs confirmation.` | `Add --force to skip the prompt.` |
| `errors.no_session` | `There is no previous search to refer to.` | `Run "mothdex search <text>" first, or use --slug.` |
| `errors.index_out_of_range` | `Result #{index} does not exist (last search has {count}).` | `Pick a number between 1 and {count}.` |
| `errors.not_found` | `No entry found for {ref}.` | `Check the spelling or search with fewer words.` |
| `errors.network` | `Cannot reach the Mothdex server.` | `Check your connection.` |
| `errors.rate_limited` | `The server asked us to slow down.` | `Wait {retry_after} and try again.` |
| `errors.upstream` | `The server had a problem (HTTP {status}).` | `Try again in a few minutes.` |
| `errors.api_incompatible` | `The server answered in a format this version does not understand.` | `Update mothdex.` |
| `errors.untrusted_url` | `Refusing to download from {host}.` | `Only {allowed} is trusted.` |
| `errors.untrusted_redirect` | `The server redirected to {host}, which is not trusted.` | `Nothing was sent there. Try again later; if it persists, report it.` |
| `errors.size_mismatch` | `The downloaded file has the wrong size ({got} of {expected}).` | `Try again, or choose another link with --link.` |
| `errors.integrity` | `The downloaded file is corrupt (archive check failed).` | `Try again, or choose another link with --link.` |
| `errors.permission_denied` | `Cannot write to {path}.` | `Pick another folder with -o, or fix its permissions.` |
| `errors.disk_full` | `Not enough free space in {path} (needs {needed}).` | `Free some space or choose another folder with -o.` |
| `errors.exists` | `{path} already exists.` | `Set download.on_exists to rename or overwrite.` |
| `errors.path_rejected` | `Refusing to write to {path}.` | `Nothing was written. Choose another folder with -o, or check profile.folders.` |
| `errors.unsafe_name` | `Refusing to save {name}: its file type can run code.` | `Nothing was written. Choose another link with --link, or pass --allow-executable if you trust it.` |
| `errors.canceled` | `Canceled.` | (none) |
| `errors.internal` | `Unexpected error.` | `Details are in {log_path}. Please report it.` |

Batch failures (exit 7): the summary lists one line per failed job, `✘ <title>: <message> [code]`.

## 8. Quiet mode

`-q`: result on stdout, errors on stderr; no progress, footers, hints or success summary. For `download`, success
prints one line per file on stdout: the final path (script friendly). `-q` with `--json`: JSON rules win. `-q` does not
change color; use `--no-color` separately.

## 9. Wording

- Imperative, present tense, sentence case; no period on labels, period on sentences.
- Nouns: *entry*, *link*, *platform*, *region*, *queue*. No "mirror" (there are none).
- Every string is an i18n key (`ui.<area>.<name>`), with placeholders and plural forms, never concatenated.
- Durations `mm:ss` (`h:mm:ss` from one hour), rates `1.2 MB/s`, dates in local time `2026-11-20 14:03`.
