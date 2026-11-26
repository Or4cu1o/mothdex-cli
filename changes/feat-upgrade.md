### Added

- T-116: shared managed-install updater with pinned release discovery, archive checksums, replaceable origin verification,
  staging, version probes and rollback; shared constructor for CLI and GUI.
- T-116: `upgrade` (alias `update`) supports explicit version and channel selection, check-only and JSON output;
  background version checks have a private daily cache and opt-outs, with terminal-only CLI, shell and TUI notices.
  Failed upgrades report exit 9 with a reinstall hint; all interfaces share progress, confirmation and restart semantics.
- T-116: reject malformed state and trailing archive payloads; keep installer probes read-only. Windows updates
  accept the installer's trusted owners and record alias file IDs after both commit and rollback.
