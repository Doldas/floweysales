---
title: Strategies and backtests
order: 5
summary: Test rule strategies on one instrument, backtest leaderboard strategies, check how settings and periods change the result, and save and export runs.
last-reviewed: 2026-11-04
---

# Strategies and backtests

FloweySales has two kinds of test, in two places. Both replay the past. Neither forecasts, and neither places an order.

| | Where | What is tested | Data |
| --- | --- | --- | --- |
| **Rule strategies** | Strategies page | Entry and exit rules on **one instrument**, such as a Fibonacci pullback | Daily prices |
| **Leaderboard backtests** | Markets page, under the leaderboard ("Backtest this strategy") | A [leaderboard](leaderboard.md) strategy applied to a **list of companies**, with a monthly or quarterly rebalance, against an index | Daily prices and reported quarters |

Past results do not predict future ones. Costs, taxes and slippage in real life differ from what is assumed here.

## Rule strategies (Strategies page)

### Pick or build a strategy

The list on the left groups the ready-made strategies: a **Fibonacci** group, one group per fictional investor character, and **My strategies**. Selecting a ready-made strategy shows its rules in words. Use **Customize a copy** to edit a copy. **New strategy** starts from a blank one.

The builder has:

- a name, a direction (long or short) and notes;
- **entry conditions**, all of which must hold, and optional **exit conditions**, any of which ends the trade early (up to eight of each);
- an optional **stop loss** and **target**, each set from a price level or a Fibonacci level plus or minus a number of ATRs;
- a time stop: close after a number of days, where 0 means never;
- **position size**, either a risk percentage of equity per trade or a fixed percentage of equity;
- swing settings that decide which swings count for Fibonacci levels.

The "In words" section reads the whole strategy back. **Save strategy**, **Save as new** and **Delete** manage your own strategies.

### Run a test

1. Choose an instrument with the search box ("Test on a company or index").
2. Choose the history: 1Y, 3Y, 5Y, 10Y, YTD, MAX, or **Custom** with a first and last date in UTC. You can give a custom period a name and save it for later in this browser.
3. Set the **cost per side** in basis points. The default is 10; the field accepts 0 to 200.
4. Press **Run test**.

### Read the result

