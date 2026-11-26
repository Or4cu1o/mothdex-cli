# Contract: self-update and version check (`internal/update`)

Status: draft v1 (T-111) · Owner: Alicerce · Implementers: T-116 (package, `mothdex upgrade`, CLI/shell/TUI check), Vitral
(T-117, GUI) · Risk: High (supply chain: the program replaces itself) · Spec: `beta-adjustments-spec.md` §5 · Threat model:
[`T-110`](../../security/T-110-threat-model-beta-adjustments.md) surface 3, TM-29 to TM-40 (§8). The integrity rules are
those of release-artifacts.md §4 and must not get weaker here.

## 1. Interface (one package, used by the CLI and the GUI)

```go
type Options struct {
    Client   *http.Client                 // built by the package, §2; tests inject a TLS test server
    Exe      string                       // os.Executable() with symlinks resolved
    Current  string                       // buildinfo version; "" (dev build) => unmanaged
    Channel  string                       // "stable" | "beta"
    DataDir  string                       // paths.data, for the check cache (§5)
    Log      *slog.Logger
    Now      func() time.Time
    Verifier Verifier                     // nil => DefaultVerifier(): native sigstore-go (D35); §3
    LookPath func(string) (string, error) // unused since D35, kept for source compatibility
}

// Verifier proves the origin of a release's checksums.txt and archives (step 3). SHA-256 against checksums.txt
// (step 2/4) is never part of it and never optional. DefaultVerifier is the native sigstore-go verifier (D35): the
// embedded Public Good trusted root, no network or TUF request at runtime, the policy of §3 step 3. It returns
// "native". No Verifier may return "none".
type Verifier interface {
    Verify(ctx context.Context, r VerifyInput) (Signature string, err error) // "cosign" | "attestation" | "native" | "none"
}
type VerifyInput struct {
    Tag                 string            // v<version>
    Checksums, Bundle   []byte            // checksums.txt and checksums.txt.sigstore.json; nil or empty Bundle (404) => integrity
    Archives            map[string]string // asset name -> verified local path; unused by the native verifier
}
func New(o Options) (*Updater, error)
func (u *Updater) Managed() (State, error)                       // install-state.md §4; *app.Error update_unavailable
func (u *Updater) Latest(ctx context.Context) (Release, error)   // Release{Version, Tag, Prerelease, PageURL}
func (u *Updater) Check(ctx context.Context, force bool) (CheckResult, error) // cache-aware, §5
func (u *Updater) Apply(ctx context.Context, o ApplyOptions) (Result, error)  // §3 and §4
func CleanupOld(exeDir string, log *slog.Logger)                 // §4 step 8; early in main, only for runs that log (§4)
```

`ApplyOptions{Version string; AllowDowngrade bool; Progress func(Progress); Confirm func(Staged) bool}`. `Confirm` is
called after §3 has passed, with `Staged{From, To, Downgrade bool; Signature, Notes string}`. If it returns false, the
staging is deleted and the result is `canceled`. `-f` passes a `Confirm` that always returns true.
`Result{From, To, Signature string; GUI, AliasRelinked, RestartRequired bool; Notes string}`, where `Signature` is
the value the `Verifier` returned. A single `Updater` runs one `Apply` at a time. Every error is an `*app.Error` with a code
from §6.

**Addendum (T-116, agreed with Forja):**

- **Progress.** `Progress{Step string; Done, Total int64}`. `Step` is one of `discover`, `download`, `verify`,
  `stage`, `swap`, `check` or `done`. `Total` is 0 when the size is unknown.
  - Both `Progress` and `Confirm` are called on `Apply`'s own goroutine, one at a time. A front end that draws on
    another thread must copy the value it receives and must not block for long.
- **CheckResult.** `CheckResult{Current, Latest, Channel, PageURL string; Available, Notify, FromCache bool;
  CheckedAt time.Time; Skipped string}`.
  - `Latest` has passed the SemVer check (TM-38), or is `""` when none is known.
  - `Available` means `Latest > Current`.
  - `Notify` applies the notice-once rule of §5. The caller that shows the notice calls `MarkNotified(latest)`,
    which rewrites `notified_*`.
  - `Skipped` carries the off-switch reason, and then no request was made.
