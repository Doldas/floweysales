# Investor automations

Flow Studio's investor nodes turn a watchlist into automations that watch the
market for you and only speak up when something happened. Every figure is
computed in Go from provider data; agents write prose on top of those rows and
can be forced to cite only figures that are in them. Nothing here trades.

## Playbooks

**Flow editor → Starter templates → Investor playbooks** (also on the Command
Center and in the command palette, Ctrl/⌘ K) copies a ready-made flow as a new,
disabled draft. Set your watchlist on the first data node, save, **run it once**,
publish, then enable it. Administrators get a Notify node and the suggested
schedule; other users get an Output node and run the flow manually.

| Playbook | What it does | Schedule |
| --- | --- | --- |
| Morning watchlist brief | Quotes + technicals → Merge → agent brief grounded on the data | Daily |
| Price move alerts | Quotes → Screen `change_1d_pct <= -4 or from_high_pct <= -20` → Only new (24 h cooldown) → table → Notify | Hourly |
| Technical setups scanner | Technical scan → Screen oversold-in-uptrend or golden cross → Only new (72 h) → table → Notify | Daily |
| SEC filing monitor | SEC filings (8-K/10-Q/10-K) → Only new → agent summary with filing links → Notify | Every 6 h |
| News catalyst digest | News headlines → Only new → agent groups catalysts and risks → Notify | Every 4 h |
| Quality & value screen | Fundamentals → Screen growth, margin, P/E → table → grounded memo bullets | Weekly |
| Top 10 buy-now leaderboard | Leaderboard (US mega caps, *Best buy now*, top 10) → table → grounded memo per pick | Daily |
| New leaderboard entrants | Leaderboard top 10 → Only new (14 days per company) → table → Notify | Daily |
| Cheap & solid bargains | Leaderboard (US dividend & value, *Cheap & solid*) → Screen `pe_ttm < 20`, margin, trend, growing revenue → table → value-trap memo | Weekly |
| Growth at a reasonable price | Leaderboard (US technology, *Growth*) → Screen revenue growth ≥ 10% and `peg < 2` → table → memo | Weekly |
| Momentum leaders alert | Leaderboard (*Momentum*) → Screen above 200-day, not overbought → Only new (7 days) → table → Notify | Daily |
| Quality on a dip (Sweden) | Leaderboard (Sweden large caps, *Quality on a dip*) → Screen 8 %+ below the high, quality ≥ 60 → Only new (3 days) → table → agent (web search for admins) | Daily |
| Quarterly reports, scored | SEC 10-Q/10-K → Only new → Leaderboard of the watchlist, kept to the filers → table → grounded "what changed" memo | Every 6 h |
| Watchlist health check | Leaderboard of the watchlist → Screen score < 45 or warning flags → table → review notes | Weekly |

Alert playbooks use **Skip when the input is empty**, so on quiet days nothing
runs past the filter: no model calls, no messages.

## Nodes

All investor nodes need a version 2 graph. Data nodes take a **watchlist** of up
to 25 symbols, or, when it is empty, the symbols in their input (a JSON array of
tickers, rows with a `symbol` field, `{"symbols": [...]}` or comma-separated
text, for example from an event payload). A symbol that fails becomes an
`{"symbol", "error"}` row instead of failing the node; the node fails only when
every symbol fails. Outputs are JSON arrays of rows, at most 16 KB.

