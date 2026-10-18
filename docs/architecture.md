# Architecture

Current components:

```text
Angular UI ──────────→ Go REST + SSE API ───────→ provider adapters (Yahoo, Finnhub, Avanza, Binance)
    │                           │
    └── chat UI ──→ AI agent ────┼── tool catalog ──→ history, quarterly financials, market stats
                                │
External MCP client → MCP transport → HTTPDataSource → Go REST API
                                │
                          SQLite trading history
```

The built-in chat calls the Go AI agent directly; the standalone MCP server exposes the same tool catalog through JSON-RPC/HTTP. They share backend logic but are separate orchestration paths.

## Data contracts

`get_market_stats` reads **locally recorded** ticks and does not provide a real-time quote. `get_latest_close` uses the latest provider OHLC close and explicitly declares `is_realtime=false`. Yahoo fiscal-period metadata is not verified: financial period labels use `CY#### Q#` and `period_basis=calendar`. Finnhub labels use source-supplied fiscal year and quarter. Verify filings before treating mixed-provider reports as comparable.

## Proposed modules

- `internal/financial`: deterministic ratios, three-statement normalized data and DCF valuation (future).
- `internal/providers`: capability registry, provider response schemas, canonical tickers, normalized periods, caching and rate controls (future).
- `internal/research`: artifacts, source provenance, filing sections, research scorecards and run evaluations (future).
- `internal/risk`: portfolio constraints and consent-gated future execution controls (future).

## Known constraints

A multi-step agent can exhaust its tool budget, and first-phase chat transcripts remain client-side until a durable store exists. Users must treat unsupported tool outcomes as incomplete rather than invented. See roadmap.
