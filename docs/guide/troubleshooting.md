---
title: Troubleshooting and FAQ
order: 14
summary: Common questions about empty screens, stale prices, missing companies and AI answers.
last-reviewed: 2026-11-06
---

# Troubleshooting and FAQ

**Markets, Financials or the leaderboard are empty.** Prices and reports come from provider plugins. Without one installed by an administrator the screens are empty and say so. See [Settings](settings.md) and [Getting started](getting-started.md).

**The badge says "No new prices since ...".** The connection is open but nothing recent arrived. The market may be closed. Use Reconnect in the badge if it looks stuck. See [Markets](markets.md).

**A price says "Delayed" or "Last update", not "Live prices".** Live is shown only when the provider says its quotes are real time and an update arrived recently. Read the badge before acting on a price.

**A company is missing from the leaderboard.** Companies with no usable prices or fewer than four reported quarters are left out. The origin panel names each one with the reason. Your own constraints can also remove companies; **Show without them** returns the plain ranking. See [Leaderboard](leaderboard.md).

**The first leaderboard load is slow.** The first load of a list takes a while; later switches are quick.

**My backtest differs from yesterday's.** Prices and reports change, and so can the method version. A saved run shows the result exactly as recorded. See [Strategies and backtests](strategies.md).

**The custom period is refused or sensitivity is unavailable.** Sensitivity needs a 3Y, 5Y or 10Y period, not custom dates.

**The agent says it could not find a source.** In evidence-only or primary-only mode the agent holds back claims it cannot support. Loosen the evidence setting or ask a narrower question. See [Research Agent](agents.md).

**Summarize is unavailable.** A summary needs an AI model to be configured by an administrator.

**My paper portfolio or market history is gone after a restart.** The SQLite file that holds them is set by `SQLITE_STORAGE_PATH`; the example configuration uses `:memory:`, which keeps nothing. See [Install, run and back up](install-and-run.md#what-to-back-up).

**Saved API keys stopped working after a move or restore.** Credentials are encrypted with the secrets key. Restore the key that was used when they were saved, or enter the credentials again.

**A term is unclear.** See the [Glossary](glossary.md).

**Can FloweySales place a trade?** No screen in the guide places orders.

**Why UTC?** All displayed times are in UTC so that dates mean the same thing everywhere.

If a problem is not here, see [Help](help.md).
