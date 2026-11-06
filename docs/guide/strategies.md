---
title: Strategies and backtests
order: 5
summary: Test rule strategies on one instrument, backtest leaderboard strategies, check how settings and periods change the result, and save and export runs.
last-reviewed: 2026-11-06
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
3. Set the **cost per side** in basis points. The default is 10; the field accepts 0 to 200. Set **slippage per side** in basis points too: it is the gap between the price the rules assume and the price of the fill, always against the trade (a buy fills higher, a sell lower, and a stop that gaps fills at the gap price). It is charged on top of the cost, the default is 0 and the field accepts 0 to 200. With 0 the result warns that every fill is assumed at the exact price.
4. Press **Run test**.

### Read the result

- A line states the period, the data provider and the cost used, followed by a line on what the latest bar says: whether an entry signal is present and whether the strategy would be in a position.
- Tiles show Strategy return, Buy and hold, Worst fall, Trades, Win rate, Profit factor, Average R and Time in market.
- The price chart marks entries and exits and draws the Fibonacci levels of the latest swing. A second chart compares the strategy's equity with buy and hold. Both charts start at 100.
- **Named stress windows** measure the strategy and the benchmark over three fixed periods, only when the replay fully covers them: the 2008 crisis (2007-10-09 to 2009-03-09), the pandemic crash (2020-02-19 to 2020-03-23) and the 2022 rate-rise bear market (2022-01-03 to 2022-10-12). Each shows total return (not per year), the benchmark's return, the difference and the worst drop. Windows the replay does not cover are listed as not covered. A backtest asks the provider for its longest history when the period starts more than nine years back (see "Longer history" below), so the 2008 window is covered only when the provider returns daily prices that far. The windows were picked with hindsight.
- **Results by market regime** (with a benchmark) labels each day as uptrend or downtrend (the benchmark's close against its own 200-day average) and calm or volatile (63-day volatility against a fixed 20% a year), using only data up to the previous close. For each regime it shows the number of days, the share of the period and the return per year of the strategy, the benchmark and the difference. A regime shorter than 63 days gets no yearly figures. The labels describe the past; they predict nothing.
- **Trade count and best periods:** the number of buys and sells, and the return with the three best rebalance periods removed. Fewer than 30 trades, or a positive result that disappears without a few periods, adds a warning.
- **Read this before trusting the result** lists the warnings that apply to this run.
- **Consistency across the period** cuts the history into four consecutive windows and scores the trades by when they were entered. The rules are the same in each window. A result that holds in only one window leans on that stretch.
- **How robust is this result?** redraws the closed trades of the replay into many simulated sequences; see [Trade-level robustness](#trade-level-robustness). It is separate from the replay and never changes it.
- **Walk-forward fit** is the one test here that fits anything. See "Walk-forward fit" below.
- **Show trades** lists every trade with entry, exit, reason, days held, return and R.

### Walk-forward fit

A strategy you build has numbers in it: the period of an average, a constant in a condition, a Fibonacci ratio, an ATR offset of a stop or target, a time stop. The fit chooses those numbers on one stretch of history and measures the choice on the next, which the choice never saw.

1. Run a test. The **Walk-forward fit** section lists the numbers of that strategy that can change. Numbers that are zero are not offered because the grid scales the current value.
2. Tick one or two numbers.
3. Choose the **folds** (2 to 8 test windows), the **training window** (*rolling* trains on the block just before each test window, *anchored* on everything before it) and what to **pick the best by** (total return, Sharpe ratio or expectancy per trade).
4. Press **Run walk-forward fit**.

How it works: the tested period is cut into one more equal block than there are folds, each at least 126 trading days. For fold *k* the server tries every combination of five scaled values per number (0.5, 0.75, 1, 1.25 and 1.5 times the current value, rounded and kept inside the allowed range), at most 25 combinations, on the training window only. A combination needs at least 5 trades there to qualify. The best one is then run once on the next block. The test blocks follow each other without overlapping and are compounded in time order.

The table shows, for every fold, the training and test dates, the values chosen, the training return, the test return, the return of your **original numbers** on the same test window and buy and hold. Above it, one line gives the compounded test return of the fitted rules, of your original numbers and of buy and hold, the number of test trades, the average training return against the average test return, and how much of the training return the test windows kept (shown only when the average training return is positive). A fold with too few trades or too little history is marked skipped with the reason and is left out of the totals, never shown as zero.

What it does not show: the result is not an independent test. You have seen all of this history, the original numbers were written by someone who had seen it, and anchored folds re-use earlier test windows for training. Trying many combinations and keeping the best is itself a way to fool yourself, so the result always warns about the search. It also warns when the test trades are fewer than 30, when the best values change from fold to fold, when fitting did worse than your original numbers, and when the training windows were clearly better than the test windows. The same slippage and cost settings apply to every fold. A fit uses one instrument and one provider and places no order.
- **Assess** sends the result to the [Research Agent](agents.md) for a written view. That text is AI-generated.

### Trade-level robustness

Under the result, **How robust is this result?** resamples the trades of the replay with a seed. Each trade is the change in your account value it caused after costs (the position size is included), not the return on the traded amount. The panel is for research and paper testing only; it places and prepares no order.

- **Method.** *Bootstrap* draws trades one at a time with replacement, so one trade can appear several times in a sequence and trades are treated as independent. *Shuffle* replays the same trades in a new order: the total return is the same in every sequence and only the worst fall varies, which shows sequence risk, not return risk.
- **Runs and seed.** 100 to 20,000 sequences (2,000 by default), each as long as the number of closed trades. The same settings and seed give the same numbers while the price data is the same. Change the seed to see how much the percentiles themselves move.
- **Stress choices.** *Alert at a fall of* gives the share of sequences that fell that far at some point. *Extra cost* takes a flat number of basis points of the account from every trade, for example to ask what a slower fill or slippage would do. *Spread of trades* widens each trade's distance from the average trade. These are assumptions you pick, listed under "What this assumes", not things that were observed.
- **Sensitivities.** *Fill delay* (0 to 5 bars) adds to every trade a random difference of zero average, sized from the instrument's daily volatility in the replay, the square root of the delay and the usual share of the account in a trade; it widens the outcomes and does not claim that delays cost or earn money on average. *Turbulent stretch* picks a share of the trades in each sequence, at a random place, and widens their distance from the average trade by the chosen factor (1.5 to 4 times), to mimic a volatile period. *Position size* multiplies every trade's change in the account value (0.25 to 3 times), a stand-in for more concentrated exposure; a trade can still lose at most the whole account. All three are stresses you choose and are listed as such.
- **Concentration.** Below the table the panel shows, from the trades that happened and not simulated, the replay's return without its three best trades and its single worst trade.
- **Export.** *Export CSV* and *Export JSON* download the shown simulation with the strategy, instrument, provider, data dates (UTC), time generated (UTC), model version, seed, settings, assumptions and limitations. The file is a copy of what was on screen; it is not stored in your account.
- **Reading the table.** The "Happened" column is the replay; the 5th to 95th columns are percentiles of simulated sequences. Figures are per number of trades, not per year. The worst fall is measured at closed trades, so a dip inside a trade is not counted and the real fall can be deeper.
- **When it refuses.** Fewer than 20 closed trades, or trades that all have the same result, are not simulated; the panel says how many trades there are instead of showing percentiles that rest on nothing. A trade still open at the last bar is left out.

Partial fills, liquidity and the market impact of larger positions are not modeled, and the delay, turbulent-stretch and position-size settings are blunt stresses on trade results, not a re-run of the rules on other prices. Only trades that happened can be drawn, so a loss worse than the worst trade cannot appear, and the biases of the replay (one instrument, one stretch of history, rules that may have been tuned on it) carry over. The result lists the instrument, provider, data dates in UTC, strategy, cost, model version and seed it used. Nothing here is a forecast or a promise of return.

## Leaderboard backtests

Open the leaderboard on the Markets page, choose a list and a strategy, and press **Backtest this strategy**.

### How the replay works

Each month (or each quarter) the strategy ranks the list as it would have looked on that day and holds the top companies with equal weights. A reported quarter is used only after it would have been public, with a default 60-day lag after the period end. Trading cost is 10 basis points per side by default. The **Slippage** buttons add 5, 10 or 25 basis points per side on top, charged on every trade, to stand for fills worse than the closing price; with *No slippage* the result warns that it assumes closing-price fills and is an upper bound. The batch and the settings check use the slippage you picked. The benchmark is the list's index: SPY for the US lists, ^OMX for Sweden and ^STOXX50E for the European list. It is a price series without dividends.

The **Fill delay** buttons decide when a trade fills. With *Same-day fill* the ranking is computed from a day's close and the trades fill at that same close, which no one can do exactly, and the result warns about it. With a delay of 1 or 2 trading days the ranking still sees only the data up to its own day, and the trades fill at the close that many trading days later. Most of the difference between the two is the move that happened in between. A result that disappears with a day of delay was not tradable as measured.

Controls: **Top 5, 10 or 15** holdings, **monthly or quarterly** rebalancing, the slippage and fill delay above, and a period of **3Y, 5Y, 10Y** or **Custom** dates (UTC).

### Longer history

When a period starts more than nine years back (one year of prices is needed before the first pick), the backtest asks the provider for its longest history instead of 10 years. It uses that answer only when the bars are daily. A provider that answers with weekly or monthly bars, or with too few bars, is not used for the long window: the 10-year window is fetched instead, the result says that longer history was requested but not served, and the periods before the first bar are simply not tested. Nothing is filled in. How far back it goes depends on the provider and the symbol, and the data start shown with the result is what was actually returned. The first run of a list takes a while; repeats are quick.

### Reading the result

- Tiles: return per year after costs, return versus the benchmark, worst fall (largest peak-to-trough drop), Sharpe (with a risk-free rate of 0), the share of rebalance periods that beat the benchmark, and turnover. Unknown values show a dash.
- The chart compares the strategy with the benchmark, both starting at 100.
- **Consecutive parts of the period** cuts the period into three equal parts and measures each on its own: return per year, benchmark, the difference and worst drop. Nothing is fitted on one part and tested on another, so this shows whether the result holds up over time, not out-of-sample skill. It appears only when each part is at least about half a trading year; otherwise it is left out.
- **How fills were assumed** (a collapsed section) lists, for the settings the run used, the fill price, the cost, the slippage, the weights, the report lag and what is *not modeled*: bid-ask spread, order size against traded volume, partial fills, market impact, dividends and taxes. It also defines the benchmark: its symbol, whether it is the list's own index or your choice, that dividends are not added, the currency the provider reported, and the dates of its bars.
- **Currencies.** Prices are never converted. When the symbols are quoted in different currencies, or the benchmark is quoted in another currency than the symbols, the result warns that the comparison ignores exchange-rate moves. When a provider does not state a currency, the result says it could not check, and does not assume one.
- **Read this before trusting the result** lists the stated biases, for example survivorship bias (the list is today's, so companies that dropped out are missing), a benchmark without dividends, few reported quarters in the early years, fewer than 24 rebalances, fills at the same close as the ranking, mixed currencies, and the execution effects that are not modeled.
- **Show rebalances** lists the holdings picked at each rebalance, the turnover and the share of holdings with fundamentals.

### Walk-forward testing

True walk-forward testing (fit on one period, test on the next) is not offered for leaderboard strategies. The factor rankings have no fitted parameters, so there is nothing to fit, and a split would only repeat the consecutive parts under another name. The result says so. Rule strategies on the Strategies page do have numbers, and they have a [walk-forward fit](#walk-forward-fit).

### Test across markets and periods

Under **Test across markets and periods**, tick up to four lists and any of 3, 5 and 10 years, then run. At most 12 backtests run at once. The result is a table by list and period, with the spread (smallest, median and largest) of each figure and a list of limitations. Failed cells show their error and are left out of the spread. While the run is going, the panel shows how many backtests have finished (for example "Backtesting · 3 of 12"). With three or more results the limitations also warn that comparing many variants and choosing the best one is a way to fool yourself.

### How much do the settings matter?

**Run sensitivity** changes one setting at a time around the current backtest: trading cost (0, 10, 25 and 50 basis points), slippage (0, 10 and 25 basis points), fill delay (0, 1 and 2 trading days), number of holdings (5, 10 and 15) and rebalance interval. It reports how wide the return range is and, when there is one, the lowest cost at which the excess return disappears. It needs a 3Y, 5Y or 10Y period, not custom dates.

### How wide was the range of outcomes?

**Run simulation** redraws the days of the backtest into many simulated paths, to show how much the result depended on the order and mix of days it happened to get. It does not predict returns, and it never changes the backtest figures above it.

- **Method.** *Block bootstrap* draws runs of 21 consecutive days, which keeps some of the clustering of calm and stormy stretches. *Bootstrap* draws single days independently and tends to understate deep falls. *Shuffle* replays the same days in a new order, so the total return is the same in every path and only the worst fall varies: it shows sequence risk, not return risk.
- **Runs and seed.** 100 to 20,000 paths (2,000 by default), each as long as the replayed period. The seed (42 by default) makes a run repeatable: the same settings and seed give the same numbers while the price data is the same. Change the seed to see how much the percentiles themselves move.
- **Stress choices.** *Alert at a fall of* gives the share of paths that fell that far at some point. *Slippage* (basis points per side) is turned into a yearly cost at the turnover the replay showed; *Extra cost* takes a yearly cost directly; *Volatility* widens each day's distance from the average. These are assumptions you pick, listed under "What this assumes", not things that were observed.
- **Sensitivities.** *Fill delay* (0 to 5 bars) adds to every trade a random difference of zero average, sized from the instrument's daily volatility in the replay, the square root of the delay and the usual share of the account in a trade; it widens the outcomes and does not claim that delays cost or earn money on average. *Turbulent stretch* picks a share of the trades in each sequence, at a random place, and widens their distance from the average trade by the chosen factor (1.5 to 4 times), to mimic a volatile period. *Position size* multiplies every trade's change in the account value (0.25 to 3 times), a stand-in for more concentrated exposure; a trade can still lose at most the whole account. All three are stresses you choose and are listed as such.
- **Concentration.** Below the table the panel shows, from the trades that happened and not simulated, the replay's return without its three best trades and its single worst trade.
- **Export.** *Export CSV* and *Export JSON* download the shown simulation with the strategy, instrument, provider, data dates (UTC), time generated (UTC), model version, seed, settings, assumptions and limitations. The file is a copy of what was on screen; it is not stored in your account.
- **Reading the table.** The "Happened" column is the replay. The 5th to 95th columns are percentiles of the simulated paths: the 5th is the level that 5% of paths fell below. The text above the table says what share of paths ended in a loss, breached your limit, or lost everything.
- **When it refuses.** Fewer than 126 replayed days (about half a year), or days that all have the same return, are not simulated. The panel says why instead of showing percentiles that rest on nothing.

Only days that happened can be drawn, so a fall worse than any in the replay cannot appear, and the biases of the backtest itself (today's list of companies, approximate costs) carry over. **Save simulation** stores the settings, seed, model version, data dates and assumptions with the result.

## Saved runs

Saving stores the question and the server's answer, so you can reopen it, summarize it or download it later. You can keep up to 30 saved runs; name each one (up to 80 characters).

- **Save backtest**, **Save markets and periods**, **Save settings check** and **Save simulation** save the current single backtest, batch, sensitivity or simulation result.
- **Open** shows a saved result again exactly as the server recorded it, with a source badge that tells where it came from: the method and its version, when the result was produced, the data provider and the last price date where they were recorded. Re-running the same question later can give a different result, because prices and reports change.
- **Summarize** asks the model for a short written summary. The server builds a fact sheet from the stored result and checks that every figure in the summary appears in it. If the check fails twice, you get the fact sheet and a note instead of an unchecked text. The check is mechanical and does not judge the wording. Short samples get a note that the evidence is thin. A summary needs an AI model to be configured.
- **CSV** and **JSON** download the run. JSON holds the parameters, the provenance and the result. CSV flattens it to a table with one row per backtest, market and period, or setting, with empty cells where a figure is unknown. Dates are UTC.
- **Delete** removes a saved run.

## Limits

- Backtests use daily prices. They do not model intraday moves, taxes, dividends in the benchmark, bid-ask spread, order size against volume, partial fills or market impact. Slippage and cost are flat rates, not measured fills.
- The lists are hand-picked and today's, so results flatter the past. No point-in-time list of members (who was in the index on each date) is available, so survivorship bias is stated, not removed or measured.
- The leaderboard backtest ranks each date from data reported by then and fills at a close. The approximate report lag is a guess, not a recorded filing date.
- Contiguous parts of a period show whether a result holds over time. They are not independent out-of-sample tests, because nothing is fitted on one part and tested on another. Only the walk-forward fit separates fitting from testing, and the strategy's original numbers were still written by someone who had seen the past.
- Settings were chosen by someone who had seen the past. Segments and settings checks show robustness. They are not proof of future skill.
- Strategies built from rules run on one instrument at a time.
