# Contract: session logs, crash files and error refs (`internal/log`)

Status: draft v1 (T-111) · Owner: Alicerce · Implementers: Bigorna (T-114, core and `mothdex logs`), Vitral (T-115,
GUI) · Risk: High (personal data in a file people share) · Spec: `beta-adjustments-spec.md` §4 · Threat model:
[`T-110`](../../security/T-110-threat-model-beta-adjustments.md) TM-16 to TM-28. Existing rules stay:
`mothdex.log` with rotation, `log.*` keys (config.md §2), and a logging failure never stops the program.

## 1. Files under `paths.logs`

```
mothdex.log                       # rotated history at log.level (unchanged)
latest.log  latest.lock           # full log of the newest session; owner token (§3)
sessions/<id>.log                 # one file per process, at log.session_level
crash/<id>.txt                    # panic only: header, value, ref, full stack
bundles/mothdex-logs-<yyyymmdd-hhmmss>.zip
```

- **Session id:** `<yyyymmdd>-<hhmmss>-<binary>-<pid>`, with the time in UTC (`20261123-014700-cli-12345`) and
  `binary` either `cli` or `gui`. Files are created with `O_EXCL`; if the name is taken, the id gets `-2`, `-3`, and
  so on. A session is one process. A whole shell or TUI run is one session.
- **File safety (TM-20).**
  - Folders are `0700` and files `0600`.
  - Session and crash files are created with `O_CREATE|O_EXCL`.
  - `latest.log` and `latest.lock` are opened without following links: `O_NOFOLLOW` on POSIX, and a reparse point is
    refused on Windows.
  - A log file that this process did not create with `O_EXCL` is used only when it belongs to the user and has a
    single link (one name). Otherwise it is refused.
  - **Unsafe folder.** A `paths.logs`, or one of its subfolders, that is a link, is not owned by the user, or is
    writable by group or others (the sticky bit is no exception) is refused.
    - The session then falls back to the default `paths.logs` (config.md §3), and the warning names both folders.
    - If the default folder is refused too, file logging is disabled with that one warning, and the command still
      runs.
  - **Exception:** FAT, exFAT and NTFS mounts have no permission bits; this is proven with `statfs`, never by
    reading the mode. On those, the mode check is skipped, and the session header plus `logs path` carry a WARN line
    `log folder has no file permissions`.
  - **Windows:** a folder, or a file this process did not create, must have one of these owners: the current user,
    the token's default owner (Administrators when the process runs elevated), SYSTEM (`S-1-5-18`), Administrators
    (`S-1-5-32-544`) or TrustedInstaller. This is the same trusted set as install.ps1 and the managed gate
    (install-state.md §4). Volumes without access lists (FAT,
    exFAT, which have no `FILE_PERSISTENT_ACLS`) skip this check, with the same WARN line as above.
- **Size caps (TM-23).** A session file stops at 20 MiB, after one `log truncated` line. A crash file stops at 1 MiB.
- **No session file for:** the cobra `__complete` and `__completeNoDesc` commands, `completion`, `version`, `--version`
  and every `logs` subcommand, plus `mothdex-gui --probe`, `--version`, `--spike` and `--help`. Tab completion, the
  upgrade check of the new binary (update.md §4) and looking at the logs must not rotate sessions away or take
  `latest.log`.
  - The CLI ones run as a **general-only session** (`SessionOptions.GeneralOnly`). It creates no session file and
    never takes `latest.log`.
  - It writes only its ERROR lines, refs included, to `mothdex.log`, opened at the first error. So every ref it shows
    can still be found (§5).
  - The GUI ones write nothing at all.
- **Windows:** every log file is opened through `x/sys/windows.CreateFile` with
  `FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE`. A tester can copy the file while the program runs, and
  another process can rotate or delete it.

## 2. Lifecycle and API

The session starts in `main` of `cmd/mothdex` and of `cmd/mothdex-gui`, before cobra and before `compose.Open`. That
way, configuration and flag errors are logged too. Until `Attach`, lines are kept in memory, up to 256 KiB; beyond
that the oldest go and a `lines dropped` marker takes their place. This memory buffer is the GUI's fallback log: when
the configuration fails, `Close` writes the buffer to the default folder.

