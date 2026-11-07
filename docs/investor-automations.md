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
| Ashgrove: compounders at a fair price | Leaderboard `owner_compounder` → Screen → table → agent memo as the character Walter Ashgrove → Notify | Weekly |
| Holm: defensive value | Leaderboard `teacher_defensive` → Screen → table → memo as Bernhard Holm → Notify | Weekly |
| Fairbanks: growth at a reasonable price | Leaderboard `pickerupper_garp` → Screen → table → memo as Marty Fairbanks → Notify | Weekly |
| New leaderboard entrants | Leaderboard top 10 → Only new (14 days per company) → table → Notify | Daily |
| Cheap & solid bargains | Leaderboard (US dividend & value, *Cheap & solid*) → Screen `pe_ttm < 20`, margin, trend, growing revenue → table → value-trap memo | Weekly |
| Growth at a reasonable price | Leaderboard (US technology, *Growth*) → Screen revenue growth ≥ 10% and `peg < 2` → table → memo | Weekly |
| Momentum leaders alert | Leaderboard (*Momentum*) → Screen above 200-day, not overbought → Only new (7 days) → table → Notify | Daily |
| Quality on a dip (Sweden) | Leaderboard (Sweden large caps, *Quality on a dip*) → Screen 8 %+ below the high, quality ≥ 60 → Only new (3 days) → table → agent (web search for admins) | Daily |
| Quarterly reports, scored | SEC 10-Q/10-K → Only new → Leaderboard of the watchlist, kept to the filers → table → grounded "what changed" memo | Every 6 h |
| Watchlist health check | Leaderboard of the watchlist → Screen score < 45 or warning flags → table → review notes | Weekly |
| Insider buying clusters | Insider activity → Screen `cluster_buy or (officer_buy and net_value > 250000)` → Only new (14 days) → table → Notify | Daily |
| Macro regime shift | Macro regime → Only new on `regime` (reminds every 14 days) → agent explains what changed, grounded → Notify | Daily |
| Portfolio drawdown guard | Portfolio check → Screen `drawdown_from_peak_pct <= -10 or kind == "deep_drawdown"` → Only new (72 h) → table → Notify. **Administrators only** | Hourly |
| Monthly rebalance check | Portfolio check → Screen `kind == "suggestion"` → table → grounded rebalance notes → Notify. **Administrators only** | Monthly |
| Dividend & earnings calendar | Dividends & earnings (30 days ahead) → Only new → table → Notify | Weekly |
| Monthly phase check-in | Wealth plan → grounded progress note (phase, probability of the goal, next checklist item) | Monthly |
| Earnings this week, scored | Dividends & earnings (earnings, 7 days) → Leaderboard of the watchlist, kept to the reporters → table → memo | Weekly |
| Strategy scorecard | Backtest of all six strategies on US mega caps → table → agent compares them and states the bias warnings | Monthly |

Monthly means every 30 days (43,200 minutes, the longest schedule). Alert
playbooks use **Skip when the input is empty**, so on quiet days nothing
runs past the filter: no model calls, no messages.

## Nodes

All investor nodes need a version 2 graph. Data nodes take a **watchlist** of up
to 25 symbols, or, when it is empty, the symbols in their input (a JSON array of
tickers, rows with a `symbol` field, `{"symbols": [...]}` or comma-separated
text, for example from an event payload). A symbol that fails becomes an
`{"symbol", "error"}` row instead of failing the node; the node fails only when
every symbol fails. Outputs are JSON arrays of rows, at most 256 KiB by default. `FLOW_NODE_OUTPUT_MAX_BYTES` raises or lowers the limit (16 KiB to 1 MiB). A node over the limit fails with its size and the limit; it never passes on a cut-off result. An agent node accepts at most 64 KiB of previous output and fails with a message to add a `screen` or `report_table` node first, rather than reading cut-off JSON. An email that is shortened says so and names the run that holds the full output. A run's resume checkpoint keeps each large node output once, so long outputs no longer fill the 8 MiB checkpoint cap after a few nodes. The node outputs a run holds at once are also capped, at 32 MiB in total; a run over the budget fails with the total and the largest node. If a checkpoint cannot be saved (for example the state is over the 8 MiB cap), the run keeps going and its steps show one `WARNING` row, "Checkpoint not saved", with the size, the reason and where a lost worker would resume from. Later checkpoints are still tried.