- **Cancellation.** `Apply` checks `ctx` before every step and again right after `Confirm` returns.
  - Up to step 4 (§3), a cancelled `ctx` deletes the staging folder and returns `canceled`.
  - A `Confirm` that waits on a dialog must also watch `ctx.Done()` and return false when it fires.
    - **CLI:** reads the answer on a goroutine and selects on `ctx.Done()`. The process ends right after, so the
      reader left behind does no harm.
    - **Shell:** keeps its prompt synchronous, so an abandoned read never steals the next line. Ctrl+C at the prompt
      answers no, giving `canceled`, and only the cancellation of the whole session ends the Shell. `Apply` checks
      `ctx` again as soon as `Confirm` returns.
    - **TUI and GUI:** their dialogs are part of the model and already end on `ctx.Done()`.
  - From the first rename of §4 on, `ctx` is no longer read. The swap, check and commit, or the rollback, always run
    to the end, so a cancel never leaves a half-swapped installation.
- **Off switches** (§5) are decided only in the `update` package. A single function takes `Options.Getenv` and the
  `update.check` value and returns the reason (`config`, `env`, `ci`, `unmanaged` or `dev_build`), or `""` when the
  check is on. Front ends never test these conditions themselves.
- **Wiring in compose.** `compose.NewUpdater(cfg config.Update, dataDir string, log *slog.Logger) (*update.Updater, error)`.
  `compose.Session` gets `Updater *update.Updater` and `UpdateCheck bool`, which is the off-switch result for this
  run.
- **`target`, and `linux_arm` in particular.** `GOOS_GOARCH`, except that `GOARCH=arm` becomes `armv6` or `armv7`
  according to the `GOARM` build setting in `runtime/debug.ReadBuildInfo`. Without that setting (a dev build) the
  result is `update_unavailable` with `reason: dev_build`. The `armv8l` kernel installs `linux_armv7`
  (release-artifacts.md §3), and the state file's `target` must equal this value.

## 2. Network and latest discovery (no GitHub API)

- **Origin (TM-29).** Every URL is built from the constant `https://github.com/Or4cu1o/mothdex-cli/releases`. No config
  key or environment variable changes it in release builds; tests use an unexported hook.
- **Transport.** TLS 1.2 or later, with the default verification. Redirects are followed only over `https`, at most 5,
  and only to `github.com`, `objects.githubusercontent.com` or `release-assets.githubusercontent.com` (re-check this
  list when implementing). Anything else is `untrusted_redirect`, and nothing is written.
- **Request shape (TM-36).** Every request is a GET with `User-Agent: mothdex`, no version and no identifier. Cookies
  are off, and no query string or request body is ever sent. `GITHUB_TOKEN` and `GH_TOKEN` are never read. The standard proxy environment is honoured. Each body has a size
  limit: `checksums.txt` 64 KiB, the Atom feed 1 MiB, the sigstore bundle 256 KiB, an archive 128 MiB.
- **`stable`:** `GET /releases/latest` with redirects off. The answer must be a 302 whose `Location` is exactly
  `https://github.com/Or4cu1o/mothdex-cli/releases/tag/v<semver>`, with no prerelease part. That version's
  `checksums.txt` must then list `mothdex_<version>_<target>`. This is the same source the installers use.
- **`beta`:** `GET /releases.atom`. Every entry link matching `/releases/tag/v<semver>` is a candidate, a `-` in the
  version marks a prerelease, and the highest SemVer among the candidates and the stable latest wins.
- **Both channels: cross-check (TM-30).** The archive names in the tag's `checksums.txt` must carry the same version
  as the tag.
