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

Choosing an index shows its provider, methodology, constituent source, as-of date (UTC), number of securities, exclusions, share-class and currency notes, licensing limits and whether past membership is known.

**FloweySales ships no list of index members.** Index lists are licensed by their providers, so an index is **unavailable** until you supply the list yourself, and then it is available to you only. Open an index and use **Your data** to do one of two things:

- **Import a CSV file.** Columns: `symbol` (required), `name`, `weight` (percent), `exchange` and `isin`. An ISIN is optional but recommended: it keeps share classes and look-alike tickers apart when indices are compared or combined (a malformed ISIN is refused). Leave a weight blank when unknown; it is kept blank, never turned into 0. Swedish tickers are written `VOLV-B.ST`, or as `VOLV-B` with `ST` in the exchange column. You also give the file's as-of date (UTC), the source of the file, and the licence or terms that let you use it, and you confirm that you may. A file with duplicate rows, a malformed symbol, a symbol from the wrong exchange, or a row count far from the index's size (for example 5 rows for the S&P 500) is refused with one line per problem, and nothing is stored.
- **Fetch it with your own Financial Modeling Prep key** (S&P 500 and Nasdaq-100 only). Paste your key once; it is stored encrypted for your account and never shown again. FMP's terms and prices are set by FMP: check that your plan includes constituent data before you use it. For the S&P 500 the list also carries FMP's dated add and remove records, which gives **past membership**. The Nasdaq-100 fetch has not been checked against a live response and is labeled unverified. OMX Stockholm 30 can only be imported from a CSV file: no data service is supported for it.

- **Add a history file** (optional, with a CSV import). Columns `date` (YYYY-MM-DD), `added` and `removed` (either may be blank): one row per membership change, no later than the list's as-of date. With it, past membership is known from the earliest row and backtests rank the members of each date. Rows that contradict today's list are accepted with a warning.

Having a key or a file does **not** by itself give you the right to display, store or share index data: your agreement with the provider decides that, and FloweySales assumes nothing. Your list is stored for your account only.

**Verified or approximate.** Every list is checked automatically: you confirmed the licence, the size is close to the index's name (share classes explain a small gap), the list is not older than 100 days, the source format is documented, and any weights add up to about 100. A list that passes everything is labeled **verified**; otherwise it is **approximate** and each failed check is shown. Verified does not mean endorsed by the index provider: the source is the one you attested.

**Compare and combine.** *Compare indices* shows, for two to six of your lists, their size, currency, date, benchmark and verification, how many securities each pair shares, and the differences that make a comparison misleading (currencies are never converted, price versus total-return benchmarks, different list dates, survivorship). Securities are matched by ISIN, otherwise by exchange and ticker, never by ticker alone. *Combined* ranks the union of the lists you choose, each security once; every index you choose needs your own list (none is substituted), and a combined universe cannot be backtested.

Once a list is in place the index shows the provider, the as-of date, how many securities it holds, what you confirmed, and whether past membership is known. A list more than 100 days old is flagged as stale. You can remove your list at any time and the index becomes unavailable again.

**Backtests on an index.** When past membership is known (an FMP list with add and remove records), each rebalance ranks the companies that were members on that date. Dates before the records begin, and every date for a CSV list, use today's members and are labeled as survivorship-biased. Companies that left the index often have no price history from your market-data provider; those are skipped, named, and the survivorship warning stays. The hand-picked Swedish and US lists are separate, and say they are not official indices.

## Coverage by region

Open **Coverage by region** to see which of the six world regions have a list. A region is listed as covered only when a list is offered and its prices and reports have been checked. Asia, Africa, South America and Oceania show "No supported market yet". Nothing is filled in for them. A maintainer can run `go run ./cmd/universe-coverage` to check candidate lists against a provider before any region is added (see the repository's `docs/universe-coverage.md`).

## Global scope, filters and comparison

**Global** (the first list) is the combination of every hand-picked list that has a reviewed source. It reaches North America and Europe and nothing else: the other four regions have no checked list, so they are shown as "No supported market yet" and nothing stands in for them. A company on two lists appears once; the same company code on two exchanges (for example AZN.ST and AZN.L) stays two separate securities.

Open **Scope** to narrow any list by region, country (two-letter code), exchange (market identifier code such as XSTO) and currency (such as SEK). A company outside the scope is not hidden: it is counted in "From list to ranking" as "Outside your filters" and named under "Left out" with the reason. A ticker that does not say where it trades cannot pass a region, country or exchange filter. Sector and liquidity filters are not offered, because the data provider returns neither.

**What a score is compared with** is your choice. *Within each region* (the default for Global) takes percentiles among companies of the same region, so a Swedish company is judged against European companies and a US one against North American ones; the merged table is ordered by score, but scores of different regions were not measured against each other. *Across all companies* uses one pool. A region with fewer than five ranked companies is flagged "Few peers in region": a percentile among so few means little.

**Currencies, accounting and time zones.** Prices are shown in each listing's own currency and no exchange rate is applied, because FloweySales has no rate source with a named provider and date. Ratios (P/E, margins, growth) are unaffected, but 12-month return, momentum and drawdown are in local currency, so a weak currency can flatter a company; **Across all companies** with several currencies warns about this. The accounting standard is not recorded per company, so each row shows the date its trailing figures run through. Exchanges close at different hours, so rows dated the same day can be hours apart; dates are UTC calendar days. The "Coverage and comparability" panel lists, per region, how many companies were listed and ranked, the price dates, the currencies, exchanges and the provider.

Global cannot be backtested: one benchmark and one currency do not fit a mixed set. Backtest one list at a time.

## Reading the table

Each row shows rank, company with a grade, the score, six small factor bars, the price, P/E, revenue growth year on year, 12-1 month momentum, distance from the 52-week high, RSI and a **Why it ranks** column.

- **Score (0-100)** is a percentile-based number *within the chosen list*. It says "better than most of these", not "will rise".
- **Grade** is a coarser reading of the score: A at 75 or more, B from 62, C from 48, D from 35, E below that.
- **Factors:** Value, Quality, Growth, Momentum, Entry timing and Low risk. Hover over a bar to see its number; a flat bar means no data.
- **Why it ranks** lists the biggest contributors, warning flags (for example Overbought, Below 200-day, Death cross, Unprofitable, Shrinking revenue, No fundamentals, Stale price, Old fundamentals), a **confidence** level with a note, and **Against**, the strongest competing explanations for the rank.
- Under **Score breakdown**, "Why it is here" also states how the company was matched (by exact symbol on a named list and its version), its ISIN (or "not recorded": an ISIN is shown only when it has been verified), and which region it was compared with. When a second provider filled in or disagreed with the primary one, "Where its numbers come from" says so.
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