| Node | Output per row | Notes |
| --- | --- | --- |
| **Watchlist quotes** | `price`, `currency`, `as_of`, `change_1d/5d/1m/3m/ytd/1y_pct`, `high_52w`, `low_52w`, `from_high_pct`, `stale` | One year of daily bars per symbol; a change is absent when the series is too short |
| **Technical scan** | `rsi14`, `rsi_state`, `macd_trend`, `macd_hist`, `ema_stack`, `sma50`, `sma200`, `above_sma50/200`, `cross` (`golden`/`death` on the latest bar), `bollinger`, `volatility_pct`, `atr_pct` | Needs 30 daily bars |
| **Fundamentals** | `revenue_ttm`, `revenue_growth_pct`, `gross/operating/net/fcf_margin_pct`, `eps_ttm`, `eps_growth_pct`, `pe_ttm`, `ttm_through` | TTM from the four latest reported quarters; a figure is absent unless every quarter it needs was reported. P/E only when price and EPS share a currency |
| **SEC filings** | `id` (accession), `form`, `filed`, `period_end`, `company`, `url` | SEC EDGAR, last 1–90 days (default 7), optional form filter. Needs `SEC_USER_AGENT` |
| **News headlines** | `id` (URL), `title`, `source`, `published` (UTC), `date_status`, `retrieved_at` (UTC), `summary`, `url` | From a provider plugin with the `news` capability (Yahoo, Finnhub); only http(s) links are kept. Newest first. `date_status` is `unavailable` when the provider gave no date: the row has no `published` and sorts last. `retrieved_at` is when we fetched it, not the article's date |
| **Insider activity** | `buy_count`, `sell_count`, `buyers`, `net_shares`, `net_value`, `largest_buy_value`, `largest_buyer`, `officer_buy`, `cluster_buy`, `latest`, `url` | SEC Form 4 filings (code `P` open-market buys and `S` sales; grants and exercises are ignored), last 1–90 days (default 30), at most 10 filings per symbol. `cluster_buy` is three or more different buyers. Values are absent when no trade had a price. Needs `SEC_USER_AGENT` |
| **Macro regime** | one row: `regime` (`risk_on`, `caution`, `risk_off`), `signals`, `reasons`, `curve_spread`, `curve_inverted`, `curve_uninverting`, `unemployment`, `sahm_rule`, `fed_funds`, `hy_spread`, `credit_stress`, `vix`, `vol_regime`, each value with a `_3m_change`, `missing`, `as_of` | FRED series `T10Y2Y`, `UNRATE`, `FEDFUNDS`, `BAMLH0A0HYM2`, `VIXCLS` through the FRED plugin (needs `FRED_API_KEY`). Signals: curve un-inverting within a year, Sahm rule (3-month average unemployment at least 0.5 above its 12-month low), credit stress (spread at least 5 or up 1.0 in 3 months), VIX 25 or more. Two signals is `risk_off`, one is `caution`. A missing series leaves its signals out |
| **Portfolio check** | a `summary` row (`total_value`, `volatility_pct`, `beta`, `max_drawdown_1y_pct`, `drift_*_pct`, `peak_equity`, `drawdown_from_peak_pct`), one row per alert (`kind` is the alert, e.g. `concentration`, `deep_drawdown`) and one `suggestion` row per rebalancing move | SYS_ADMIN only, like the paper portfolio. Uses [portfolio analytics](wealth-plan.md#portfolio-risk) and the peak in `paper_equity_peak` |
| **Dividends & earnings** | `id` (`symbol\|kind\|date`), `kind`, `date`, `days_until`, `amount`, `currency`, `ratio`, `confirmed`, `provider` | From a provider plugin with the `events` capability. `limit` is the days ahead (default 30, at most 90), `lookback_days` reaches into the past, `kinds` filters (`dividend`, `split`, `earnings`) |
| **Wealth plan** | one row: `phase`, `phase_job`, `next_checklist_item`, `core_equity_pct`/`bonds_pct`/`cash_pct`/`satellite_pct`, `fi_number`, `fi_ratio_pct`, `goal_amount`, `probability_goal_pct` (with `probability_label`), `years_to_goal_p50`, `required_contribution_75`, `projection` | The saved plan of the flow's owner, seed 1 so figures are stable between runs. Empty (with a note) when no plan is saved. See [wealth plan](wealth-plan.md) |
| **Strategy backtest** | one row per strategy: `cagr_pct`, `excess_cagr_pct`, `volatility_pct`, `sharpe`, `sortino`, `max_drawdown_pct`, `calmar`, `hit_rate_pct`, `avg_turnover_pct`, `rebalances`, `warnings`, `warning_text` | Settings: universe or watchlist, strategy (or `all`), holdings (`limit`, default 10) and `years` (3 or 5). Monthly rebalancing, 10 bps per trade. At most one per flow, 120 s budget. See [Backtest](#backtest) |
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
`GET /api/market/backtest?universe=…&strategy=…&top=10&rebalance=monthly|quarterly&cost_bps=10&slippage_bps=0&years=3|5|10` (or `from=YYYY-MM-DD&to=YYYY-MM-DD`, UTC; the response carries `from`, `to`, `data_start`, `data_end` and a `notice` when the data is shorter than asked)
(or `symbols=` with up to 25 tickers; readable by `research.read`).

**Consecutive parts.** A backtest of at least about 1.5 years also returns `segments`: the period cut into three equal consecutive parts, each measured on its own (return per year, volatility, worst drop, Sharpe, and the benchmark's return and the excess when there is a benchmark), with `segment_summary` ("Beat the benchmark in 2 of 3 consecutive parts.") and a `segment_note`. This shows whether a result holds up over time or came from one stretch. It is **not** out-of-sample or walk-forward testing: nothing is fitted on one part and tested on another, and the factors and settings were chosen by someone who had seen the past. Neighbouring parts share their boundary day. A shorter period returns no parts rather than parts too short to mean anything. The parts appear under the chart and in the facts sheet of a saved-run summary.

**Stress windows.** A backtest also returns `stress`: how the strategy and the benchmark did inside named past falls, 2007-10-09 to 2009-03-09 (2008 financial crisis), 2020-02-19 to 2020-03-23 (pandemic crash) and 2022-01-03 to 2022-10-12 (rate-rise bear market), all UTC. A window is shown only when the replayed curve covers all of it; the others are named in `stress_not_covered` and not tested (a backtest fetches at most 10 years of prices, so today the 2008 window is always listed as not covered). Each window gives the total return (not per year) of the strategy and the benchmark, the excess and the worst fall inside it. `stress_note` states the caveat: the windows were picked with hindsight, after the falls were known.

**Market regimes.** With a benchmark, `regimes` groups the replayed days by a label computed from the benchmark's own prices up to the previous close: uptrend when it closes at or above its 200-day average, otherwise downtrend; volatile when its 63-day volatility is 20% a year or more, otherwise calm. Each regime gives its days, its share of the labeled days and, when it has at least 63 days, the strategy's and the benchmark's return per year and the excess (shorter regimes carry no yearly figures, never zero). The rule uses no bar after the day it labels (a test removes later bars and checks the labels do not move). The labels describe the past and predict no future regime; `regime_note` says so, and `regime_unlabeled_days` counts replayed days without enough benchmark history.

**Counts and warnings.** `trades` counts buys and sells of single holdings (the first rebalance buys its whole portfolio). The warning `few_trades` appears below 30 trades. `concentration` gives the total return over the rebalance periods and the return with the three best periods left out; `few_periods_drive_result` appears when the total is positive and the rest is zero or negative. Batch and settings checks add a multiple-testing note once three or more results are compared: the best of many tries looks better than it is likely to be on new data.

**Walk-forward.** There is no walk-forward test, on purpose: it means fitting settings on one window and scoring them on the next, and these factor strategies have no fitted parameters (formulas, holdings count and rebalance interval are fixed in advance), so a split would only repeat the consecutive parts. Picking a strategy after seeing results is the look-ahead walk-forward exists to catch, and nothing here does that for you; `walk_forward_note` says this. The Strategies page keeps its separate four-window view of fixed rules, which has the same limit. Stress windows, regimes, the trade count and the best-periods figure appear under the chart, in the JSON export (the full result), in the CSV export of a single run (as extra tables, missing figures empty) and in the facts sheet of a saved-run summary, so a summary can only cite figures recorded there.

**Across markets and periods.** `GET /api/market/backtest/batch?strategy=…&universes=a,b&periods=3,5,10` runs the same strategy once per universe and period (`symbols=` instead of `universes=` tests a custom list). A period is `3`, `5` or `10` years, or a UTC range such as `2019-01-01..2022-12-31` (an open end means today). The same `top`, `rebalance`, `cost_bps` and `benchmark` apply to every cell. At most 4 universes, 6 periods and 12 backtests per call; each cell is an ordinary backtest, cached like one. The response lists each cell's metrics, notice and warnings (a failed cell keeps its place with its error, and curves are left out), and a summary: minimum, median and maximum of return, excess return, worst fall and Sharpe over the cells that completed, how many beat the benchmark or lost money, and the weakest and strongest cell. The summary also states its limits: periods that end today overlap, so cells are not independent samples; every universe is today's list, so survivorship bias applies; and fewer than six completed cells is too few to say much. In the app this is the *Test across markets and periods* section under a backtest.

**Sensitivity.** `GET /api/market/backtest/sensitivity?universe=…&strategy=…&years=5&costs=0,10,25,50&tops=5,10,15` changes one setting at a time around the backtest you asked for: trading cost (`cost_bps`), number of holdings (`top`) and the other rebalance interval. It returns the base run and each variant with its metrics, then a summary: the spread of return and excess return, how many variants kept a positive excess return, and the lowest tested cost at which the excess return was zero or negative. Holdings counts the universe cannot fill are skipped and said so. Combinations of changes are not tested, and every variant shares the same period, universe and survivorship bias; the notes in the response say so. Up to 6 values per setting; each variant is an ordinary cached backtest. In the app it is the *How much do the settings matter?* block under *Test across markets and periods*.

**Monte Carlo.** `GET /api/market/backtest/montecarlo?…` takes the same query as the backtest plus `method=block|bootstrap|shuffle` (default `block`), `runs` (100–20000, default 2000), `seed` (default 42), `horizon` (trading days per path, default the replayed days), `block_days` (default 21), `drawdown_limit` (a negative percentage such as `-20`), `extra_cost_bps` (a yearly drag, 0–1000), `slippage_bps` (per side, 0–100, converted to a yearly drag at the replay's average turnover and rebalance interval) and `volatility_scale` (0.5–3, default 1). It resamples the replay's daily returns (`internal/montecarlo`), with a seeded PCG generator, into simulated paths and returns percentile bands (5th, 25th, median, 75th, 95th) of the total return and of the worst fall from a peak, the share of paths that ended below the start, breached the limit or lost everything, the assumptions and the limitations. The deterministic backtest figures come back unchanged as `backtest_metrics`, apart from `simulation`, along with the data dates and the time the backtest was produced. Same query and seed give the same numbers while the cached backtest is the same. The `shuffle` method needs the horizon to equal the replayed days. Fewer than 126 daily returns, returns that are all equal, or a value that is not a real return are refused with 400 instead of simulated. Resampling does not predict returns: only days that happened can be drawn, the backtest's own biases carry over, and the response says so. In the app it is the *How wide was the range of outcomes?* block. `model_version` (`mc-1`) changes whenever the resampling does. Trade-level resampling of a Strategies-page replay is described below.

**Strategy Monte Carlo.** `POST /api/strategies/montecarlo` takes the body of `POST /api/strategies/backtest` plus `method=trade_bootstrap|trade_shuffle`, `runs` (100–20000), `trades` (per path, default the closed trades observed; shuffle needs exactly that), `seed`, `drawdown_limit`, `extra_cost_bps` (0–500 per trade), `dispersion_scale` (0.5–3) and three further stresses: `delay_bars` (0–5), `regime_share_pct` with `regime_scale` (a share of each path's trades, at a random place, whose distance from the average trade is multiplied by 1–4) and `position_scale` (0.25–3, a multiple of every trade's change in the account value). It replays the strategy once and resamples the account-value change of its closed trades (`internal/montecarlo/trades.go`); an open trade is left out. The delay stress adds zero-mean noise with a spread of daily volatility (measured on the replay's close curve) times the square root of the delay times the median share of the account in a trade; `simulation.config` echoes both measured inputs. Fewer than 20 closed trades, all-equal trades or a stress outside its range are refused. `observed` and `simulation.observed` are the replay, unchanged; `simulation.concentration` is measured on the observed trades (return without the best three, worst single trade). Same body and seed give the same numbers while the price data is the same. The Strategies page can export the shown result as CSV or JSON with provider, UTC data dates, model version, seed and assumptions; it is not stored on the server.

**Saved results.** A backtest, a markets-and-periods batch, a settings check or a simulation can be saved by name (30 per user). `POST /api/market/backtest/runs {name, kind: single|batch|sensitivity|montecarlo, query}` runs the query on the server, from the cache when it is fresh, and stores the server's own answer; a result sent by the client is refused, so a saved run is always something the server produced. `GET /api/market/backtest/runs` lists them, `GET …/runs/{id}` returns the query and result, `DELETE …/runs/{id}` removes one, and `GET …/runs/{id}/export` downloads JSON (the query, the UTC save time, a note that re-running later may differ, and the full result with its assumptions, data coverage and warnings) or, with `?format=csv`, a table with one row per cell or variant (missing figures are empty, never zero). Saved runs belong to their owner and are deleted with the account. `GET …/runs/{id}` and the JSON export also carry a `provenance` fact (the shared contract, [ADR 0003](adr/0003-provenance-contract.md)): calculated by the method `backtest.single`, `backtest.batch` or `backtest.sensitivity` with a version and a plain-language formula, the time the server produced the answer, the provider and the date of the last price when the result records them, and the standing caveats (no dividends, today's universe). Only a single backtest records its provider; a markets-and-periods or settings check does not, and a result without a production time has none either, so those facts are `partial` and say what is missing rather than naming it. A result that cannot be read gives an `unknown` fact. Opening a saved run shows a note under the saved list with the fact as a source popover; running anything new removes the note. The batch and sensitivity endpoints and these routes are open to ordinary signed-in users, like the backtest itself.

**Summary of a saved run.** `POST /api/market/backtest/runs/{id}/summary` asks the configured model to summarize a saved run (where it worked, where it failed, risk, sample size, limitations, what to test next). The server writes the facts sheet from the stored result, so the client supplies no numbers, and tells the model to copy figures exactly. Every figure in the text is then checked against the sheet with the same mechanical check as flow agents (`grounding.Check`); on a mismatch the model gets one correction, and if the second text fails too, no summary is shown and the response carries the facts sheet with the reason. The check finds invented or altered figures. It does not judge the wording or whether a claim follows from the numbers. The server itself adds the notes that must not depend on the model: research, not investment advice, and a thin-sample warning when fewer than 12 rebalances, cells or variants sit behind the figures. The citation is the run id and its recorded figures. Summaries are not stored.

**Method.** The first rebalance is the first trading day on or after the start
date once `top` symbols have a year of prices. Every 21 trading days (63 for
quarterly) the leaderboard scores the universe exactly as it does today, the
best `top` companies are held at equal weight, and weights then drift with
prices until the next rebalance. Each trade costs `cost_bps` per side (default
10) plus `slippage_bps` per side (0 to 100, default 0), the gap between the closing price and the fill. Both are charged on the traded share of the portfolio, so a strategy that trades more pays more; the first buy-in from cash pays on the whole portfolio. With no slippage the result carries the warning `slippage_not_modeled`. Slippage is a flat assumption, not an estimate from spreads or volume, which this app does not have. Optional execution realism (all off by default): `spread_bps` (0 to 200, the quoted spread as a whole; half is charged per side), `notional` (a portfolio value in the price currency, up to 1e12; when set, each order is sized against the symbol's average daily traded value over the 20 bars before the fill, never including the fill day), `max_participation_pct` (1 to 100, default 10: the largest share of that value one order may take; the rest is left unfilled) and `impact_bps` (0 to 500; per side, impact bps times the square root of the order's share of the average daily traded value, used only with `notional`). Sells fill first and buys use only the cash they free. The response adds `liquidity` (orders, partial orders, orders without a volume estimate, the largest participation, the unfilled share, and the spread and impact drag in basis points), `unfilled_pct` per rebalance and the warnings `partial_fills` and `volume_unknown`; a symbol with no volume fills in full. Not modeled even then: taxes, dividends, queue position and intraday liquidity. The Strategies replay accepts `spread_bps` too and returns `stress`, `stress_not_covered`, `regimes` and `regime_unlabeled_days` measured against the instrument's own buy and hold. The benchmark is held in full with no costs: SPY for the US universes,
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

### Reproducible, dated and humble rankings

Every ranking (`GET /api/market/leaderboard`, the `get_leaderboard` agent tool) returns a `provenance` block, and every row says how far to trust it:

| Field | Meaning |
| --- | --- |
| `provenance.methodology` | `version` and `hash` of the factors, metrics, strategy weights, caps and the below-median value for missing factors. Any change to the rules changes the hash. |
| `provenance.methodology` details | Also the plain-language description the Leaderboard panel shows above the table: `factor_info` (what each factor asks), `metrics` per factor (`label`, `note`, direction, weight), `caps`, `scale` and `missing_data`. |
| `provenance.universe_hash` | Hash of the symbol list that was ranked. |
| `provenance.inputs_hash` | Hash of the metric rows the scores came from. Same methodology, universe and inputs hash give the same scores. |
| `provenance.price_dates` | Newest and oldest price date in the data. |
| `provenance.disclaimer` | "Research suggestion, not personalized financial advice." Shown under the table. |
| row `breakdown` | Only with `?explain=1`. Per factor: `weight`, `score`, `contribution` (score x weight; the contributions add up to the row score), `missing`, and `capped_from` with `cap_reason` when a cap applied; per metric: `value`, `percentile`, `weight`, direction. The Markets page always asks for it. |
| `universe_info` | The list that was ranked, without its symbols: `label`, `description`, `region`, `countries` and `source` (`kind` of `curated`, `disclosure` or `custom`; `name`; `url` and `as_of`, both `null` when not verified; `rule` for how companies were chosen; `limitations`). |
| `selection` | From list to ranking: `initial`, `ranked`, every company left out with its reason, `steps`, and counts of ranked companies per `countries` and `exchanges`. Initial minus excluded equals ranked. |
| row `discovery`, `metrics_source`, `listing` | Three separate answers: why the company is on the list (and the list's source), where its numbers come from (provider, price date, fundamentals through), and which listing it is (exchange, MIC and country read from the ticker suffix; a bare US ticker says the exchange is not recorded). |
| `provenance.fact`, `provenance.universe_fact` | The ranking (calculated, with method id, version, provider, price date and the time it was produced) and its list (assumed for a hand-picked or typed list, observed for a disclosure) in the shared [provenance format](adr/0003-provenance-contract.md), shown with `fs-source`. A list with no recorded review date is `partial`. |
| `provenance.data_age` | Whether the ranking mixes data ages, stated in words (`statements`): how many prices are older than the newest and the span in days, stale-price flags, the range of reported-quarter dates, companies without a reported quarter, an old newest price (more than 5 days at the time of the request) and a list whose review date is missing, over 180 days old or only a period. Missing is never counted as zero. When `mixed` is true, or the newest price or the list is old, the Leaderboard shows the statements as a warning above the table. |
| `provenance.bias` | Selection and survivorship statements with the numbers behind them in `measures`: that the list is today's list and not point-in-time (`point_in_time` is always `false`; no point-in-time lists exist), how many listed companies had no usable data, the median 12-month price change of the top against the whole ranked list when at least 6 companies have one (in-sample, backward looking), and market concentration. These describe the data; they do not correct it. |
| row `metrics_source.fact`, `metrics_source.record_url` | The numbers' origin as a provenance fact (observed, provider, price date). `record_url` is the provider's own page for the symbol only where the address follows from the provider and the symbol alone (today: Yahoo, `https://finance.yahoo.com/quote/<symbol>`); otherwise it is `null` and the row says no link is recorded. No exchange or ISIN link is made up. |
| row `confidence`, `confidence_note` | `high`, `medium` or `low` from factor coverage, universe size and input age, with the reason. |
| row `fundamentals_age_days` | Age of the newest reported quarter at the price date (never the clock). Over 200 days adds the `stale_fundamentals` flag. |
| row `alternatives` | Up to three competing explanations: a trend that may already be priced in, overbought entry, negative earnings, old fundamentals, and heavily weighted factors that score low or have no data. |

#### Your objective and constraints

Each user can save an objective and constraints for the Markets leaderboard (`GET|PUT /api/market/leaderboard/preferences`, stored per user in `ranking_preferences`; they belong to the user, not to the system). They are applied to `GET /api/market/leaderboard` for the signed-in user unless the request has `preferences=off`; flow nodes and the `get_leaderboard` agent tool never apply them, so an automation does not change with someone's settings.

- **Objective.** `preserve` (protect capital) multiplies the weights of value by 1.2, quality 1.3, growth 0.6, momentum 0.6 and low risk 2.0; `growth` multiplies value by 0.8, growth 1.4, momentum 1.2 and low risk 0.6. The weights are then made to sum to 1 again (four decimals). No objective or `balanced` leaves the strategy as it is.
- **Constraints.** A volatility limit (percent a year), leaving out companies with non-positive trailing earnings, requiring reported fundamentals, and a never-list of symbols. They remove companies after scoring, so scores keep meaning "better than most of this list"; ranks are renumbered over what remains. Every company removed appears in `selection.excluded` as "Removed by your constraints: ..." and as a step in the pipeline. A volatility limit cannot be checked for a company with no volatility figure, so that company is removed and the reason says it was unknown.
- **Reproducible.** The preferences, in canonical form, are part of `provenance.methodology` and its `hash`, and the response lists `preferences.effective_weights`. The same preferences and inputs give the same ranking; a ranking made with preferences never has the fingerprint of one made without.

The curated lists live in `internal/server/universes.json`. They are hand-picked, say so, and state no review date or link because none has been verified; they are not index constituents. Reference-portfolio lists name the filer, form, date and SEC link of the disclosure they come from.

The methodology version (`leaderboardMethodologyVersion`) must be bumped when the scoring rules change. Out-of-sample splits and a survivorship-free universe are not provided; the backtest states those biases.
