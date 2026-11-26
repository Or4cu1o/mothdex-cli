# T-110 Threat model: beta adjustments (mothdex-cli v0.2.0)

Author: Sentinela, 2026-11-26. Before coding. Base: origin/main 2403c84 (install.sh, install.ps1, internal/log,
cmd/mothdex-gui/gui.go, docs/release.md, T-16, T-70). Spec: docs/contracts/beta-adjustments-spec.md (authoritative),
plan beta-adjustments-plan.md. Owner decision D34: pipe install is allowed; not argued here.
Severity of each control if missing at audit time = the level in brackets (risk-process scale). T-16 and T-70 controls
(TH-*, IC-1..IC-13) stay in force; nothing below relaxes them.

Method: data flow + STRIDE on the changed surfaces only, against the current code. Tools: manual review of the files
above, git. Not run: no scanner applies to a design that does not exist yet. Not verified: GitHub asset host names
and immutable-release behaviour on releases/latest/download (re-check on the first v0.2.0 release).

## Surface 1: one-line install, PATH, rc files, alias, state file, uninstall (Guindaste, T-113; state format Alicerce, T-111)

Trust boundary that moves: with a pipe the script runs before anything verifies it. The SHA-256 / cosign / gh checks
inside the script protect the binaries, not the script. Everything below limits damage around that fact.

- TM-1 [Major] Truncation-safe body. install.sh: all logic in functions, a single last line `main "$@"`; install.ps1:
  whole body in one scriptblock/function invoked on the last line. Test: feed the script cut at every 10% point to
  sh / iex; nothing is installed or written, exit != 0 (or parse error), no PATH/rc change.
- TM-2 [Major] stdin is the script under curl|sh. No `read` anywhere; every external command the script runs (curl,
  wget, tar, cosign, gh, the installed `mothdex --version`, `mothdex-gui --probe`, fish) gets `</dev/null`. Test:
  run via `cat install.sh | sh` with a shim cosign/gh/mothdex that reads stdin; install completes and the shim saw EOF.
- TM-3 [Major] No session leakage under iex. Set-StrictMode, $ErrorActionPreference, $ProgressPreference, helper
  functions (Fail, Get-Remote, ...) and variables stay inside the child scope; no `exit` anywhere (throw / return with
  $global:LASTEXITCODE = 1). [Net.ServicePointManager]::SecurityProtocol: add Tls12 without removing what was set, and
  do not restore a weaker value. Test (PS 5.1 and 7): after a failing and a succeeding `irm|iex`, the window is open,
  $LASTEXITCODE != 0 on failure, Get-Command Fail / Get-Remote finds nothing, StrictMode and EAP equal their prior
  values.
- TM-4 [Major] install.cmd forwards arguments without code injection. %* must never be pasted into the -Command
  string. Accept only the known switches (-Version, -BinDir, -RetroBatPath, -Alias, -NoAlias, -NoGui, -Gui, -NoLauncher,
  -Launcher, -NoModifyPath, -DryRun, -Uninstall) and validate values (version regex, path rules of Resolve-BinDir, alias
  regex) in batch, or pass them through an environment variable / -args that the scriptblock parses as data. Exit code
  of PowerShell is returned (exit /b %errorlevel%). Set TLS 1.2 before irm. Test: `install.cmd -Version "1.0.0;calc"`,
  `install.cmd "$(calc)"`, `install.cmd -BinDir "C:\a & calc"` run nothing and exit != 0.
- TM-5 [Major] Installer version = release version. The publish pipeline stamps each installer asset with its own tag;
  default install is that stamped version (no re-resolution of latest), so the script and its NOTICE_NAMES/archive rules
  always match. Installer assets are listed in checksums.txt (so signed + attested) and uploaded only by the gated
  publish job; verify-dist checks they are present and stamped. Test: verify-dist fails on an unstamped or unlisted
  installer; install from release N while N+1 is latest installs N.
