# Roadmap

This is a prioritized, evolving engineering plan—not a promise of delivery dates.

| Phase | Scope | Release gate |
| --- | --- | --- |
| P0 Foundation | CI and PR policy; publish Pages docs; preserve tool-call transcripts; typed absent data; correct fiscal/calendar reporting semantics | Green Go and frontend builds; regression tests |
| P1 Financial research | Annual reports and 10-K/10-Q retrieval; provenance; full-period statement data; ratios + DCF engine; cite document sections | Golden fixture tests against official filings, no invented data |
| P1 Provider expansion | Unified provider adapters, per-endpoint capability registry, rate limiting, cache/TTL, normalization, especially Avanza research and compatible unofficial wrappers | Integration tests, ToS/access review, graceful failures |
| P2 Agent intelligence | Durable task plans, memory by task, evidence ledger, tool budgets, retries, agent evaluation suite | Objective task-completion and hallucination evaluations |
| P2 Investment ranking | Weighted factors, valuation, moat, quality, momentum, risk, data freshness; explainable score and rebalancing suggestions | Out-of-sample backtests, leakage checks, source provenance |
| P3 Risk and paper trading | Portfolio exposure budgets, position sizing, stop-loss & rebalance simulation, transaction cost models, approvals | Walk-forward testing, failure simulation and audit logs |
| P4 Broker automation (future) | Consent-gated execution gateway, kill switch, idempotency, limits, human review, regulatory assessment | Separate security/compliance design approval; *not enabled by default* |

The first pass should deepen market-data reliability, not maximize the number of endpoints indiscriminately.

## Flow Studio / Run History UX progress (2026-10-24)

Nine incremental PRs [#96–#104](flow-studio-progress.md) are merged: independent History, My Flows, node search, canvas improvements, execution graph snapshots, run timing, graph Undo/Redo, Workspace folder deletion, inline preflight and full floating node settings replacing the persistent Details pane. Tracking issue [#95](https://github.com/Doldas/floweysales/issues/95) is still open for typed-edge validation parity, accessibility and UI tests, automatic layout, draft/publish revisions, and more complete diagnostics. See the [progress report](flow-studio-progress.md) for distinctions between delivered functionality and remaining work.