- A line states the period, the data provider and the cost used, followed by a line on what the latest bar says: whether an entry signal is present and whether the strategy would be in a position.
- Tiles show Strategy return, Buy and hold, Worst fall, Trades, Win rate, Profit factor, Average R and Time in market.
- The price chart marks entries and exits and draws the Fibonacci levels of the latest swing. A second chart compares the strategy's equity with buy and hold. Both charts start at 100.
- **Named stress windows** measure the strategy and the benchmark over three fixed periods, only when the replay fully covers them: the 2008 crisis (2007-10-09 to 2009-03-09), the pandemic crash (2020-02-19 to 2020-03-23) and the 2022 rate-rise bear market (2022-01-03 to 2022-10-12). Each shows total return (not per year), the benchmark's return, the difference and the worst drop. Windows the replay does not cover are listed as not covered; a backtest fetches at most 10 years of prices, so the 2008 window is currently always listed that way. The windows were picked with hindsight.
- **Results by market regime** (with a benchmark) labels each day as uptrend or downtrend (the benchmark's close against its own 200-day average) and calm or volatile (63-day volatility against a fixed 20% a year), using only data up to the previous close. For each regime it shows the number of days, the share of the period and the return per year of the strategy, the benchmark and the difference. A regime shorter than 63 days gets no yearly figures. The labels describe the past; they predict nothing.
- **Trade count and best periods:** the number of buys and sells, and the return with the three best rebalance periods removed. Fewer than 30 trades, or a positive result that disappears without a few periods, adds a warning.
- **Read this before trusting the result** lists the warnings that apply to this run.
- **Consistency across the period** cuts the history into four consecutive windows and scores the trades by when they were entered. The rules are the same in each window. A result that holds in only one window leans on that stretch.
- **Show trades** lists every trade with entry, exit, reason, days held, return and R.
- **Assess** sends the result to the [Research Agent](agents.md) for a written view. That text is AI-generated.

## Leaderboard backtests

Open the leaderboard on the Markets page, choose a list and a strategy, and press **Backtest this strategy**.

### How the replay works

Each month (or each quarter) the strategy ranks the list as it would have looked on that day and holds the top companies with equal weights. A reported quarter is used only after it would have been public, with a default 60-day lag after the period end. Trading cost is 10 basis points per side by default. The benchmark is the list's index: SPY for the US lists, ^OMX for Sweden and ^STOXX50E for the European list. It is a price index without dividends.

Controls: **Top 5, 10 or 15** holdings, **monthly or quarterly** rebalancing, and a period of **3Y, 5Y, 10Y** or **Custom** dates (UTC). The first run of a list takes a while; repeats are quick.

### Reading the result

- Tiles: return per year after costs, return versus the benchmark, worst fall (largest peak-to-trough drop), Sharpe (with a risk-free rate of 0), the share of rebalance periods that beat the benchmark, and turnover. Unknown values show a dash.
- The chart compares the strategy with the benchmark, both starting at 100.
- **Consecutive parts of the period** cuts the period into three equal parts and measures each on its own: return per year, benchmark, the difference and worst drop. Nothing is fitted on one part and tested on another, so this shows whether the result holds up over time, not out-of-sample skill. It appears only when each part is at least about half a trading year; otherwise it is left out.
- **Read this before trusting the result** lists the stated biases, for example survivorship bias (the list is today's, so companies that dropped out are missing), a benchmark without dividends, few reported quarters in the early years, or fewer than 24 rebalances.
- **Show rebalances** lists the holdings picked at each rebalance, the turnover and the share of holdings with fundamentals.

### Walk-forward testing

True walk-forward testing (fit on one period, test on the next) is not offered for these strategies. The factor rankings have no fitted parameters, so there is nothing to fit, and a split would only repeat the consecutive parts under another name. The result says so.

### Test across markets and periods

Under **Test across markets and periods**, tick up to four lists and any of 3, 5 and 10 years, then run. At most 12 backtests run at once. The result is a table by list and period, with the spread (smallest, median and largest) of each figure and a list of limitations. Failed cells show their error and are left out of the spread. While the run is going, the panel shows how many backtests have finished (for example "Backtesting · 3 of 12"). With three or more results the limitations also warn that comparing many variants and choosing the best one is a way to fool yourself.

### How much do the settings matter?

**Run sensitivity** changes one setting at a time around the current backtest: trading cost (0, 10, 25 and 50 basis points), number of holdings (5, 10 and 15) and rebalance interval. It reports how wide the return range is and, when there is one, the lowest cost at which the excess return disappears. It needs a 3Y, 5Y or 10Y period, not custom dates.

## Saved runs

Saving stores the question and the server's answer, so you can reopen it, summarize it or download it later. You can keep up to 30 saved runs; name each one (up to 80 characters).

- **Save backtest**, **Save markets and periods** and **Save settings check** save the current single backtest, batch or sensitivity result.
- **Open** shows a saved result again exactly as the server recorded it, with a source badge that tells where it came from: the method and its version, when the result was produced, the data provider and the last price date where they were recorded. Re-running the same question later can give a different result, because prices and reports change.
- **Summarize** asks the model for a short written summary. The server builds a fact sheet from the stored result and checks that every figure in the summary appears in it. If the check fails twice, you get the fact sheet and a note instead of an unchecked text. The check is mechanical and does not judge the wording. Short samples get a note that the evidence is thin. A summary needs an AI model to be configured.
- **CSV** and **JSON** download the run. JSON holds the parameters, the provenance and the result. CSV flattens it to a table with one row per backtest, market and period, or setting, with empty cells where a figure is unknown. Dates are UTC.
- **Delete** removes a saved run.

## Limits

- Backtests use daily prices. They do not model intraday moves, taxes, dividends in the benchmark or market impact.
- The lists are hand-picked and today's, so results flatter the past.
- Settings were chosen by someone who had seen the past. Segments and settings checks show robustness. They are not proof of future skill.
- Strategies built from rules run on one instrument at a time.