```go
func StartSession(o SessionOptions) *Session                  // never nil; Binary "cli"|"gui", Args, buildinfo, Now
func (s *Session) Attach(a AttachOptions) error               // Dir, Level, Keep, Secrets []string, Home; opens files
func (s *Session) Update(i Info)                              // Mode command|shell|tui|gui, Profile, Lang, ConfigSource
func (s *Session) Handler() slog.Handler                      // session file + latest.log, already redacting
func (s *Session) LogPath() string                            // latest.log while owned, else the session file
func (s *Session) SessionPath() string
func (s *Session) WriteCrash(recovered any, stack []byte, ref string) (string, error)
func (s *Session) Close() error                               // writes "session end" with duration and error count
func NewRef() string; func ParseRef(in string) (string, error)
func RecordError(l *slog.Logger, r ErrorRecord)               // Ref, Code, Cmd, Message (English), Cause
```

- **`compose.Options` gets a `Logs *log.Session` field.** It is not called `Session`, because `Options.Session` is
  already a bool. `compose.Open` resolves the configuration and calls `Attach`
  with `paths.logs`, `log.session_level` and `log.sessions_keep`. It then builds the process logger: the session
  handler plus the `mothdex.log` handler at `log.level`, plus the console handler when `log.console` is set.
  `compose.Session.Logs` carries the session. `compose.Session.LogPath` stays and still names `mothdex.log`, which
  the GUI uses; the path shown with an error is `Logs.LogPath()`.
- **`Attach` failures** (folder not writable, bad level) are returned once as a warning. The handler then discards
  lines, so nothing ever stops the program.
- **`Attach` without a configuration** (it failed to load): `Close` attaches to the default `paths.logs`, from
  `config.ResolvePaths` with an empty `Config`, which honours portable mode.

**Header.** The first line of every session file and of `latest.log`:
`msg="session start"` with `session`, `version`, `commit`, `build_date`, `os`, `arch`, `binary`, `pid`, `args`, and
`started_at` (RFC 3339 local time, with offset). After `Update`, a `msg="session info"` line adds `mode`, `profile`,
`lang` and `config_source` (`defaults`, or the file path with the home folder replaced by `~`). `mothdex logs list`
reads the last `session info` line.

**`args` is an allowlist (TM-17), masked inside the package.**

- Flag names are always logged.
- A flag's value is logged only when the flag carries the cobra annotation `mothdex.log_value`. Forja sets it on
  non-sensitive flags such as `--json`, `-m`, `--platform` and `--link`.
- Everything else becomes `<redacted>`: unknown flags, URL-shaped values, `--config` and `-o` paths, and the value of
  `config set`.
- Positional arguments are kept, with the home folder replaced by `~`. They are search terms and slugs.

## 3. `latest.log` ownership

- **Token.** `latest.lock` holds the owner token `<pid>-<process start, unix ns>`.
- **Recovery uses OS lock semantics only** (TM-21): a lock held by a dead process is released by the OS, and PID
  liveness is never checked.
- **Taking over, inside `Attach`.** The process takes, waiting at most 1 s, an exclusive OS lock on `latest.lock` (`flock` or `LockFileEx`,
  from `x/sys`), writes its token, truncates `latest.log` and writes its buffered lines. Then it releases the lock.
  The newest process to start always wins. If the lock cannot be taken in time, this process runs without
  `latest.log`.
- **Each write to `latest.log`.** The writer takes the same lock, waiting at most 100 ms, and reads the token from its
  open lock handle with `ReadAt`. It writes only if the token is still its own, then releases. A different token means
  ownership is lost for good: the handle is closed, and `LogPath` switches to the session file. If the lock times out,
  that one line is skipped in `latest.log` only, and is still in the session file.
- **Writes are independent.** The session file and `mothdex.log` never wait on this lock.
- **Test:** two processes or goroutine fakes starting in turn. After the handover, `latest.log` holds only the second
  session from its header on, and neither session file is missing a line.

## 4. Levels, retention, keys

| Key | Values | Default | Env |
|---|---|---|---|
| `log.session_level` | `debug`, `info`, `warn` or `error`; applies to the session file and `latest.log` | `debug` (beta) | `MOTHDEX_LOG_SESSION_LEVEL` |
| `log.sessions_keep` | 1 to 100 | 20 | `MOTHDEX_LOG_SESSIONS_KEEP` |

