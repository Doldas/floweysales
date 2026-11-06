---
title: Leaderboard
order: 4
summary: How the company leaderboard ranks a list, what the factors and grades mean, and how to inspect the method and the data.
last-reviewed: 2026-11-06
---

# Leaderboard

The leaderboard ranks a list of companies for a chosen strategy. It sits on the Markets page, under the chart, and is titled "Leaderboard: worth a look now". A high rank means a company is worth a closer look. It is not a recommendation to buy.

The ranking is calculated by the server from provider data: reported quarters, a year of daily prices and a few indicators. The screen only displays the result. Nothing in it is written by an AI model.

## Choose a list and a strategy

**The list (universe).** A row of buttons picks which companies are ranked:

| List | Companies | Benchmark used in backtests |
| --- | --- | --- |
| US mega caps | 25 large US-listed companies | SPY |
| US technology | 25 large US software, semiconductor and internet companies | SPY |
| US dividend & value | 25 established US dividend payers | SPY |
| Sweden large caps | 30 Stockholm large caps in the style of OMXS30 (not the official index) | ^OMX |
| European leaders | 25 large companies listed in seven European countries | ^STOXX50E |
| Reference portfolios | Today's publicly disclosed holdings of the real portfolio each fictional character's approach follows (offered when a disclosure is available). Ranking today's holdings has hindsight bias | SPY |
| My watchlist | Your own watchlist or trackers, at most 25 symbols | SPY |

The curated lists are picked by the maintainers by hand. They are not official indices, may be out of date and have survivorship bias; the screen says so.

**The strategy.** The chips below the list pick the weights:

| Strategy | What it favors |
| --- | --- |
| Best buy now | A balance: reasonably priced, profitable, growing companies in an uptrend that are not overextended |
| Cheap & solid | High earnings yield and low PEG, backed by margins, at a calm entry |
| Growth at a fair price | Fast, accelerating revenue and earnings growth, with value as a brake |
| Momentum leaders | The strongest 12-month and 3-month trends, confirmed by moving averages |
| Quality compounders | The highest and steadiest margins with growth and low volatility |
| Quality on a dip | Profitable companies that pulled back from their high while the long-term trend holds |
| Ashgrove, Holm and Fairbanks | Factor approximations of three fictional characters' approaches. They cannot see what a person would, such as management or moats |

## Index universes

The **Index** row offers well-known market indices: S&P 500, Nasdaq-100 and OMX Stockholm 30. An *index* is a published list of companies chosen by its provider's rules; each member is an *index constituent*, usually weighted by *market capitalization* (share price times shares outstanding). Membership is not a ranking and not a recommendation. The strategies rank whichever list you choose, so the index decides who is considered and the strategy decides the order.

Choosing an index shows its provider, methodology, constituent source, as-of date (UTC), number of securities, exclusions, share-class and currency notes, licensing limits and whether past membership is known. An index appears as **unavailable** until its constituent list has been verified as legally usable. At the moment all three are unavailable: FloweySales shows what each index is, but no members, and does not guess them. The hand-picked Swedish and US lists are separate, and say they are not official indices. Once an index is available it still holds only today's members, so a backtest on it flatters the past.

## Coverage by region