| Node | Output per row | Notes |
| --- | --- | --- |
| **Watchlist quotes** | `price`, `currency`, `as_of`, `change_1d/5d/1m/3m/ytd/1y_pct`, `high_52w`, `low_52w`, `from_high_pct`, `stale` | One year of daily bars per symbol; a change is absent when the series is too short |
| **Technical scan** | `rsi14`, `rsi_state`, `macd_trend`, `macd_hist`, `ema_stack`, `sma50`, `sma200`, `above_sma50/200`, `cross` (`golden`/`death` on the latest bar), `bollinger`, `volatility_pct`, `atr_pct` | Needs 30 daily bars |
| **Fundamentals** | `revenue_ttm`, `revenue_growth_pct`, `gross/operating/net/fcf_margin_pct`, `eps_ttm`, `eps_growth_pct`, `pe_ttm`, `ttm_through` | TTM from the four latest reported quarters; a figure is absent unless every quarter it needs was reported. P/E only when price and EPS share a currency |
| **SEC filings** | `id` (accession), `form`, `filed`, `period_end`, `company`, `url` | SEC EDGAR, last 1–90 days (default 7), optional form filter. Needs `SEC_USER_AGENT` |
| **News headlines** | `id` (URL), `title`, `source`, `published`, `summary`, `url` | From a provider plugin with the `news` capability (Yahoo, Finnhub); only http(s) links are kept |
| **Insider activity** | `buy_count`, `sell_count`, `buyers`, `net_shares`, `net_value`, `largest_buy_value`, `largest_buyer`, `officer_buy`, `cluster_buy`, `latest`, `url` | SEC Form 4 filings (code `P` open-market buys and `S` sales; grants and exercises are ignored), last 1–90 days (default 30), at most 10 filings per symbol. `cluster_buy` is three or more different buyers. Values are absent when no trade had a price. Needs `SEC_USER_AGENT` |
| **Macro regime** | one row: `regime` (`risk_on`, `caution`, `risk_off`), `signals`, `reasons`, `curve_spread`, `curve_inverted`, `curve_uninverting`, `unemployment`, `sahm_rule`, `fed_funds`, `hy_spread`, `credit_stress`, `vix`, `vol_regime`, each value with a `_3m_change`, `missing`, `as_of` | FRED series `T10Y2Y`, `UNRATE`, `FEDFUNDS`, `BAMLH0A0HYM2`, `VIXCLS` through the FRED plugin (needs `FRED_API_KEY`). Signals: curve un-inverting within a year, Sahm rule (3-month average unemployment at least 0.5 above its 12-month low), credit stress (spread at least 5 or up 1.0 in 3 months), VIX 25 or more. Two signals is `risk_off`, one is `caution`. A missing series leaves its signals out |
| **Portfolio check** | a `summary` row (`total_value`, `volatility_pct`, `beta`, `max_drawdown_1y_pct`, `drift_*_pct`, `peak_equity`, `drawdown_from_peak_pct`), one row per alert (`kind` is the alert, e.g. `concentration`, `deep_drawdown`) and one `suggestion` row per rebalancing move | SYS_ADMIN only, like the paper portfolio. Uses [portfolio analytics](wealth-plan.md#portfolio-risk) and the peak in `paper_equity_peak` |
| **Dividends & earnings** | `id` (`symbol\|kind\|date`), `kind`, `date`, `days_until`, `amount`, `currency`, `ratio`, `confirmed`, `provider` | From a provider plugin with the `events` capability. `limit` is the days ahead (default 30, at most 90), `lookback_days` reaches into the past, `kinds` filters (`dividend`, `split`, `earnings`) |
| **Leaderboard** | `rank`, `score`, `grade`, the six factor scores, `coverage_pct`, key figures, `reasons`, `flags` | See [Leaderboard](#leaderboard). A preset universe or a watchlist; passes on the top 1–25 |
| **Screen** | the matching rows | Rule, sort (`-field` for descending) and limit; rows with `error` never pass |
| **Only new since last run** | rows not passed on before | Remembers up to 2,000 keys per node for 180 days (Liquibase 032, `agent_flow_node_state`). Key: chosen fields, else `id`, `url` or `symbol`. A cooldown lets a row through again after N hours |
| **Report table** | Markdown table | Columns chosen or automatic; empty input renders nothing |
| **Notify** | passes its input on | SYS_ADMIN only, side effect `notify.send`, see below |

### Screen rules

```
change_1d_pct <= -3 and (rsi14 < 30 or from_high_pct < -20)
rsi_state == "oversold" and not above_sma200
title contains "guidance"
```

Comparisons `< <= > >= == !=` (`=` means `==`) and `contains`, combined with
`and`, `or`, `not` and parentheses. Text comparisons ignore case; bare words
compare as text (`cross == golden`). A row without the field never matches: an
absent figure is not zero. Rules are parsed, never evaluated as code, and are
limited to 500 characters. The editor suggests the fields of the node feeding
the screen.

### Skip when empty

Any version 2 node except Trigger, Merge and Value can set **Skip when the input
is empty**. When its input is `""`, `[]`, `{}` or `null` the node is recorded as
SKIPPED and so is everything that depends only on it. The If / Else node also
has **Has rows / is not empty** and **Is empty** operators.

### Grounded agents without tools

**Require grounded figures** no longer needs research tools. Without tools every
figure in the answer must appear in the node input, for example the rows of a
watchlist or fundamentals node; otherwise the attempt fails and its retries
apply. This is how the brief and memo playbooks keep agents to the data.

## Leaderboard

The leaderboard ranks companies that look worth buying now. It is on the
**Markets** page (pick a universe and a strategy, click a row to open the
company, *Research the top 5* with the agent or *Automate as a flow*), in the
Leaderboard flow node and at `GET /api/market/leaderboard?universe=…&strategy=…`
(or `symbols=` with up to 25 tickers; readable by `research.read`).

For every company it loads a year of daily prices and the reported quarters,
then scores six factors. Each factor is the weighted average percentile of its
metrics **within the ranked universe** (0 = worst, 100 = best of these):

| Factor | Metrics |
| --- | --- |
| Value | earnings yield (1 / trailing P/E; losses count as −10 %), PEG (P/E ÷ EPS growth) |
| Quality | operating, net, free-cash-flow and gross margin (TTM), profitable quarters of the last four |
| Growth | TTM revenue and EPS growth year over year, revenue acceleration (latest quarter's growth minus the previous quarter's) |
| Momentum | 12-1 month return (skips the latest month), 3-month return, trend points (above the 200-day average, bullish EMA stack, bullish MACD) |
| Entry timing | closeness to a ~12 % pullback from the 52-week high and to RSI 45: cheaper than the peak, not broken, not overbought |
| Risk | annualized volatility (lower is better), maximum drawdown over the year |

Hard caps keep a relative ranking honest: a loss-making company scores at most
15 on value, RSI 75+ at most 20 on timing, and a price below the 200-day
average at most 35 on momentum. A metric only one company has is ignored; a
factor with no data counts as 40 and lowers `coverage_pct`. A strategy weighs
the factors into the score (grades A ≥ 75, B ≥ 62, C ≥ 48, D ≥ 35, else E):

| Strategy | Value | Quality | Growth | Momentum | Timing | Risk |
| --- | --- | --- | --- | --- | --- | --- |
| Best buy now | 22 % | 20 % | 20 % | 18 % | 12 % | 8 % |
| Cheap & solid | 50 % | 25 % | | | 15 % | 10 % |
| Growth at a fair price | 20 % | 20 % | 45 % | 15 % | | |
| Momentum leaders | | 10 % | 20 % | 60 % | | 10 % |
| Quality compounders | 10 % | 50 % | 20 % | | | 20 % |
| Quality on a dip | 20 % | 30 % | | 15 % | 35 % | |

`reasons` cites the figures behind the strongest weighted factors and `flags`
lists what to check before buying: `overbought`, `below_sma200`,
`death_cross`, `unprofitable`, `shrinking_revenue`, `no_fundamentals`,
`stale_price`. Screens can use both, e.g. `not (flags contains "overbought")`.

Universes (Yahoo tickers): **US mega caps**, **US technology**, **US dividend &
value** (25 each), **Sweden large caps** (30, OMXS30 style) and **European
leaders** (25). A universe's metrics are reused for 15 minutes, so switching
strategy is instant and concurrent viewers share one fetch. Symbols without
data are listed as not ranked. When a node has a universe or watchlist and its
input is JSON naming symbols (for example new filings), the whole list is
ranked and only those symbols are passed on, each with its rank in the list.

Scores compare companies with each other on reported and price data. They are
not forecasts or price targets and nothing here trades.

## Backtest

A ranking nobody has tested is a guess, so the leaderboard can replay a
strategy over the past. On **Markets → Leaderboard** choose *Backtest this
strategy*, or call
`GET /api/market/backtest?universe=…&strategy=…&top=10&rebalance=monthly|quarterly&cost_bps=10&years=3|5`
(or `symbols=` with up to 25 tickers; readable by `research.read`).

**Method.** The first rebalance is the first trading day on or after the start
date once `top` symbols have a year of prices. Every 21 trading days (63 for
quarterly) the leaderboard scores the universe exactly as it does today, the
best `top` companies are held at equal weight, and weights then drift with
prices until the next rebalance. Each trade costs `cost_bps` per side (default
10). The benchmark is held in full with no costs: SPY for the US universes,
`^OMX` for Sweden and `^STOXX50E` for Europe. Metrics are computed from the
daily curve: yearly return (CAGR), volatility, Sharpe and Sortino (risk-free
rate taken as 0), worst peak-to-trough fall, the share of rebalance periods
that beat the benchmark, and average turnover. The first year of prices is
warm-up, so a five-year test fetches ten years of history.

**Point in time.** At each rebalance the strategy sees only prices up to that
day and only quarters that would have been published: a quarter counts from its
period end plus 60 days. Nothing later leaks into an earlier choice.

**Warnings** are returned with every result and shown above it:

| Id | Meaning |
| --- | --- |
| `survivorship_bias` | Always. The universe is today's list; companies that fell out of it are missing, which flatters past returns. |
| `price_index_benchmark` | The benchmark has no dividends. |
| `thin_fundamentals` | Fewer than 60 % of symbols had four reported quarters on average, so early picks lean on price factors. |
| `short_history` | Fewer than 24 rebalances fit the history. |
| `benchmark_unavailable`, `symbols_unavailable` | The benchmark or some symbols could not be fetched. |

With fewer than six rebalances the request fails instead of guessing. Results
are cached for six hours and identical concurrent requests share one run. Past
results do not predict future ones; the backtest is a check on a strategy, not
a forecast.

## Notifications

A **Notify** node sends its input, with JSON rows rendered as a table and a
footer naming the flow, run and a not-investment-advice note:

- **Email the flow owner** through the SMTP settings (Settings → Email).
- **Chat webhook**: one Slack, Discord, Microsoft Teams or generic JSON
  endpoint, configured by a SYS_ADMIN under *Function library & triggers →
  Notifications*. The URL must be public https; it is sealed with
  `FLOWEYSALES_SECRETS_KEY` (stored as integration `notify_webhook`), never put
  in a graph or sent back to the browser, and the connection is pinned to the
  resolved public address with redirects refused. **Send test message** checks it.

The subject accepts `{{flow}}`, `{{date}}` and `{{count}}`. Empty input sends
nothing. Notify is a side effect like Workspace writes: it is audited, can
require approval, and makes the flow subject to the production-readiness gate.

API (SYS_ADMIN): `GET /api/admin/flow-notify-channels`,
`PUT|DELETE /api/admin/flow-notify-channels/webhook` (`{"provider","url"}`),
`POST /api/admin/flow-notify-channels/test`. Changes are in the change log.

## Plugins for investors

Beyond the market-data providers, the official store has:

- **News** (`news` capability) in Yahoo 1.1 and Finnhub 1.1.
- **FRED** — macro series as symbols: `DGS10` (10-year Treasury), `T10Y2Y`
  (yield curve), `CPIAUCSL` (CPI), `UNRATE`, `FEDFUNDS`. Needs a free
  `FRED_API_KEY`. The `currency` field carries the series' units.
- **ECB** — euro reference rates and derived crosses: `EURUSD`, `USDSEK`.

They work in every watchlist, technical and screen node: set the node's
provider to `FRED` or `ECB`.
