# Issue #258: deterministic hardening

Status: implemented in PR branch; pending review and CI. Parent issue remains open.

Delivered: Validate actual calendar dates for index membership snapshots.

Validation: `node --test frontend/src/app/components/index-sources-view.spec.ts` plus CI. No live providers are contacted by these regressions.

Outstanding: The full issue's upstream provider/license tests and feature acceptance remain separate. Do not close the parent issue based only on this fix.

## Currency mix in combined universes

`combineIndexes` now reports the quote currencies of the combined indices and adds a warning when they differ (for example S&P 500 in USD with OMXS30 in SEK). Nothing is converted. Covered by `TestCombinedUniverseDisclosesCurrencyMix`. Licensed constituent coverage and historical membership remain open.

## Open index-universe architecture (2026-11)

- **Data stays with the user.** No constituent list, ISIN or licensed price is in the repository. Tests use `internal/indexsource/fixture`, which invents companies ("Synthetic Co NDA001"), tickers and ISINs with valid check digits.
- **Adapters.** `indexsource.Fetcher` plus a registry (`Register`, `Adapters`) describe how a list is obtained; CSV and FMP are registered, and every adapter carries the licence notice (a key or a file proves access, not display or redistribution rights). A new provider is one type and one `Register` call; screening and ranking are unchanged.
- **Verification.** `indexsource.Assess` returns `verified` only when the licence is confirmed, the size matches the name within a tolerance, the list is at most 100 days old, the source format is documented and any weights are plausible. It is not proof that a list is official.
- **History.** CSV lists can carry a `date,added,removed` file; `MembersAt` and `EverMembers` then rebuild past membership, and contradictions with today's list are warned about.
- **Comparison and combination.** `compareIndexes` and `combinedUniverse` match by canonical identity (ISIN, else exchange and ticker), never convert currencies and refuse to substitute a missing list.
- **Production activation still needs, per index:** a data source you are permitted to use (your own provider agreement or file), your confirmation of its terms, and a check of a few members and the count against the provider's own list. Real constituent coverage is not claimed until that is done; the FMP Nasdaq-100 response shape has still not been verified against a live response.