- **Notes (TM-39).** The CHANGELOG summary shown before confirmation comes from the `CHANGELOG.md` inside the
  **verified** CLI archive: the sections for versions after `Current`, passed through `app.SanitizeText`, then cut to
  20 lines and 2 KiB. That is why the interactive `upgrade` runs §3 (stage and verify) **before** it asks.
  - Answering no deletes the staging folder.
  - `upgrade --check`, the CLI notice, the shell banner, the TUI footer and the GUI notice show only the version and
    the release page URL, never notes from the network.
- **No downgrades unless asked.** `Check` and a plain `Apply` only offer a version greater than `Current` by SemVer
  precedence. A lower version needs `upgrade --version X.Y.Z`, which sets `AllowDowngrade` and shows "downgrade" in
  the confirmation. `-f` skips the prompt, never a verification.

## 3. Verification order (nothing installed is touched until step 5)

1. **Managed check** (install-state.md §4, TM-33), then the exclusive lock on `<dir>/.mothdex-update.lock`. If the lock is
   held, the result is `update_failed` with `reason: in_progress`.
2. **Checksums.** Download `checksums.txt` of `v<version>`. A 404 is `not_found`. The file must have exactly one valid
   line for `mothdex_<version>_<target>.<ext>`, and also for `mothdex-gui_<version>_<target>.<ext>` when the state
   says `gui: true`. A GUI archive missing from a release is `update_failed` with `reason: gui_missing`, and the hint
   is the installer command.
3. **Signature (TM-31),** through `Verifier`. It is called once, after the SHA-256 of every archive (first bullet of
   step 4) and before extraction. The default is the **native sigstore-go verifier (D35, TM-32)**. Its policy:
   - **Bundle and identity.** The bundle must verify the bytes of `checksums.txt` (artifact digest). The certificate SAN
     must be exactly `https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/v<version>`, and
     the issuer exactly `https://token.actions.githubusercontent.com`.
   - **Evidence.** It requires at least one verified transparency-log entry, at least one SCT
     (`WithSignedCertificateTimestamps(1)`, as `cosign verify-blob` checks by default), and at least one
     authenticated observer timestamp. The certificate is validated at that time, not at the wall clock.
   - **Trusted root.** The trusted root is `internal/update/trusted-root.json`, embedded. A test pins its SHA-256, and
     `scripts/update-trusted-root.go` refreshes it through Sigstore TUF, writing pretty JSON and printing the old and
     new hashes. Nothing is fetched at runtime.
   - **Failure.** Any verification failure, including a missing bundle, is `integrity` with `reason: signature`.
     There is no SHA-256-only path.
   - **Hints.** The reinstall hint is shown only for trust-material errors: an unknown CA, an unknown log key, or a
     time outside the root's validity. Identity or digest mismatches and a missing bundle show a neutral "possible
     tampering, do not reinstall, report it" hint. Otherwise a tampered release would steer users to the SHA-256-only
     installers (R6).
4. **Archives.**
   - Download into `<dir>/.mothdex-update-<random>/` (mode `0700`) and check each SHA-256 against its line. A mismatch
     is `integrity`; a body longer than its limit is `size_mismatch`.
   - Downloads follow the T-27 size rules: a `Content-Length` that disagrees with the bytes received is
     `size_mismatch`.
   - Staged files are created with `O_EXCL` under unique names and `fsync`ed.
   - Extract only the exact root members of release-artifacts.md §1 that the install needs (IC-4). Regular files only:
     a link, a path, an extra member or a duplicate name is `integrity`. A member that decompresses past 128 MiB is
     `size_mismatch`. Forward compatibility (Crivo M3, agreed with Regente):
     - **Executables and DLLs keep exact names**, and an extra member at the archive root is still `integrity`.
     - **Extra regular files under `THIRD_PARTY/` are accepted**, at most 64 of them, each at most 1 MiB, within the
       128 MiB total. The name must match `^THIRD_PARTY/[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`: a base name only, with no
       path and no leading dot.
     - **Accepted extras are never executed**, and are written to `mothdex-licenses` only when they are on the known
       notice list.
     - **Duplicate names are `integrity`,** including names that differ only in case: they would collide on Windows
       and on case-insensitive volumes. This holds for every member, not only the extras.
     - **Extras are never used as notes** or for the CHANGELOG summary, and their content is never logged: only the
       count, or a sanitized name (Sentinela).
     - **Directory entries:** an empty directory entry named exactly `THIRD_PARTY/` is ignored; any other is
       `integrity`.
   - Run the staged `mothdex --version --json` in the staging folder with a scrubbed environment (IC-1 style) and a
     10 s limit; `data.version` must equal the target version exactly.
   - With the GUI: the staged `mothdex-gui --version` must succeed, and `mothdex-gui --probe` must exit 0. Exit 8 is
     `update_failed` with `reason: gui_not_eligible`. On Windows the staged `SDL2.dll` sits next to the staged GUI, so
     the probe loads it.

   Any failure up to here deletes the staging folder and leaves the installation exactly as it was.

