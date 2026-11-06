---
title: What's new
order: 13
summary: What changed in the November 2026 release, and what is still not available.
last-reviewed: 2026-11-06
---

# What's new

## November 2026 release

**Agents**
- Agent answers get a claim check: each cited sentence is compared with the cited text for company, metric, period, unit and direction, and shown as supported, contradicted, not proven or not checked, with the source passage under **Why is this claim supported?** See [Agents, teams and evidence](agents.md).

**Backtests**
- Choose a custom period in UTC, test several lists and periods at once, and check how trading cost, holdings and rebalance interval change a result. See [Strategies and backtests](strategies.md).
- The Strategies page can resample a replay's closed trades with a seed to show percentile bands and the spread of the worst fall. See [Strategies and backtests](strategies.md).
- Results are cut into consecutive parts of the period, and the result lists the biases that apply. Saved runs carry a source badge and export with their provenance.
- Slippage and a fill delay (fill 1 or 2 trading days after the ranking) in leaderboard backtests, both included in the settings check. A collapsed "How fills were assumed" section states what is and is not modeled, and the benchmark is defined with its currency and dates. Mixed currencies and a benchmark in another currency are flagged.
- Longer history: a period that starts more than nine years back asks the provider for its longest daily history, and says so when only the 10-year window could be served.
- Rule strategies get a slippage setting and a **walk-forward fit** that chooses up to two of their numbers on one window and tests them on the next. See [Strategies and backtests](strategies.md#walk-forward-fit).

**Research Agent**
- The agent can hand a question to a team and show each member's answer with attribution.
- Research answers can be exported as Markdown or JSON with the evidence policy, sources and check result. See [Research Agent](agents.md).
- Evidence settings choose standard, evidence-only or primary-only answers, with an optional age limit and source classes. You can save your own default. See [Research Agent](agents.md).
- Teams can have a ceiling on tokens, cost and tool calls.

**Markets**
- The feed status badge separates the connection, freshness, kind of data and market session.
- The instrument chip in the top bar has favorites, recent instruments and a full-screen picker on phones that also shows the full instrument identity and holds the favorite button. The phone top bar stays on one row without the page title or a star, with low-frequency actions in a More menu. See [Markets](markets.md).

**Flow Studio**
- **Test unsaved draft** runs the graph in the editor without saving, including a brand-new flow. Side effects are simulated unless you confirm real ones, and Run history marks each run as a draft test or a saved version with its snapshot hash. See [Flows](flows.md).

**Leaderboard**
- A method panel with a fingerprint, origin of each company on the list, bias statements, a coverage-by-region view, and your own objective and constraints. See [Leaderboard](leaderboard.md).

**Files**
- Markdown files have Edit source, Preview, Rich text and (on wide screens) Split views, with a visible saved or unsaved state. A folder's `README.md` is shown as its overview. See [More areas](more-areas.md).

**Plugins**
- Plugins survive app upgrades: one that no longer fits is kept with its settings and data instead of being removed. Plugins can declare which FloweySales versions they support, an incompatible version cannot be installed, the version an update replaced can be restored, and a report after an upgrade shows what was kept and verified. See [Settings](settings.md).

**Documentation**
- This guide, a tutorial, and a weekly check of outside links.

## Not available yet

- Markets in Asia, Africa, South America and Oceania. A region is added only after a provider's prices and reports for it have been checked.
- Walk-forward testing of leaderboard strategies. The factor rankings have no fitted parameters to fit.
- Point-in-time lists of index members, so survivorship bias is stated but not measured.
- Bid-ask spread, order size against traded volume, partial fills and market impact in backtests.
- A quote time and delay from every provider. Providers that do not send them show them as unknown.
- Price history beyond what the provider returns as daily bars.
