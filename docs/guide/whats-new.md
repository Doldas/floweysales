---
title: What's new
order: 13
summary: What changed in the November 2026 release, and what is still not available.
last-reviewed: 2026-11-06
---

# What's new

## November 2026 release

**Backtests**
- Choose a custom period in UTC, test several lists and periods at once, and check how trading cost, holdings and rebalance interval change a result. See [Strategies and backtests](strategies.md).
- Results are cut into consecutive parts of the period, and the result lists the biases that apply. Saved runs carry a source badge and export with their provenance.

**Research Agent**
- The agent can hand a question to a team and show each member's answer with attribution.
- Evidence settings choose standard, evidence-only or primary-only answers, with an optional age limit and source classes. You can save your own default. See [Research Agent](agents.md).
- Teams can have a ceiling on tokens, cost and tool calls.

**Markets**
- The feed status badge separates the connection, freshness, kind of data and market session.
- The instrument chip in the top bar has favorites, recent instruments and a full-screen picker on phones. See [Markets](markets.md).

**Flow Studio**
- **Test unsaved draft** runs the graph in the editor without saving, including a brand-new flow. Side effects are simulated unless you confirm real ones, and Run history marks each run as a draft test or a saved version with its snapshot hash. See [Flows](flows.md).

**Leaderboard**
- A method panel with a fingerprint, origin of each company on the list, bias statements, a coverage-by-region view, and your own objective and constraints. See [Leaderboard](leaderboard.md).

**Files**
- Markdown files have Edit source, Preview, Rich text and (on wide screens) Split views, with a visible saved or unsaved state. A folder's `README.md` is shown as its overview. See [More areas](more-areas.md).

**Documentation**
- This guide, a tutorial, and a weekly check of outside links.

## Not available yet

- Markets in Asia, Africa, South America and Oceania. A region is added only after a provider's prices and reports for it have been checked.
- True walk-forward testing. The factor rankings have no fitted parameters to fit.
- A quote time and delay from every provider. Providers that do not send them show them as unknown.
- Long price history beyond what a backtest fetches (10 years).
