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
| Agent answers (chat) | Partial | See [agent-evidence.md](agent-evidence.md). Source registry and citation check for `search_web`, `read_source` and `get_sec_section` (`internal/sources`), in chat, agent teams and flow agent nodes. Sources a delegated team's members retrieved are registered when `delegate_to_team` returns (see [agent-personas.md](agent-personas.md#delegating-to-a-saved-team)). Sources are stored with task runs (checkpoint) and flow agent steps (run history). Other tools are not yet registered; a team chair's summary is not checked. |
| Financial statements | Planned | |

States: **Covered** (API and UI), **Planned** (not started).

## Leaderboard regions (#209)

The Leaderboard lists all six world regions with the universes offered in each. Asia, Africa, South America and Oceania
show "No supported market yet": no universe there has been checked against the provider (prices and four quarters of
reports for at least 80% of members, Plan 4 WP3 step 1). A region is added only after that check.

The check is `go run ./cmd/universe-coverage`, which can be run offline (it validates the candidate lists) and, by someone
with provider access, measures each candidate market against the bar. See [universe-coverage.md](universe-coverage.md). Until
a maintainer has run it with access and written a reviewed list into `universes.json`, the regions stay unsupported.

## Index universes (#258)

The Leaderboard has an **Index** selector for S&P 500, Nasdaq-100 and OMX Stockholm 30 (OMXS30). The registry is data
(`internal/server/indices.json`, served by `GET /api/market/indices`) and **carries no members**: FloweySales ships no licensed
constituent list. Each index is `unavailable` until a user supplies a list of their own (CSV import, or a fetch with their own
Financial Modeling Prep key; `internal/indexsource`, `internal/server/index_sources.go`), and is then available to that user only.
The list is stored per user (table `index_sources`) and never committed. Ranking an index the caller has not supplied
(`GET /api/market/leaderboard?universe=index_sp500`) returns 422 with the reason. The hand-picked "Swedish large caps" list is
not OMXS30 and is not presented as it. See [universe-coverage.md](universe-coverage.md#user-supplied-index-constituents-258).

Each index records provider, methodology (summary and link), constituent source, retrieval and as-of dates (UTC), nominal and
actual security counts, exclusions, share-class policy, currency and benchmark convention, licensing limits, and whether
membership history is point-in-time. Validation (`validateIndex`, run at startup and in tests) refuses to publish an index as
available without a verified licence, an https source, an as-of date, well-formed unique constituents (by ISIN or
exchange-qualified symbol, never ticker alone) whose exchange suffix matches the index country.

A maintainer does not enable an index by committing a list; the registry stays empty. A user's own list is validated at import
(duplicates, symbol and exchange format, count against the nominal size, as-of date, licence confirmation) and shown with its
provider, as-of date, count, the licence text they confirmed and whether past membership is known. Freshness over 100 days is
flagged stale. A list with only current members warns about survivorship bias; a list with add/remove records feeds each
backtest rebalance with that date's members and says where it had to fall back to today's.

Not done yet: using an index as a backtest benchmark across currencies and total return, and a
combined-universe mode in the UI (`combineIndexes` deduplicates by canonical identity and reports overlap, and is tested, but
is not wired to an endpoint).
