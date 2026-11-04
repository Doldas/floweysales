# Provenance coverage

Which surfaces describe the origin of what they show, using the contract in
[ADR 0003](adr/0003-provenance-contract.md). Issue #204 tracks this table.

| Surface | State | Notes |
| --- | --- | --- |
| World headlines (World page, company impact panel) | Covered | `provenance` on each headline, shown with `fs-source`. Web search results claim no publication time. |
| Flow news rows | Planned | Rows should carry the same fact. |
| Leaderboard | Planned | Methodology and input hash exist (`leaderboard_method.go`); not yet in the shared format. |
| Quotes and feed status | Planned | `QuoteSnapshot` already has source, as-of and fetched time. |
| Saved backtest runs | Partial | `provenance` on a saved run and in its JSON export: method id and version, production time, provider and last price date where recorded; `partial` for batch and settings checks, which do not record a provider. Shown with `fs-source`. Live (unsaved) backtests and the Strategies page are not yet covered. |
| Strategy results | Planned | Calculated; needs method ids. |
| Agent answers (chat) | Partial | See [agent-evidence.md](agent-evidence.md). Source registry and citation check for `search_web` and `get_sec_section` (`internal/sources`). Teams, flows and other tools are not yet registered. |
| Financial statements | Planned | |

States: **Covered** (API and UI), **Planned** (not started).

## Leaderboard regions (#209)

The Leaderboard lists all six world regions with the universes offered in each. Asia, Africa, South America and Oceania
show "No supported market yet": no universe there has been checked against the provider (prices and four quarters of
reports for at least 80% of members, Plan 4 WP3 step 1). A region is added only after that check.