- TM-6 [Minor] One canonical URL, https only. Docs show only https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.<ext>
  (one place); no -k/--insecure/-SkipCertificateCheck anywhere. A future short domain only 30x-redirects to that URL
  (HTTPS, HSTS, registrar lock) and is added to the TCB in the ADR. "Download, verify (gh attestation verify install.sh
  / cosign on checksums.txt), read, run" stays documented. Test: grep of README/docs finds the URL once and no
  insecure flag.
- TM-7 [Major] PATH write only for a trusted directory. The folder added to PATH (user PATH or rc line) must pass the
  existing dir_safe / Test-FolderTrusted rules at the moment of writing (owned by the user, not writable by others, not
  a link). Never Machine PATH; elevation is not requested; when elevated, warn and still write only the user PATH.
  Test: -BinDir on a folder writable by Users / a world-writable dir: PATH unchanged, copy-paste block printed;
  Machine Path byte-identical before/after.
- TM-8 [Major] Windows user PATH preserved byte-for-byte except our entry. Read the raw value from HKCU\Environment
  without expanding (DoNotExpandEnvironmentNames), keep its kind (REG_EXPAND_SZ stays REG_EXPAND_SZ), append our entry
  once (dedup case-insensitive, trailing-slash normalised, also matching an expanded equal), then broadcast
  WM_SETTINGCHANGE. [Environment]::GetEnvironmentVariable('Path','User') + Set... expands %VAR% entries and rewrites the
  kind: do not use it for the write. Refuse to write (print the block) when the read fails or the result exceeds
  32767 chars. Never setx. Test: user Path with %USERPROFILE%\bin and REG_EXPAND_SZ stays unexpanded and same kind;
  second run does not duplicate; uninstall restores the prior value exactly.
- TM-9 [Major] rc line is a fixed template. Exactly `export PATH="<dir>:$PATH" # mothdex` (fish: run `fish -c` with
  the dir as argv, never interpolated into fish code). <dir> must pass valid_path (no quote, $, backtick, newline,
  space). Append only when the rc file (after resolving links) is a regular file owned by the current user and not
  group/world-writable; otherwise print the block. When uid is 0 and HOME is not owned by uid 0 (sudo), do not touch
  rc files. Test: bin_dir with $, quote or newline is refused; rc owned by another user / mode 0666 unchanged;
  `sudo sh install.sh` leaves the user rc untouched.
- TM-10 [Major] Uninstall removes only what was added. rc: delete only lines equal to the exact recorded line (not any
  line containing "mothdex"); rewrite atomically keeping mode and owner. Windows PATH: remove only the exact recorded
  entry, once. fish: remove only the recorded path. Test: rc with user lines `alias m=mothdex # mothdex` and our line;
  uninstall leaves the user line; PATH entries of others unchanged.
- TM-11 [Major] Order: PATH, rc and alias are changed only after the binaries are installed and `--version` passed; any
  earlier failure leaves PATH, rc, alias and state untouched. Test: tampered checksums.txt / probe failure path leaves
  rc, HKCU Path and bin_dir alias unchanged.
- TM-12 [Major] Alias never overwrites or deletes foreign files. Create only if `<bin_dir>/<alias>` does not exist, or
  the state file records it as ours AND it is a symlink to `mothdex` (POSIX) / a file named in state (Windows).
  Conflict check: command -v / Get-Command resolving to anything not ours (including builtins, cmdlets, aliases,
  functions visible to the installer) = occupied, then mthd, then none. Create atomically (temp name + rename).
  Name rule ^[a-z][a-z0-9-]{1,15}$ plus deny-list: mothdex, mothdex-gui, Windows device names (con, prn, aux, nul,
  com0-9, lpt0-9). Test: fake mdex in PATH and a user file at <bin_dir>/mdex: neither changed, mthd used; -Alias nul /
  -Alias ../x refused.
