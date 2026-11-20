# Contract: configuration (`internal/config`)

Status: draft v1 (T-2 phase 1) · Owner: Alicerce · Implementer: Forja (T-11) · Risk: Medium
Starting point: [legacy `CLIDOWNROM` `config.yml`](https://github.com/Or4cu1o/CLIDOWNROM/blob/main/source/config/config.yml). System profiles and their `profile.*` keys: [`profiles.md`](profiles.md).

## 1. Precedence

`defaults (code) < YAML file < MOTHDEX_* environment < command-line flags`, merged per key (koanf).
`config list` shows each effective value with its source: `default | file | env | flag`.

Config file location, first match wins: `--config <path>` › `MOTHDEX_CONFIG` › portable mode › OS default (§3).
A missing file is fine (defaults apply). The file is created only by `config set` or `config reset`.

## 2. Schema v1 (YAML, with defaults)

```yaml
version: 1                      # schema version; unknown higher version => error with exit code 2
api:
  base_url: https://api.mothdex.net/v1
  timeout: 15s                  # per attempt
  rate_limit: 5                 # req/s, clamped 1..10
  max_retries: 2                # 0..5
download:
  dir: ""                       # "" = active profile destination (profiles.md §4); generic: <paths.data>/ROMs/<platform>, portable: <exe dir>/ROMs/<platform>
  concurrency: 2                # 1..6
  max_attempts: 5               # 1..10
  not_found_retries: 3          # 0..5
  first_byte_timeout: 10m       # 30s..15m
  idle_timeout: 5m              # 30s..15m
  on_exists: skip               # skip | rename (up to 99) | overwrite (honored only with --force)
  max_file_size: 256GiB         # larger links are rejected (T-16 CR-5); sizes accept KiB/MiB/GiB
  verify: true                  # zip CRC + future hashes; size is always checked
  boxart: true
  allowed_hosts: [proxy.mothdex.net]   # https only, exact host match; v0.1 proxy-only (downloader §2)
  prefer:
    languages: []               # e.g. [En, Pt]
    formats: []
    sources: []
    size: none                  # none | smallest | largest (ux/prefer-syntax.md §3; ranking step 4)
cache:
  enabled: true
  max_size_mb: 200              # 10..10240
  ttl: { search: 24h, entry: 24h, meta: 168h, info: 1h, covers: 720h }
paths:                          # "" = OS default (§3)
  temp: ""
  data: ""                      # history.jsonl
  logs: ""
interface:
  language: auto                # auto | <xx_yy> (case-insensitive)
  color: auto                   # auto | always | never (NO_COLOR is honored)
  confirm_downloads: true
  page_size: 10                 # 1..100
profile:                        # see profiles.md §6
  id: auto
  rom_root: ""
  retrobat_path: ""
  folders: {}                   # file / `config set` only (open-ended map, no env mapping)
  refresh: true
log:
  level: info                   # debug | info | warn | error
  file: true
  console: false
  max_size_mb: 10
  max_files: 5
```

Dropped from the legacy file: `chunk_size`, `preferred_hosts`, `mirror_test_*`/`performance.*` (there are no mirrors),
`theme` (Bússola may bring it back). Durations use Go syntax (`90s`, `10m`, `168h`).

## 3. OS default paths

| | Config file | `paths.temp` (cache, `.part`) | `paths.data` | `paths.logs` |
|---|---|---|---|---|
| Linux / BSD | `$XDG_CONFIG_HOME/mothdex/config.yaml` (`~/.config`) | `$XDG_CACHE_HOME/mothdex` (`~/.cache`) | `$XDG_STATE_HOME/mothdex` (`~/.local/state`) | `<data>/logs` |
| Windows | `%AppData%\mothdex\config.yaml` | `%LocalAppData%\mothdex\cache` | `%LocalAppData%\mothdex` | `%LocalAppData%\mothdex\logs` |
| macOS | `~/Library/Application Support/mothdex/config.yaml` | `~/Library/Caches/mothdex` | `~/Library/Application Support/mothdex` | `~/Library/Logs/mothdex` |

**Portable mode:** if a `mothdex.yaml` file sits next to the executable, it is the config file and every empty path
resolves under `<exe dir>/mothdex-data/`, except `download.dir`, which defaults to `<exe dir>/ROMs/<platform>` (legacy layout). This serves RetroBat and USB installs. Emulation systems (Batocera
`/userdata`, EmuELEC `/storage`, …) get their paths from the detected profile ([`profiles.md`](profiles.md)).

## 4. Environment variables

Rule: `MOTHDEX_` + the key path in upper case, with `.` replaced by `_`. Example: `download.first_byte_timeout` →
`MOTHDEX_DOWNLOAD_FIRST_BYTE_TIMEOUT`; `cache.ttl.search` → `MOTHDEX_CACHE_TTL_SEARCH`. Lists are comma-separated.
Because the schema is closed, the mapping is a fixed table generated from it; a test asserts that no two keys map
to the same variable. Unknown `MOTHDEX_*` variables log a warning. Reserved: `MOTHDEX_CONFIG` (file path).

## 5. Flags

Global flags with a config equivalent: `--lang` (`interface.language`), `--no-color` (`interface.color=never`),
`--refresh` (per command: skip cache reads, not persisted), `--profile` (`profile.id`), `--config`. Command flags override their keys for that run only:
`-j/--jobs` (`download.concurrency`), `-o/--output` (`download.dir`), `--no-boxart` (`download.boxart=false`),
`-m/--max-results` (`interface.page_size`).

**Safety flags with no config or env equivalent** (T-16): `-f/--force` (allows `on_exists: overwrite` and skips
confirmations) and `--allow-executable` (downloader §6.1). They apply to one run only and are never persisted.

## 6. Validation and writing

- Type and range errors give exit code 2 with `key`, the bad value and its source (`file:line` when known).
- Unknown keys in the file are a warning, not an error (forward compatibility).
- `config set <key> <value>` validates against the schema, then rewrites the YAML file atomically. Comments are **not**
  preserved; this is documented in `config --help`. `config reset <key|all>` removes the key from the file.
- No secrets exist in this schema; any future secret goes in an environment variable only, never in the file.

## Resolved questions (Regente, T-2 phase 1)

- OQ-CFG-1: the generic default is `<paths.data>/ROMs/<platform>`; portable mode keeps `ROMs/<platform>` next to the executable. System profiles override it (profiles.md).
