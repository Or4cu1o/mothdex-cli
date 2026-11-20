# UX: `--prefer` syntax (resolves OQ-APP-1)

Status: settled v1 (T-6; Alicerce confirmed the service side) · Owner: Bússola · Implementer: Forja (CLI parsing) · Risk: Medium
Service side: `Preference` and `LinkSelector` in [`app-service.md`](../contracts/cli/app-service.md) §2 and the ranking rule in §3.

## 1. Syntax

```
--prefer key=value[,value...]        repeatable
```

| Key | Matches | Values | Maps to |
|---|---|---|---|
| `lang` | tokens of `link.extras` (case-insensitive) | `En`, `Pt`, `Ja`, ... ordered | `Preference.Languages` |
| `format` | `link.format` | `chd`, `gba`, `bin+cue`, ... ordered | `Preference.Formats` |
| `source` | `link.source_name` | `Myrient`, ... ordered | `Preference.Sources` |
| `size` | `link.size` | exactly one of `smallest`, `largest` | `Preference.Size` |

Order inside a key is priority order (first is best). Repeating a key appends its values in order of appearance.
Values containing a comma or space are quoted by the shell as usual (`--prefer "source=Some Host"`).

```
mothdex download 3 --prefer lang=En,Pt --prefer format=chd --prefer size=smallest
mothdex download --search "zelda" -p snes --prefer lang=En
```

Why this shape: one flag, no positional magic, extensible without new flags, and it is a 1:1 image of the
`download.prefer` config block, so people can move a command line into the config file unchanged.

## 2. Ranking (from the service, restated for users)

Criteria apply in this fixed order, not in flag order: language, then format, then source, then size, then the API
order. `Game` links are kept before ranking when any exist. Region is **not** a link attribute (it belongs to the
entry), so it is selected with the existing `-r/--region` filter on `search`, `random` and `download --search`, not
with `--prefer`. `--link <n>` wins over everything and conflicts with `--prefer`: using both is `usage` (exit 2).

## 3. Config and precedence

Config keys (`download.prefer.languages|formats|sources`, plus `download.prefer.size`: `none|smallest|largest`,
default `none`, defined in [`config.md`](../contracts/cli/config.md) §2) are the baseline. A `--prefer` key **replaces** that key's list for
the run (it does not merge): `--prefer lang=Pt` ignores configured languages but keeps configured formats.
Env mapping follows the config rule (`MOTHDEX_DOWNLOAD_PREFER_LANGUAGES=En,Pt`).

## 4. Validation (CLI layer)

- Unknown key: `usage`, exit 2: `Unknown --prefer key "lng". Valid keys: lang, format, source, size.`
- Missing `=` or empty value: `usage`: `--prefer needs key=value, for example lang=En,Pt.`
- `size` with anything but `smallest`/`largest`, or with two values: `usage`.
- Values are not checked against the catalogue (extras are free text); a value that matches nothing just ranks last.
  The `download` and `info` output shows `Why`, so a typo is visible (`Why: -`), and `-v` adds one line:
  `No link matches lang=Ptt; using API order.`

## 5. Output

Non-interactive commands take the top-ranked link and print it before downloading:
`Link 2 chosen (lang=En, format=chd, smallest of 3).` on stderr, hidden by `-q`. `info --prefer ...` marks the winner
`*` with a `Why` column (cli-conventions §2). TUI and Shell use the same strings from `LinkChoice.Reasons`.

## 6. Help text (English source)

```
--prefer key=value[,value...]   Rank links when several exist (repeatable).
                                Keys: lang, format, source (ordered lists), size (smallest|largest).
                                Example: --prefer lang=En,Pt --prefer size=smallest
```