- TM-13 [Major] Upgrade/reinstall recreates the Windows hardlink to the new mothdex.exe (rename a locked old link to
  .old) and checks it is the same file (same file index) as mothdex.exe; POSIX symlink is relative (`mothdex`). Test:
  after upgrade `mdex --version` prints the new version.
- TM-14 [Major] State file is data, never code. Fixed name in bin_dir, written atomically, 0644, no secrets. install.sh
  never sources or evals it: parse key=value with a strict per-key regex (or JSON read only in Go). Every value is
  re-validated on read (alias regex + deny-list, dir == bin_dir, rc path must be one of ~/.bashrc ~/.zshrc
  ~/.config/fish/... under the current HOME, PATH entry == bin_dir). Invalid state = treat as absent, warn, touch nothing
  recorded in it. Test: state with alias=../../etc/x, rc=/etc/profile, a line containing $(touch /tmp/p): uninstall
  removes none of those and /tmp/p is not created.
- TM-15 [Minor] install.cmd file hygiene: documented line downloads to %TEMP% (not CWD) or the script refuses to run when
  it is not where it was saved; it never deletes anything but itself. Owner may keep the spec line; then residual R7.
  Test: running from a folder with another install.cmd is not affected by it.

## Surface 2: session logs, latest.log, crash files, ref, logs command, bundle (Bigorna T-114, Vitral T-115)

- TM-16 [Major] One redaction point at write time for every sink (mothdex.log, session file, latest.log, crash file,
  fallback log, console copy): a slog handler wrapper (ReplaceAttr + value scan) that (a) replaces the configured
  api.base_url and any URL under it with <api>; (b) strips userinfo, query and fragment from every other URL, including
  inside *url.Error / error strings (Get "https://u:p@h/x?token=..."); (c) drops values of Authorization,
  Proxy-Authorization, Cookie, Set-Cookie, X-Api-Key and any header whose name contains token, key, secret, auth;
  (d) never logs values of HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, NO_PROXY or any env value (names of MOTHDEX_* only).
  Never log request/response bodies or full header sets at any level, debug included. Test: a fake server returning a
  signed download URL ?token=S3CRET, a proxy URL with u:p, and an api.base_url with userinfo: after a debug-level
  session, grep of all log files and the bundle finds no S3CRET, u:p or API host.
- TM-17 [Major] Session header args are allowlisted. Log flag names always; values only for flags in a non-sensitive
  allowlist; positional args of `config set` (value), anything URL-shaped and unknown flags are masked. Test: `config
  set api.base_url https://u:p@x` and `--config /home/bob/c.yaml` do not appear raw in the header.
- TM-18 [Major] Bundle privacy. Replace the home dir in every spelling (both slash forms, JSON-escaped backslashes,
  case-insensitive on Windows, \\?\ prefix, 8.3 short form when resolvable) by ~ / %USERPROFILE%; also the user name as
  a path component (e.g. /run/media/<user>/, C:\Users\<user>), and never write the hostname anywhere. The bundle re-runs
  the TM-16 redactor over every copied file (logs written by v0.1.x had no redaction). Manifest config: allowlist of keys
  shown in clear; every other key, including keys added later, is masked (a test enumerates schema keys and fails on an
  unclassified one). Test: fixtures with C:/Users/Bob/..., C:\\Users\\bob\\..., /run/media/bob/: bundle contains no bob;
  a v0.1.x-style mothdex.log with the API URL is redacted in the bundle.
- TM-19 [Minor] Bundle content is an allowlist: manifest.json, README.txt, last N session files and crash files matching
  the exact name patterns, tail of mothdex.log. Entry names are fixed or the validated base name (no directories from
  disk, no ..). 10 MB enforced while writing (oldest sessions dropped first, then mothdex.log tail shortened), never
  after. Written as temp + rename, O_EXCL on the final name, 0600; -o must be an existing directory that is not a link.
  No network. README.txt says the issue is public and to review the zip first. Test: a 50 MB session yields a zip under
  10 MB; a symlink in sessions/ is not followed; no socket opened during `logs bundle`.
