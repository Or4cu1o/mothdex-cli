# Managed release updates

`update.New` builds the shared updater. `compose.NewUpdater` supplies the resolved process executable, build version,
configured channel, data directory and logger to CLI and GUI sessions. `Check` discovers public release metadata;
`Apply` stages and verifies every installed component before asking `Confirm`, then swaps or restores the whole set.

`Progress` and `Confirm` run serially on the Apply goroutine. A confirmation dialog must watch the caller's context.
Cancellation is checked before the swap; after the first swap, commit or rollback completes independently of cancellation.
No UI should apply an update without explicit confirmation. `RestartRequired` tells persistent interfaces to restart.

Tests use TLS fixture servers, generated binary probes and temporary installation records, never production services.
T-110 TM-29 through TM-40 and `docs/contracts/cli/update.md` define the security boundary.