## 4. Swap, check, rollback (`update_failed` on any failure from here on)

The swap set (TM-34) is `mothdex[.exe]`, plus the Windows hardlink alias `<alias>.exe`, plus `mothdex-gui[.exe]` and on
Windows `SDL2.dll` when `gui` is true. The licence notices are replaced after the binaries, and a failure there is
only a warning.
- **Where they go.** Into `<dir>/mothdex-licenses/`, under the flat names the installers use (their `NOTICE_NAMES`
  list, T-70 IC-5), and only from the GUI archive's `THIRD_PARTY/`. A CLI-only install has no notices folder.
- **Nothing else.** `LICENSE` and the files of `THIRD_PARTY/` are never written at the root of `dir`, which may be a
  shared `~/.local/bin`.
- **How, the installers' rule (agreed with Guindaste).**
  1. Build the complete set in a private folder, `<dir>/.mothdex-licenses.new.<id>`.
  2. Check the existing `mothdex-licenses` before removing anything. If it is a link or a reparse point, or holds any
     file outside the known names, leave the whole folder untouched, with a warning.
  3. Otherwise replace it with a rename.

5. **Swap, Linux and macOS:** for each file, first `link(f, f.old)`, or a copy that keeps the mode when the file system
   has no hardlinks. Then `rename(staged, f)`, which is atomic in the same folder.
6. **Swap, Windows:** for each file, `rename(f, f.old)` (the running `.exe` and a loaded DLL can be renamed, not
   overwritten), then `rename(staged, f)`. A stale `f.old` that cannot be deleted is moved aside to
   `f.old-<random>` first.
   - **Hardlink alias:** relinked only when it is ours, meaning its file ID equals the current `mothdex.exe` or the
     state's `alias_file_id` (install-state.md §4). The steps are `rename(<alias>.exe, <alias>.exe.old)`, then
     `CreateHardLink(<alias>.exe, mothdex.exe)`. The user may be running `mdex upgrade` itself.
   - The commit (step 8) writes the new link's file ID to `alias_file_id`. A rollback relinks the alias to the
     restored file and writes that file's ID.
   - **Unix symlink alias:** needs nothing.
7. **Post-swap check:** the installed `<dir>/mothdex --version --json`, run with the same scrubbed environment and
   10 s limit, must report exactly the target version. On Windows, `<alias>.exe` must have the same file ID as
   `mothdex.exe` (TM-13). and the GUI
   `--version` must succeed when the GUI was swapped.
   - **If the check fails, roll back in reverse order.** On Unix, `rename(f.old, f)`. On Windows, `rename(f, f.failed)`
     then `rename(f.old, f)`, and the alias is relinked. Then return `update_failed` with `details.restored: true`.
   - **If a rollback step fails,** return `details.restored: false`, with a message that gives the one-line install
     command.