- TM-20 [Major] File creation safety. Dirs 0700, files 0600 (rotator already uses 0600). Session and crash files are
  created with O_CREATE|O_EXCL (retry with a suffix on collision); latest.log and the lock are opened without following
  links (O_NOFOLLOW on POSIX; refuse a reparse point on Windows). If paths.logs (or a subdir) is a link, not owned by the
  user, or writable by others without sticky bit, file logging is disabled with one warning (never fatal). Test:
  pre-created latest.log -> symlink to a victim file: victim unchanged; paths.logs=/tmp-like shared dir: no files
  written, command succeeds.
- TM-21 [Minor] latest.log takeover is safe and bounded. The last process to start owns latest.log; after takeover the
  previous process never writes another byte into the new latest.log (e.g. create a new file and rename it over; on
  Windows open with FILE_SHARE_READ|FILE_SHARE_WRITE|FILE_SHARE_DELETE so rename and tester copy work). Stale lock
  recovery uses OS lock semantics (flock / LockFileEx), never PID liveness. Lock wait is bounded (<= 1 s), then the
  process continues without latest.log. Test: CLI and GUI concurrently writing marker lines: each file contains only
  its owner's markers after takeover; a crashed owner's lock is recovered; a held lock does not delay a command > 1 s.
- TM-22 [Major] Deletion is pattern-bound. Retention and `logs clear` delete only regular files whose names match the
  exact session / crash / bundle patterns inside sessions/, crash/, bundles/; no recursion, no link following, never
  paths.logs itself; mothdex.log rotation unchanged. sessions_keep validated (1..100) and crashes kept = 10. Test:
  foo.txt, a dir, and x.log -> ~/important inside sessions/ survive `logs clear -f` and retention; paths.logs=~ deletes
  nothing outside the patterns.
- TM-23 [Minor] Size bounds. Per-session file cap (e.g. 20 MB, then one truncation line and stop), crash file cap
  (e.g. 1 MB), bundles/ keeps the last 5. Test: a long shell session at debug does not grow the session file past the cap.
- TM-24 [Minor] Input to logs subcommands. `logs show <session>` accepts only an existing session id matching the name
  pattern, resolved inside sessions/ (refuse /, \, ..); --ref matches ^[ABCDEFGHJKMNPQRSTVWXYZ23456789]{6}$ (the
  chosen alphabet); --tail and --sessions bounded integers. Test: `logs show ../../etc/passwd` -> usage error, nothing
  read.
- TM-25 [Minor] Log and terminal injection. Values stay quoted by slog (no raw newline in a record); crash files and
  the session header go through the same sanitiser; `logs show`, `logs last`, the GUI error screen and banners strip C0/C1
  control chars and ESC before display. Test: a fake API title containing a newline + level=ERROR ref=AAAAAA and an
  ESC]52 sequence: shows escaped, `logs show --ref AAAAAA` does not match it.
- TM-26 [Minor] ref from crypto/rand, no PID/time/user inside; it is a lookup key, never authorisation. Test:
  10^5 refs, alphabet respected, no collision required beyond what 30 bits give (document).
- TM-27 [Minor] GUI crash and fallback (Vitral). recover writes debug.Stack() and the panic value through the TM-16
  redactor into crash/ (TM-20 rules) and one line in latest.log, then exits with the internal code; the fallback log
  before config load goes to the OS default logs dir (never a path taken from the invalid config) and echoes rejected
  config values only through the existing displayValue redaction. "Export logs" calls the same bundle code (TM-18/19),
  needs a confirm, opens no network. Test: forced panic (build tag) with a URL ?token=S3CRET in the panic value: crash
  file has the stack, no S3CRET; invalid config with paths.logs=/x: fallback file in the default dir only.
- TM-28 [Minor] `logs open` and the GUI launch exec the file manager with argv (explorer.exe <dir>, open <dir>,
  xdg-open <dir>), never via sh -c / cmd /c start. Test: paths.logs containing & or ; opens nothing else.

