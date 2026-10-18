# FloweySales vision

**Mission:** Help people make more informed investment decisions with locally controlled AI, auditable research and reliable market data.

## Product principles

1. **Evidence before confidence.** No market or statement figures without a source, timestamp and period. Separate facts, assumptions, models and predictions.
2. **Agentic, not opaque.** Multi-step research plans should expose the tools invoked, intermediate calculations, limitations, and the reason for a ranking.
3. **Local first.** OpenAI-compatible local inference (Ollama/LM Studio/llama.cpp), with opt-in external providers.
4. **Model-independent finance.** Financial math, validations, portfolio/risk limits and transaction guards belong in deterministic code, not model prose.
5. **Human agency first.** Provide rankings, buy/sell watchlists and alerts for investigation; require human review for real transactions. No automatic investment decisions by default.
6. **Compliance by design.** Preserve audit evidence, secrets hygiene, appropriate provider terms and clear disclosures. Product jurisdictional requirements must be assessed before third-party distribution.

## Planned research flow

Universe screen → verify company identity → ingest filings and market data → calculate deterministic ratios → scenario valuation → compare risks → rank with explainable weights → cite primary sources → produce proposal and confidence range → human review / paper-trade.

The work toward fully automated trading is a *research direction*, not currently implemented functionality. It needs backtesting, robust risk controls, authenticated broker integrations, monitoring, rollback and an explicit go/no-go review.
