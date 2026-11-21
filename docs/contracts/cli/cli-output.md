# Contract: CLI machine output, export and exit codes

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-20) · Risk: Medium
Human output (tables, colors, progress, wording) belongs to Bússola (T-6). This file only fixes what scripts depend on.

## 1. Streams

- **stdout** carries only the result: the human table, or exactly one JSON document with `--json`.
- **Untrusted text** (T-16 CR-14): every API string (title, filename, extras, source names) has `Cc` and `Cf` runes
  (ANSI escapes, bidi controls, zero-width) removed before it reaches a terminal (human output, TUI, Shell). JSON
  output keeps the raw value, escaped by the encoder.
- **stderr** carries progress, warnings, prompts and logs. With `--json`, stderr shows only warnings and errors
  (no progress bars); `-q/--quiet` suppresses everything except errors.
- `--json` implies non-interactive: no confirmation prompts; when a prompt would be needed, the command fails with
  `confirmation_required` unless `-f/--force` is given.

## 2. `--json` envelope (schema v1)

```json
{
  "schema_version": 1,
  "command": "search",
  "ok": true,
  "data": { },
  "error": null,
  "meta": { "cached": false, "stale": false, "fetched_at": "2026-11-20T12:00:00Z", "cli_version": "0.1.0" },
  "warnings": []
}
```

On failure: `"ok": false`, `"data": null` (or partial data for batch downloads), and
`"error": {"code": "not_found", "message": "…", "retryable": false, "exit_code": 3, "details": {}}`.
`message` is always English (stable for logs); localized text appears only in human output.

**Compatibility:** additive changes (new fields, new error codes, new commands) keep `schema_version: 1`.
Renaming, removing or changing the type of a field bumps the version. Consumers must ignore unknown fields.

## 3. `data` per command

Entries and links use the API v1 field names exactly (api-client §1), so `data` can be fed back to other tools.

| Command | `data` |
|---|---|
| `search` | `{query:{text,platforms,regions,serial,sort}, page, page_size, total_pages, total_results, past_last_page, dropped, results:[{index, …Entry}]}` (`dropped`: results hidden by the slug rule, app-service §3) |
| `info` | `{entry: Entry}` |
| `random` | `{entries: [Entry]}` |
| `platforms` | `{platforms: [{id, name, brand, class}]}` sorted by id |
| `regions` | `{regions: [{id, name}]}` |
| `db-info` | `{total_entries}` |
| `version` (and `--version`) | `{version, commit, date, go, os, arch}` (§3.1) |
| `download` | `{jobs: [{slug, link_index, filename, raw_filename, path, state, bytes, expected_bytes, sha256, crc:"ok|skipped|failed", hash:"ok|skipped|failed", attempts, duration_ms, error:{code,message}|null}]}` (`filename` is the sanitized name; `raw_filename` is the received one, control characters stripped in human output) |
| `boxart` | `{slug, path}` |
| `history` | `{records: [HistoryRecord]}` (same fields as a `download` job + `id, started_at, finished_at, platform, title, source_name`) |
| `cache stats` | `{namespaces: [{name, items, bytes, expired}], total_bytes, max_bytes}` |
| `cache clear` | `{cleared: [ns]}` |
| `config list/get` | `{settings: [{key, value, source}]}` |
| `profile show/detect` | `{profile: {id, name, host, source, rom_root, verified, warnings[]}, unverified: [string]}` |

### 3.1 Version information

`mothdex version` and the global `--version` print the same data; with `--json` it is the `data` object of the
standard envelope, and `meta.cli_version` always equals `data.version`.

| Field | Content | Dev build (`go build`/`go run`, no ldflags) |
|---|---|---|
| `version` | SemVer **without** the leading `v` (`0.1.0`, `0.2.0-rc.1`), the same as the release artifact name ([`release-artifacts.md`](release-artifacts.md)) | `""` (`go install` builds use the module version from `runtime/debug.ReadBuildInfo` when present) |
| `commit` | full 40-hex commit SHA | `""`, or `vcs.revision` from build info when present |
| `date` | **commit** date (not build time; builds are reproducible), RFC 3339 UTC | `""`, or `vcs.time` when present |
| `go` | Go toolchain version (`runtime.Version()`, e.g. `go1.26.3`) | always set |
| `os`, `arch` | `runtime.GOOS`, `runtime.GOARCH` (`arm` for armv6/armv7) | always set |

Unknown values are empty strings, never placeholders such as `none` or `unknown`. Human output:
`mothdex 0.1.0 (commit 1a2b3c4, 2026-11-20)` (short SHA and date only), or `mothdex dev` when `version` is empty.
The values live in `internal/buildinfo` (set with `-X` ldflags), so every package can read them.

## 4. Error codes and exit codes

| Exit | Meaning | `error.code` values |
|---|---|---|
| 0 | success (including `Skipped` downloads) | — |
| 1 | unexpected internal error | `internal` |
| 2 | usage or configuration error | `usage`, `invalid_config`, `invalid_request`, `confirmation_required`, `unmapped_platform` |
| 3 | not found | `not_found`, `no_session`, `index_out_of_range` |
| 4 | network or service unavailable | `network`, `rate_limited`, `upstream`, `api_incompatible`, `untrusted_url`, `untrusted_redirect` |
| 5 | integrity | `size_mismatch`, `integrity` |
| 6 | local filesystem | `permission_denied`, `disk_full`, `exists`, `path_rejected`, `unsafe_name` |
| 7 | partial: batch with some jobs failed | the first failure code, plus all of them in `data.jobs[].error` |
| 130 | interrupted (SIGINT/SIGTERM, `canceled`); wins over 7 when a batch is both interrupted and has failures | `canceled` |

`error.exit_code` in the `--json` envelope is the process exit code (7 for a partial batch, whatever the first failure's own
code maps to). `warnings[]` carries every warning also printed on stderr. `--json` escapes the C1 controls U+0080 to
U+009F as `\u00XX` besides what the encoder already escapes.

Error codes are lowercase snake_case, stable, and are also the i18n keys under `errors.<code>`.

## 5. Export (`search`, `history`)

Syntax: `-e, --export <path>` plus `--format txt|json|csv` (default: inferred from the extension; error if it
cannot be inferred). If `<path>` is an existing directory, the file is `<path>/mothdex-<command>-<UTC yyyymmddThhmmss>.<fmt>`.
This replaces the legacy two-value flag `-e <fmt> <path>`, which Cobra cannot express (accepted by Regente).

- **json**: the same envelope as `--json` (so `schema_version` is included).
- **csv**: RFC 4180, UTF-8 without BOM, header row, one row per entry or record. List values are joined with `;`.
  Cells starting with `=`, `+`, `-`, `@`, tab or CR get a leading `'` (formula injection: titles are external data),
  after the control-character stripping above.
  - search columns: `index,slug,title,platform,regions,serial,links,best_link_filename,best_link_size`
  - history columns: `id,started_at,finished_at,state,slug,title,platform,filename,path,bytes,sha256,crc,attempts,error_code`
- **txt**: the human table without colors, at a fixed width of 120 columns.
- Never overwrites an existing file without `-f/--force`; the write is atomic (temp file plus rename).

## Resolved questions (Regente, T-2 phase 1)

- OQ-OUT-1: `download --json-events` (NDJSON progress) is deferred to the backlog.
- OQ-OUT-2: no CSV BOM option.