## Surface 3: internal/update, mothdex upgrade, daily check (Alicerce contract T-111; Forja/Bigorna T-116, Vitral T-117)

- TM-29 [Major] Pinned origin. Every URL is built from the constant https://github.com/Or4cu1o/mothdex-cli/releases;
  no config key or env var changes it in release builds (tests use an unexported hook or build tag). HTTP client: TLS
  >= 1.2, default verification, at most 5 redirects, each redirect https and to an allowlisted host (github.com,
  objects.githubusercontent.com, release-assets.githubusercontent.com; verify the current list at implementation).
  Test: redirect to http or to evil host -> network/integrity error, nothing written.
- TM-30 [Major] Latest discovery. Read the Location of releases/latest without following it; accept only
  https://github.com/Or4cu1o/mothdex-cli/releases/tag/v<semver>; then fetch download/<tag>/checksums.txt by tag and
  require its archive names carry the same version. Plain `upgrade` and the daily check never select a version <= the
  running one; a lower version only with explicit --version, a downgrade warning and confirmation (-f skips the prompt,
  never verification). Beta channel discovery, if it uses the API: unauthenticated, never reads GITHUB_TOKEN/GH_TOKEN,
  same validation. Test: latest pointing to an older tag -> no update offered; Location with another repo -> error.
- TM-31 [Major] Verify everything before touching anything. SHA-256 against the tag's checksums.txt for every archive
  (CLI, GUI); cosign verify-blob with identity .../release.yml@refs/tags/<tag> and the GitHub OIDC issuer, or gh
  attestation verify --repo --signer-workflow --source-ref for every archive; external tools run by argv with a timeout,
  pass only on exit 0; the result (signed / SHA-256 only) is printed and logged honestly. Extraction reuses IC-4 (exact
  names, regular files, no links) with a decompressed-size cap; downloads reuse the T-27 size rules and a hard cap on
  checksums.txt (64 KB). Any failure aborts before the first installed file changes, for all components. Test: tampered
  checksums.txt, tampered GUI zip, extra member, link member, oversized body: installation byte-identical.
- TM-32 [Suggestion, owner/Alicerce decision] Verify the cosign bundle natively in Go (sigstore-go) so the upgrade path
  is always signature-checked instead of SHA-256-only when cosign/gh are absent. Check licence (Apache-2.0) and size.
  Without it: residual R6.
  Status 2026-11-26: done by D35 (owner), cli#152. `mothdex upgrade` verifies natively with an embedded Public Good
  trusted root, pinned by hash in a test. The checks are the exact workflow SAN for the tag, the exact issuer, the
  artifact digest of checksums.txt, a tlog entry, an SCT and an observer timestamp, with no SHA-256-only fallback
  (update.md §3 step 3). The reinstall hint is shown only for trust-material errors. Installers keep cosign or
  gh attestation when present.
- TM-33 [Major] Managed-install gate. Update only when the state file exists, origin=installer, its dir equals
  filepath.Dir(EvalSymlinks(os.Executable())) (alias invocation resolves to the same dir), the dir passes the
  installer's trust rules (owner, not writable by others, not a link), and the effective user owns it (refuse sudo on a
  user install). Otherwise update_unavailable with the one-line install command. Test: state copied to another dir,
  dir made group-writable, `sudo mdex upgrade`: refused, nothing written.
- TM-34 [Major] Swap and rollback. Stage new files in the install dir (O_EXCL, unique names), fsync, then: POSIX rename
  over; Windows rename current to <name>.old then move new in, for mothdex.exe, mdex.exe, mothdex-gui.exe, SDL2.dll.
  Run the new mothdex --version with a scrubbed env (IC-1 style) and a timeout; it must print exactly the expected
  version. Any failure restores every swapped file in reverse order and exits update_failed. Test: new binary exits 1 /
  prints the wrong version / hangs: old files restored and run; partial failure after the GUI swap restores the CLI too.