8. **Commit:**
   - rewrite the state file (install-state.md §3) and delete the staging folder;
   - delete every `f.old` on Unix, and on Windows those not in use;
   - set `RestartRequired: true`. The shell, TUI and GUI tell the user to exit and open again.
   - **`CleanupOld`** runs at the next start of either binary, only for a run that gets a session log (logs.md
     §1). It never runs for `mothdex-gui --probe`, `--version`, `--spike` or `--help` (ports.md §5.2: the probe writes
     nothing), nor for `mothdex version`, `--version`, `completion` or `__complete*`. So neither the staged binary's
     `--version` of §3 step 4 nor the post-swap check of step 7 cleans up anything.
   - **Locking.** It takes `<dir>/.mothdex-update.lock`, creating the file if needed, only after the managed check
     (install-state.md §4) has passed. An unmanaged folder or a staging folder therefore never gets a file.
     - If the lock is held (an `Apply` is running), cleanup is skipped for this run.
     - A staging folder's own lock is tested by opening the existing file without `O_CREATE`; a missing lock means
       the folder is free.
   - **Where it is called.** In the CLI, from a hook after flag parsing and before the service opens. In the GUI,
     only for a run that opens the GUI.
   - It deletes, in the executable's folder only,
     `mothdex.exe`, `mothdex-gui.exe`, `SDL2.dll` and `<alias>.exe` followed by `.old`, `.old-*` or `.failed`. It also
     deletes `.mothdex-update-*` folders older than 1 h whose lock is free. It also deletes regular files matching
     `^\.mothdex-(write-probe|json)-[0-9]+$` that are older than 1 h, while holding the update lock: the leftovers of
     an `Apply` that crashed.
   - **TM-35:** only regular files, never reparse points or links, and only exact names: never a broader glob. A
     locked file stays and is retried at the next start. Failures go to the log.

## 5. Automatic check, cache and the 24 h rule

- **Cache:** `<paths.data>/update-check.json`, written atomically:
  `{schema_version:1, checked_at, channel, current, latest, notified_version, notified_at}`. It does not live in
  `paths.temp`, because `cache clear` must not reset it.
- **The cache is a hint, never an instruction (TM-38).**
  - It is written with mode `0600`, through a temporary file and a rename, without following links.
  - It is parsed strictly, and `latest` must pass the SemVer regex before anything prints it.
  - A corrupt cache means no notice, and it is rewritten at the next check.
  - `upgrade` always discovers and verifies again, and never installs from the cache.
- **When a check runs:** when there is no cache, or `checked_at` is older than 24 h or in the future, or `channel` or
  `current` changed. Otherwise the cache answers.
- **CLI:** a goroutine starts with the command and has a 3 s timeout. At the end of the command, a notice is shown on
  stderr when the result (fresh or cached) has `latest > current`, and `notified_version != latest` or `notified_at`
  is older than 24 h. The notice reads `Nova versão disponível: 0.1.1 → 0.2.0. Atualize com: mdex upgrade` (i18n key
  `update.notice`, with `mdex` replaced by the invoked name). A check that has not finished is abandoned: the process
  never waits for it.
- **Where the check is skipped:** when stderr is not a terminal, with `--json` or `-q`, and for `upgrade`, `version`,
  `completion`, `__complete*` and `logs`. In all these cases no request is made either.
- **Shell banner, TUI footer:** both use the cached result. **GUI:** checks after the first frame, with the same cache
  and rules, and shows the notice and the Settings › About item.
  - "Update" needs an explicit confirmation on the gamepad and never starts on its own. It runs the same `Apply`,
    and the confirmation comes after staging, so it can show the verified notes.
  - The restart runs the new `mothdex-gui` by its absolute path, with argv only (TM-40).
- **Off switches** (no connection at all, TM-36): `update.check: false`; `MOTHDEX_NO_UPDATE_CHECK` set to anything but `""`,
  `0` or `false`; `CI` not empty; an unmanaged installation or a dev build. `upgrade --check` is an explicit request
  and ignores the first three, but it never applies anything.

| Key | Values | Default | Env |
|---|---|---|---|
| `update.check` | bool | `true` | `MOTHDEX_UPDATE_CHECK` |
| `update.channel` | `stable` or `beta`; `upgrade --channel` overrides it for one run | `stable` | `MOTHDEX_UPDATE_CHANNEL` |

