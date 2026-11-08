# Architecture

Current components:

```text
Angular/Bootstrap UI → Go REST + SSE API ───────→ provider plugins (separate processes; Yahoo, Finnhub, Avanza, Binance from the plugin store)
    │                           │
    ├── chat UI ──→ AI agent ────┼── tool catalog ──→ history, quarterly financials, market stats, SEC, web search
    │                           │
    └── Flow Studio ──→ flow engine ─ nodes: agent, research, screen, notify, workspace, …
                                │
External MCP client → MCP transport → HTTPDataSource → Go REST API
                                │
                     MySQL (Liquibase-managed schema)
```

The built-in chat calls the Go AI agent directly; the standalone MCP server exposes the same tool catalog through JSON-RPC/HTTP. They share backend logic but are separate orchestration paths.

## Data contracts

`get_market_stats` reads **locally recorded** ticks and does not provide a real-time quote. `get_latest_close` uses the latest provider OHLC close and explicitly declares `is_realtime=false`. Yahoo fiscal-period metadata is not verified: financial period labels use `CY#### Q#` and `period_basis=calendar`. Finnhub labels use source-supplied fiscal year and quarter. Verify filings before treating mixed-provider reports as comparable.

## Modules

Everything below exists in the repository today (`internal/`).

- `server`: REST and SSE handlers, flow engine, authorization and the agent endpoints.
- `ai`: the research agent, teams and tool catalog; `persona`: the investor characters; `eval`: scoring of agents on a fixed dataset.
- `market`, `plugins`, `tracker`: the provider-agnostic market model, the plugin runtime and parallel trackers.
- `sec`, `valuation`: SEC EDGAR metadata and XBRL with provenance; deterministic DCF and ratios.
- `sources`, `sourcetrust`, `grounding`, `provenance`: what a run retrieved, how trustworthy it is, whether stated figures appear in it, and where every displayed value came from.
- `websearch`, `vision`: search providers with failover and budgets; PDF and image understanding.
- `strategy`, `backtest`, `montecarlo`, `portfolio`, `planner`, `trading`: rule strategies, ranking backtests, resampled robustness, portfolio risk, the wealth plan and the simulated broker (paper only; no real orders).
- `indexsource`, `universecheck`: user-supplied index constituents and the check that a market may be offered.
- `identity`, `storage`, `redact`, `mailer`, `notify`, `updates`: sign-in, MySQL access, credential masking, the single mail boundary, notifications and container updates.
- `mcp`: the standalone Model Context Protocol server. `pluginsdk` is a separate Go module for plugin authors.

The database is MySQL with a Liquibase schema ([database-and-auth.md](database-and-auth.md)). SQLite is used only by the one-time import tool `cmd/migrate-sqlite`.

## Known constraints

A multi-step agent can exhaust its tool budget, and first-phase chat transcripts remain client-side until a durable store exists. Users must treat unsupported tool outcomes as incomplete rather than invented. See roadmap.


### Shared evidence popovers

Source details use the shared Angular `fs-source` component rather than page-specific tooltips. It exposes an explicit button (`aria-expanded`, `aria-controls`, `aria-haspopup`), supports Escape dismissal with focus restoration and lays its detail panel out inside narrow phone viewports. New pages should reuse this primitive and the common provenance formatter, not independently render source URLs or timestamps. The popover is informational and does not imply that the origin was verified. See issue #263.
