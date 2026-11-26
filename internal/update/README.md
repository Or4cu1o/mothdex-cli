# Managed release updates

`update.New` builds the shared updater. `compose.NewUpdater` supplies the resolved process executable, build version,
configured channel, data directory and logger to CLI and GUI sessions. `Check` discovers public release metadata;
`Apply` stages and verifies every installed component before asking `Confirm`, then swaps or restores the whole set.

`Progress` and `Confirm` run serially on the Apply goroutine. A confirmation dialog must watch the caller's context.
Cancellation is checked before the swap; after the first swap, commit or rollback completes independently of cancellation.
No UI should apply an update without explicit confirmation. `RestartRequired` tells persistent interfaces to restart.

Tests use TLS fixture servers, generated binary probes and temporary installation records, never production services.
T-110 TM-29 through TM-40 and `docs/contracts/cli/update.md` define the security boundary.

The default verifier uses sigstore-go with the embedded Public Good root. It validates the artifact digest, Fulcio
chain, exact release-workflow/tag identity, GitHub Actions OIDC issuer, certificate transparency proof (SCT) and
authenticated transparency evidence/time. The root is pinned by SHA-256 (see docs/release.md), and `trust.go` limits
the reinstall hint to failures the embedded root may explain.
It performs no network requests and never falls back to tools or checksum-only verification. Refresh the embedded
root through verified TUF metadata with `go run scripts/update-trusted-root.go`; review root changes with each release.

Managed checks are read-only. Apply probes writability after locking, and gives preparation a ten-minute budget;
confirmation has no preparation deadline. Explicit checks take at most thirty seconds; background checks take three.
Headers have a fifteen-second limit and each response body a thirty-second idle limit. Future THIRD_PARTY notices
are discarded within the bounded archive policy. Only known GUI notices refresh mothdex-licenses via a private folder
and rename; unknown entries preserve the existing folder. The statfs exception for permissionless filesystems is
shared with the logging and installer policy while effective-user ownership remains mandatory on Unix.

Every error carries a stable English Message and `message_key: update.errors.<family>.message` for CLI/GUI translation,
plus `hint_key`. Technical reasons remain in Args. Errors before replacement default to `restored: true` and the unchanged
hint. Signature verification selects its tampering/trusted-root hint; unmanaged installations, a missing GUI archive
and incomplete rollback retain explicit installer hints. Rollback success selects the neutral restored hint.
