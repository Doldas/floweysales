# Issue #258: deterministic hardening

Status: implemented in PR branch; pending review and CI. Parent issue remains open.

Delivered: Validate actual calendar dates for index membership snapshots.

Validation: `node --test frontend/src/app/components/index-sources-view.spec.ts` plus CI. No live providers are contacted by these regressions.

Outstanding: The full issue's upstream provider/license tests and feature acceptance remain separate. Do not close the parent issue based only on this fix.

## Currency mix in combined universes

`combineIndexes` now reports the quote currencies of the combined indices and adds a warning when they differ (for example S&P 500 in USD with OMXS30 in SEK). Nothing is converted. Covered by `TestCombinedUniverseDisclosesCurrencyMix`. Licensed constituent coverage and historical membership remain open.