- **`mothdex.log`** keeps following `log.level`. GUI `-log-level <lvl>` sets `log.level` for that run, with flag
  source.
- **Retention runs in `Attach`.** It keeps the newest `log.sessions_keep` files in `sessions/`, the newest 10 in
  `crash/` and the newest 5 in `bundles/`, by name. It never deletes the session's own file. Deletes are best effort.
- **Retention never deletes a live session (backlog Crivo 142-6).** While a session runs, it holds an exclusive OS
  lock on its own session file: `flock`, or `LockFileEx` on one byte at offset 2^30, which sits beyond the data, so
  readers are not blocked.
  - Retention and `logs clear` try a non-blocking lock on each candidate, and skip a file whose lock is held.
  - So `sessions/` may hold more than `log.sessions_keep` files while several processes run. The excess goes at a
    later `Attach`.
  - The OS releases the lock of a crashed process, so its file becomes deletable.
- **Deletion is pattern-bound (TM-22).** Retention and `logs clear` delete only regular files whose names match the
  exact session, crash or bundle pattern, inside `sessions/`, `crash/` and `bundles/`. They never recurse, never
  follow links and never touch `paths.logs` itself.

## 5. Crash files and error refs

- **Recover in each `main`.** The CLI root and the GUI `runGUI` `recover()`. They call `NewRef`, write an ERROR line
  with `ref`, `code=internal`, `panic` and `stack` (from `debug.Stack()`), call `WriteCrash`, and exit 1. `WriteCrash`
  writes `crash/<id>.txt` with the header, the panic value, the ref and the stack, then `fsync`s it.
- **Panics in other goroutines** are caught through `debug.SetCrashOutput(<session file>)`, so the trace ends up in the
  session file. They get no `crash/` file in v0.2 (gap G-LOG-1).
  - The runtime writes that trace as it is, so it is **not redacted** inside the local session file, which has mode
    `0600` (residual risk, Sentinela).
  - Every way out of that file is redacted again: `logs show`, `logs last` and the bundle.
- **Refs.** A ref is 6 characters of Crockford base32 (`0123456789ABCDEFGHJKMNPQRSTVWXYZ`, no `I`, `L`, `O` or `U`),
  uppercase, from `crypto/rand`, with no PID, time or user in it (TM-26).
  - The regex is `^[0-9ABCDEFGHJKMNPQRSTVWXYZ]{6}$`. This replaces the 30-symbol alphabet in TM-24 (agreed with
    Sentinela): `0` and `1` cannot be misread, because `O`, `I` and `L` never appear.
  - 30 bits make a collision unlikely, not impossible.
  - A ref is a lookup key, never an authorization.
- **Parsing.** `ParseRef` uppercases its input, maps `O` to `0` and `I`/`L` to `1`, drops spaces and `-`, and returns
  `usage` for anything that is not then 6 valid characters. A ref is unique only in practice: `logs show --ref`
  without a session searches every session and then `mothdex.log`, where the errors of general-only sessions are,
  and shows every match.
  - Unreadable session files are skipped.
  - Only the matching lines and their context are redacted for display.
- **One ref per error occurrence shown**, assigned where the error is rendered: CLI `output.go`, the shell, the TUI and
  the GUI error screen. Each job of a batch download that fails gets its own ref.
- **`RecordError` runs before the error is printed.** It writes
  `level=ERROR msg="<English message>" ref=7F3K2Q code=network cmd=search cause="…"`, where `cause` is the full
  `%v` chain after redaction.
- **Display is sanitized (TM-25).** `logs show`, `logs last`, the GUI error screen and banners strip C0/C1 controls
  and ESC before display. slog quotes values, so a record never holds a raw newline.
- **Human output** (CLI, shell, TUI): `código: network · ref: 7F3K2Q` followed by `log: <LogPath>`. The path uses `~`
  on Unix, and `%LocalAppData%` on Windows when it sits below that folder. When this process no longer owns
  `latest.log`, the session file is shown instead.
- **GUI:** the error screen shows `code · ref` and the same path.
- **`--json`:** `error.ref` and `data.jobs[].error.ref` are added. The change is additive, so `schema_version` stays 1
  (cli-output.md §2).

## 6. Privacy and redaction