Open **Coverage by region** to see which of the six world regions have a list. A region is listed as covered only when a list is offered and its prices and reports have been checked. Asia, Africa, South America and Oceania show "No supported market yet". Nothing is filled in for them. A maintainer can run `go run ./cmd/universe-coverage` to check candidate lists against a provider before any region is added (see the repository's `docs/universe-coverage.md`).

## Reading the table

Each row shows rank, company with a grade, the score, six small factor bars, the price, P/E, revenue growth year on year, 12-1 month momentum, distance from the 52-week high, RSI and a **Why it ranks** column.

- **Score (0-100)** is a percentile-based number *within the chosen list*. It says "better than most of these", not "will rise".
- **Grade** is a coarser reading of the score: A at 75 or more, B from 62, C from 48, D from 35, E below that.
- **Factors:** Value, Quality, Growth, Momentum, Entry timing and Low risk. Hover over a bar to see its number; a flat bar means no data.
- **Why it ranks** lists the biggest contributors, warning flags (for example Overbought, Below 200-day, Death cross, Unprofitable, Shrinking revenue, No fundamentals, Stale price, Old fundamentals), a **confidence** level with a note, and **Against**, the strongest competing explanations for the rank.
- Select a row to open the company in Markets. **Score breakdown** shows every metric, its value, percentile and weight.

When the table mixes data of different ages (some prices older than the newest, companies with no reported quarter, an old newest price, or a list with no review date), a warning above the table says so and the method panel lists the counts.

Companies whose provider gave no usable prices or fewer than four reported quarters are left out, and the origin panel names each one with the reason. If no company has enough data, the screen says so and asks whether a provider plugin is installed.

## How the score is made

Open **How this ranking works** for the exact method of the version you are looking at. In short:

1. Each metric becomes a percentile from 0 to 100 among the companies that have it. Ties share their average rank, and "lower is better" metrics are flipped.
2. A factor is the weighted average of its metrics.
3. Absolute caps limit a factor when there is a clear problem: value is capped at 15 when trailing earnings per share is not positive, timing at 20 when RSI is 75 or higher, momentum at 35 when the price is below its 200-day average.
4. A factor with no data at all scores 40, below the median. Unknown is not treated as good.
5. The score is the sum of each factor times the strategy's weight.

The panel also gives a worked example, the method version and a fingerprint (hash) of the method. The footer of the table shows a fingerprint of the input data and the oldest and newest price dates. The same method, list and data give the same scores.

## Where do these companies come from?

The panel **Where do these companies come from?** answers three questions separately:

- **Why is a company on the list?** The list's source, the rule used to pick it, its limits and the date it applies to. Under each company you can see its own inclusion reason, exchange and country, and a link to an original record where one exists.
- **Where do its numbers come from?** The provider, the date of the latest price, and the quarter fundamentals run through.
- **Why does it rank where it does?** The score breakdown.

The panel also traces the list to the ranking: how many companies were on it, how many were left out for lack of data, how many were ranked, and where they trade. **Download list as CSV** or **as JSON** saves the table.

## Your objective and constraints

On the Markets page you can save an **objective** (preserve capital or grow) and **constraints** (a volatility limit, leave out companies with non-positive earnings, require fundamentals, a list of symbols never to show). The objective tilts the factor weights by documented multipliers; constraints remove companies after scoring, and each removal is named. Your choices are stored for you only, are part of the method fingerprint, and **Show without them** returns the plain ranking. Flow nodes and the agent's ranking tool never apply them.

## Bias statements

The panel states that the list is today's, not the list of an earlier date, so past rankings and backtests flatter the past. It also gives the share of listed companies dropped for lack of data, the median 12-month change of the top ranks against the whole list (labeled in-sample, with at least six companies), and market concentration. The ranking, the list and each row carry a source badge (method, provider, price date). Where the provider is Yahoo, a row links to the provider's own quote page; other providers get no link.

## What you can do from here

- **Research the top 5** sends the top five to the [Research Agent](agents.md).
- **Backtest this strategy** opens the backtest panel. See [Strategies and backtests](strategies.md).
- **Automate as a flow** copies a matching starter flow into Flow Studio. It appears for people who may create flows. See [Flow Studio](flows.md).
- **Refresh** reloads the ranking. The first load of a list takes a while; later switches are quick.

## Limits

- Scores are relative to the list. A list of weak companies still has a top rank.
- A ranking depends on the provider's data. Stale or missing data is flagged, not repaired.
- The ranking does not know your goals, taxes or risk tolerance.
- The factor view cannot judge things that need reading, such as management quality.
