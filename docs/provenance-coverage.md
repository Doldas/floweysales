# Provenance coverage

Which surfaces describe the origin of what they show, using the contract in
[ADR 0003](adr/0003-provenance-contract.md). Issue #204 tracks this table.

| Surface | State | Notes |
| --- | --- | --- |
| World headlines (World page, company impact panel) | Covered | `provenance` on each headline, shown with `fs-source`. Web search results claim no publication time. |
| Flow news rows | Planned | Rows should carry the same fact. |
| Leaderboard | Partial | `provenance.fact` (the ranking, calculated, with method id and version), `provenance.universe_fact` (the list) and `metrics_source.fact` on each row (provider, price date, the provider's own page where the address follows from the provider and symbol), shown with `fs-source`. Data-age and bias statements are stated, not left implicit. Not covered: the Leaderboard flow node, the agent tool's rows and the per-metric fundamentals period. |
| Quotes and feed status | Partial | Live ticks carry `poll_time` and, when known, `quote_time`, `delay_seconds`, `market_state` (with `market_basis`); `QuoteSnapshot` adds `market_session`. The feed status popover shows them. Session is computed from regular exchange hours for known tickers (no holidays) unless the provider states it. Not yet in the shared `provenance` format. |
| Saved backtest runs | Partial | `provenance` on a saved run and in its JSON export: method id and version, production time, provider and last price date where recorded; `partial` for batch and settings checks, which do not record a provider. Shown with `fs-source`. Live (unsaved) backtests and the Strategies page are not yet covered. |
| Strategy results | Planned | Calculated; needs method ids. |
| Agent answers (chat) | Partial | See [agent-evidence.md](agent-evidence.md). Source registry and citation check for `search_web` and `get_sec_section` (`internal/sources`). Sources a delegated team's members retrieved are registered when `delegate_to_team` returns (see [agent-personas.md](agent-personas.md#delegating-to-a-saved-team)). Standalone team runs, flows and other tools are not yet registered. |
| Financial statements | Planned | |

States: **Covered** (API and UI), **Planned** (not started).

## Leaderboard regions (#209)

The Leaderboard lists all six world regions with the universes offered in each. Asia, Africa, South America and Oceania
show "No supported market yet": no universe there has been checked against the provider (prices and four quarters of
reports for at least 80% of members, Plan 4 WP3 step 1). A region is added only after that check.

The check is `go run ./cmd/universe-coverage`, which can be run offline (it validates the candidate lists) and, by someone
with provider access, measures each candidate market against the bar. See [universe-coverage.md](universe-coverage.md). Until
a maintainer has run it with access and written a reviewed list into `universes.json`, the regions stay unsupported.