`MOTHDEX_NO_UPDATE_CHECK` joins `MOTHDEX_CONFIG` as a reserved variable (config.md §4), so it is not reported as unknown.

## 6. Errors, output and logs

- **`update_unavailable`** (exit 9): the installation is not managed. `details.reason` is one of
  install-state.md §4, plus `dev_build`. The hint gives the one-line install command for the OS.
- **`update_failed`** (exit 9): `details.reason` is one of `in_progress`, `gui_missing`, `gui_not_eligible`,
  `swap_failed` or `check_failed`, and `details.restored` is a bool.
- **Reused codes:** `integrity`, `size_mismatch`, `network`, `untrusted_redirect`, `not_found`, and
  `confirmation_required` (`--json` without `-f`).
- **Time limits (Crivo M1).**
  - Network: `ResponseHeaderTimeout` 15 s, and a 30 s idle limit on each body.
  - An interactive `Check` or `upgrade --check` gets 30 s; the background check keeps 3 s.
  - `Apply` gives discover, download, verify and stage 10 min together. The `Confirm` wait has no limit, because a
    person is reading. The swap, check and commit run under their own 10 s probes and ignore `ctx` (§1 addendum).
- **`details.restored`** is `true` whenever the previous version is in place: a failure before the first rename, or
  a complete rollback. It is `false` only when a rollback step failed, and only then is the hint the install command.
- **`upgrade --json` data:** `{current, latest, available, updated, channel, signature, restart_required, notes,
  downgrade}`.
  - A downgrade, even with `-f`, prints a warning on stderr before the swap (human output). Under `--json` the same
    text goes into `warnings[]` instead (cli-output.md §1).
  - `-f` skips only the prompt, never the warning.
- **Log lines:** `msg="update check"` with `current latest channel result duration_ms` (result is `available`,
  `current`, `failed` or `skipped:<reason>`), and `msg="update apply"` with `from to signature result reason restored`.

## 7. Tests (High: risk paths)

These run against a TLS test server and a temporary install folder:

- a tampered `checksums.txt`, or a tampered archive, changes nothing;
- a bundle with the wrong tag, repo, ref or issuer, a tampered `checksums.txt`, no tlog entry, no SCT, or a missing bundle aborts and leaves the install byte-identical; one fixture is a real public release bundle verified against the embedded root;
- a redirect to another host is refused;
- a new binary whose `--version` fails is rolled back;
- the Windows rename path runs while the old `.exe` is still open, and `CleanupOld` then deletes the `.old`;
- with every off switch, the recording client sees no request;
- the 24 h and notice-once rules are driven by a fake `Now`;
- with the beta channel, the Atom feed picks the highest SemVer, and an older version is never offered without
  `--version`.

## 8. Security controls (T-110) and residual risks

TM-29 to TM-40 are folded into the sections above. Each one is a Major or Minor audit item for T-116 and T-117 (see
the threat model for its test).

| Control | Section |
|---|---|
| TM-29 | §2 Origin |
| TM-30 | §2 latest and cross-check |
| TM-31 | §3 |
| TM-32 | §1 and §3, done (D35) |
| TM-33 | install-state.md §4 |
| TM-34 | §4 |
| TM-35 | §4 step 8 |
| TM-36 | §2 and §5 |
| TM-37 | §5 |
| TM-38 | §5 |
| TM-39 | §2 Notes |
| TM-40 | §5 GUI |

Residual risks:

- **R6.** Closed for `upgrade` by D35. Installers without cosign or a logged-in gh remain SHA-256 only.
- **Embedded root staleness (new, D35).** A Sigstore root, CA or Rekor-shard rotation that the embedded root lacks
  makes older binaries fail closed, and they need a reinstall. Revocations arrive only with a Mothdex release.
- **R8.** A malicious release built by the real workflow passes every check. It reaches only the users who run
  `upgrade`, because nothing is ever installed automatically.
- **R10.** The check reveals IP address and timing to GitHub. The off switches exist for that.