- **One redactor, at write time (TM-16).** A handler wrapper (`ReplaceAttr` plus a scan of string and error values)
  sits in front of every sink: `mothdex.log`, the session file, `latest.log`, crash files, the fallback buffer and the
  console copy. It is needed because `url.Error` text embeds the API URL.
  - The `api.base_url` value, any URL under it, its host and every `Secrets` value become `<api>`.
  - Every other URL loses its userinfo, query and fragment.
  - `key=value` secrets outside a URL lose their value: a key ending in `token`, `signature`, `password`, `passwd`,
    `secret` or `api_key`, plus `sig` and `x-amz-credential`.
  - Values are dropped for `Authorization`, `Proxy-Authorization`, `Cookie`, `Set-Cookie`, `X-Api-Key` and any
    header whose name contains `token`, `key`, `secret` or `auth`.
  - The values of `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY` and any other environment value are never
    logged.
  - Request and response bodies, and full header sets, are never logged, at any level.
- **Bundle (TM-18).** The bundle runs the same redactor again over every copied file, because v0.1.x logs were never
  redacted.
  - The home folder becomes `~`, or `%USERPROFILE%` on Windows, in every spelling: both slash forms, JSON-escaped
    backslashes, case-insensitive on Windows, the `\\?\` prefix, and the 8.3 short form when it can be resolved.
  - The user name is replaced wherever it is a path component (`/run/media/<user>/`, `C:\Users\<user>`).
  - The host name is never written anywhere.

## 7. `mothdex logs` (`--json` data)

| Subcommand | `data` |
|---|---|
| `path` | `{dir, latest, session}` |
| `last [--tail N]` | `{session, lines: [string]}`: the newest session other than the running one |
| `list` | `{sessions: [{id, binary, mode, started_at, duration_ms, errors, current}]}`, newest first |
| `show <id> [--ref R]` | Without `--ref`: `{session, lines: [string]}`. With `--ref`: `{session, matches: [{ref, line, context: [string]}]}`, with 10 lines of context on each side. `<id>` must match the session pattern and exist in `sessions/`; `/`, `\` and `..` are `usage` (TM-24). |
| `bundle [-o dir] [--sessions N]` | `{path, bytes, sessions: [id]}`. N runs from 1 to 20, default 5. |
| `clear [-f]` | `{removed: {sessions, crashes, bundles}}`. Without `-f` it asks for confirmation (`confirmation_required` under `--json`). |
| `open` | `{dir}`. It opens the folder with argv only (`explorer.exe <dir>`, `open <dir>`, `xdg-open <dir>`), never through a shell (TM-28), and fails with `usage` when there is no GUI session. |

**Bundle.** The zip holds `manifest.json`, `README.txt`, `sessions/`, `crash/` and `mothdex.log.tail`.

- **The content is an allowlist (TM-19).** Entry names are fixed, or a base name that has been validated.
- **Manifest:** `schema_version: 1`, `version`, `commit`, `os`, `arch`, `profile` (as configured, so it may be `auto`), `lang`, the configuration from
  `config list`, and the names (never the values) of the `MOTHDEX_*` variables that are set.
  - Configuration keys are shown in clear only when they are on an allowlist. Every other key is masked, including
    keys added later; a test fails on any schema key that is not classified.
- **What goes in:** the tail of `mothdex.log` is the last 1 MiB, and each session file is capped at its last 2 MiB.
  Files are added newest first. The 10 MiB limit is enforced while writing, in this drop order:
  1. crash files, oldest first;
  2. sessions, oldest first;
  3. the `mothdex.log` tail is shortened.

  The newest session is kept, and so is a non-empty tail.
- **No network.** `README.txt` says the issue will be public and asks the tester to review the zip first.
- **`-o`** must be an existing folder that is not a link.
- **How it is written:** to a temporary name, then renamed, with `O_EXCL` on the final name and mode `0600`. It never
  overwrites an existing file.
- **GUI "Export logs"** calls the same bundle code after a confirmation (TM-27).

## 8. Tests that matter

- The `latest.log` handover (§3).
- Redaction of the API URL inside an `url.Error` at write time.
- A crash file from a forced panic, behind the build tag `crashtest`.
- A bundle under 10 MiB with no API URL, no token and no home path.
- `ParseRef` normalization.
- Retention never deleting the session's own file.
