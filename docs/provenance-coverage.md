# Provenance coverage

Which surfaces describe the origin of what they show, using the contract in
[ADR 0003](adr/0003-provenance-contract.md). Issue #204 tracks this table.

| Surface | State | Notes |
| --- | --- | --- |
| World headlines (World page, company impact panel) | Covered | `provenance` on each headline, shown with `fs-source`. Web search results claim no publication time. |
| Flow nodes (all rows) | Covered | Every data-producing investor node stamps each row with `provenance` (`flow_provenance.go`); select, only-new, report and notify nodes keep the fact the rows carry, and a report table ends with a Sources line naming the origins or the number of rows with an unknown source. News rows never invent a publication time. |
| Leaderboard (page, flow node, agent tool) | Covered | `provenance.fact` (ranking, calculated, method id and version), `provenance.universe_fact` and `metrics_source.fact` per row on the page; the flow node stamps its rows; the agent tool returns `provenance`. Not covered: the per-metric fundamentals period (the row states the newest reported quarter, `ttm_through`). |
| Quotes, history, ticks and feed status | Covered | `GET /api/quote`, `/api/history`, `/api/prices`, `/api/stats` and each streamed tick carry a shared `provenance` fact (provider, quote time as the as-of, poll or fetch time, provider page where known). A provider that gives no quote time gets none invented. The feed status popover still shows delay and session. |
| Financial statements | Covered | `provenance` on `GET /api/financials` (provider, newest period end as-of) shown on the Financials page; SEC filings, facts and sections name EDGAR, the document, filing date and period. |
| Saved and live backtests | Covered | Saved runs and exports (method id and version, time, provider, last price date; `partial` for batch and settings checks, which record no provider) and the live single, batch, Monte Carlo and sensitivity endpoints, shown with `fs-source`. |
| Strategy results | Covered | Backtest, walk-forward and Monte Carlo results carry `provenance` with method ids `strategy.*`. |
| Calculations (DCF, position size, wealth plan, portfolio analytics, positions, Today briefing) | Covered | Calculated facts naming inputs; assumptions (plan profile, DCF scenarios, risk settings) are stated as inputs, not observed values. Each Today part has its own fact. |
| Plugin and domain-provider results | Covered | Observed, partial: the plugin is named as the provider and FloweySales says it does not verify the content. |
| Agent tool results | Covered | `sources.ToolOrigins` classifies every tool; results of data tools get a `provenance` object, and a test fails when a tool is missing. Citable sources (search, read, SEC section, delegated teams) keep their numbered registry. |
| Agent answers (chat, teams, tasks) | Partial | See [agent-evidence.md](agent-evidence.md). Answer text is model output; figures in prose are not traced back to a tool result. A team chair's summary is not citation-checked. |

States: **Covered** (API and UI), **Partial** (some of it), **Exempt** (not data about markets or results, with a reason).

## How coverage is measured (#204)

The table is no longer only manual. `internal/server/provenance_registry.go` classifies every HTTP route as covered, partial or exempt with a reason, and `provenance_registry_test.go` reads the route table from the source and fails when:

- a route is not classified (a new endpoint cannot ship without saying how its data is attributed);
- a route marked covered no longer mentions provenance in its handler (or in the files it names);
- a registry entry names a route that does not exist, or an exemption has no reason;
- the share of fully covered data routes falls below 85%.

Further guards: every investor flow node type must be classed as stamped or pass-through (`flow_provenance.go`); every agent tool must have an origin in `sources.ToolOrigins`; `frontend/src/app/core/provenance-surfaces.spec.ts` fails when a view stops binding `fs-source` to its provenance. Responses are serialized with the fact beside the data, so export (JSON, flow output) keeps it.

## Leaderboard regions (#209)

The Leaderboard lists all six world regions with the universes offered in each. Asia, Africa, South America and Oceania
show "No supported market yet": no universe there has been checked against the provider (prices and four quarters of
reports for at least 80% of members, Plan 4 WP3 step 1). A region is added only after that check.

The check is `go run ./cmd/universe-coverage`, which can be run offline (it validates the candidate lists) and, by someone
with provider access, measures each candidate market against the bar. See [universe-coverage.md](universe-coverage.md). Until
a maintainer has run it with access and written a reviewed list into `universes.json`, the regions stay unsupported.

### Global scope and comparability (#209)

`universe=global` is the union of the curated lists, so it reaches exactly the regions that have one (North America and
Europe); it is not a list anyone chose and it redistributes no index constituents. Region, country, exchange and
currency filters narrow any list, and every company a filter removes is named with the reason. Percentiles are taken within
each region by default for `global`, or across one pool on request. Prices stay in local currency: there is no FX source,
so no rate is applied and the response states that (`scope.comparability.fx`). Not done: an FX source with a named provider
and date, per-company accounting standard, sector and liquidity filters, and a market in Asia or Africa; each needs
provider access (see [universe-coverage.md](universe-coverage.md)).

### Identity and providers (#208)

A company's row carries `identifier.isin` only when an ISIN has been recorded and verified (checksum-validated) in
`universes.json`; none is recorded today and the row says "not recorded". `discovery.how` states how it was matched and
`list_version` names the version of the list. A second provider can fill companies the first could not load and flag price
disagreements; the row names the provider that supplied its figures.

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