- TM-35 [Minor] .old cleanup touches only the exact names above in the install dir, regular files, not reparse points;
  a locked .old is left and retried next run; leftover staging files matching the exact staging pattern are removed.
  Test: user file foo.old and a junction named mothdex.exe.old are kept.
- TM-36 [Major] Daily check privacy and opt-out. Requests carry no version, id, cookie, token or query string; fixed
  User-Agent without version (spec: no version sent). update.check=false, MOTHDEX_NO_UPDATE_CHECK=1, CI set (any non-empty
  value) or unmanaged install -> zero connections. Test: fake server records headers; opt-out cases record no
  connection.
- TM-37 [Minor] Check never blocks. Background, 3 s timeout, the command never waits for it at exit; the notice only on
  an interactive stderr, never with --json, -q or non-TTY. Test: a server that never answers: command duration unchanged
  (within noise), no notice under --json.
- TM-38 [Minor] Cache is a hint, never an instruction. Written atomically 0600 without following links; parsed
  strictly; version validated against the semver regex before printing; `upgrade` always re-discovers and verifies, never
  installs from cache data; corrupt cache = no notice, rewritten next check. Test: cache with 9.9.9 and an ESC sequence:
  no escape printed, `upgrade --check` asks the server.
- TM-39 [Minor] Untrusted display text. CHANGELOG summary shown before confirmation comes from the verified archive (or
  is labelled unverified), control chars stripped, length capped; same for shell banner, TUI footer and GUI About.
- TM-40 [Minor] GUI update (T-117) uses the same internal/update with explicit gamepad confirmation, never automatic;
  restart execs the new mothdex-gui by absolute path with argv only.

## Residual risks (for the ADR)

- R1 Script authenticity is not checked before it runs. Whoever can change what releases/latest/download/install.* serves
  (owner account, release workflow before publish, GitHub) gets code execution as the user on every new install; the
  in-script checks then prove nothing. Reduced by: immutable releases, v* tag ruleset, release environment reviewers,
  installers inside signed checksums.txt + attestations (after-the-fact audit), owner 2FA. Accepted (D34).
- R2 TLS interception by a CA the machine trusts (corporate proxy, malware) can swap the script; the pipe adds no check.
- R3 A short domain later adds its DNS, registrar and host to the trusted base (TM-6).
- R4 Copy-paste lures (lookalike URLs, edited commands in issues/chats). One canonical URL in docs only reduces it.
- R5 Running the line in an elevated PowerShell runs the installer as admin; it warns and stays per-user.
- R6 Without cosign or a logged-in gh (most users), installs are SHA-256 only: transit integrity, not origin. Closed
  for `mothdex upgrade` by TM-32/D35, which always verifies the signature natively.
- R7 The alias conflict check sees only what the installer process sees: a non-interactive sh / -NoProfile PowerShell
  misses functions and aliases from the user rc/profile; the PATH it sees may differ from the interactive one.
- R8 A malicious release produced by the real workflow passes every check: signatures prove origin, not intent. Upgrade
  spreads it to every managed install within a day of the check (only to users who run upgrade; never automatic).
- R9 Redaction is pattern-based: new secret formats or user content (search terms, titles, ROM folder names) can remain
  in a bundle posted to a public issue; the tester reviews it first.
- R10 The daily check reveals IP and timing of mothdex use to GitHub; opt-outs exist.
- R11 Same-user malware can rewrite the install dir, state file, rc files and PATH regardless of these controls (out of
  scope: same trust level).
- R12 (added with D35) Embedded-root staleness. Upgrade trusts only the Sigstore root embedded in the running binary.
  A rotation of the Sigstore root, Fulcio CA or Rekor shard that this root lacks makes that binary fail closed, and the
  user must reinstall. A revocation reaches users only with a Mothdex release. Refreshed by
  scripts/update-trusted-root.go; the reinstall hint is limited to trust-material errors (TM-32 status).

Verdict: design-stage model, no code to audit. Controls marked Major block the corresponding PR at audit if missing.
