---
title: Markets
order: 3
summary: The price chart, indicators, watchlist, feed status badge, fundamentals, quarterly financials and the World view.
last-reviewed: 2026-11-04
---

# Markets

Markets is where you look at one instrument at a time: its price history, key figures, indicators and fundamentals. The same page hosts the [Leaderboard](leaderboard.md). Quarterly statements have their own page, Financials, and news themes are on World.

Everything on these screens comes from the market-data provider plugins your administrator installed. Without one, the screens are empty and say so.

## Pick an instrument

Use the search box in the top bar or on the page. Results show the symbol, the name where the provider has it, the exchange and the **provider**. Press Enter to try the exact symbol you typed. Recently used instruments are listed when you focus the search box.

The selection is kept in the address of the page, so a copied link opens the same instrument.

## Watchlist and trackers

The row under the search box lists the instruments you follow, up to 16 (the counter shows `n/16`). Select one to view it; the small cross stops tracking it.

What this row is depends on your role:

- **Most users** get a **watchlist** and a research view. The key figures are the last close, the change over the chosen range, and the high and low of that range.
- **Administrators** get **active trackers** with a live feed: the latest price, the stop-loss level, fast and slow EMAs and the unrealized profit or loss of the paper portfolio. Administrators also see a button that deletes the stored tick history; it asks you to click twice.

If a figure is missing, the screen shows a dash, not zero.

## The price chart

- **History range:** 1D, 5D, 1M, 3M, 1Y, 3Y, 5Y, 10Y or MAX. The default is 1M.
- **Studies** are switched on and off with the chips above the chart: EMA 9, 21, 50, 100 and 200, Bollinger bands, VWAP, volume, RSI 14 and MACD.
- **Reset zoom** returns the chart to the full range after you zoomed or panned.
- **Analysis insights** below the chart show RSI (14), MACD (12, 26, 9), the order of the 9, 21, 50 and 200 EMAs and the distance from the 200 EMA, when the history is long enough.
- **Analyze with agent** sends a prepared question about the instrument, range and studies to the [Research Agent](agents.md). The answer is AI-generated.

Daily bars carry the date of the bar in UTC. Axis labels and tooltips use UTC.

## The feed status badge

The badge in the top bar describes four separate things, and you can open it for details:

- **The connection:** whether the app is connected to the price stream.
- **Freshness:** how long ago the last update arrived.
- **Data kind:** what the provider delivers, such as real-time quotes or bar closes.
- **Market session:** open or closed. If the provider states it, that is used. Otherwise it is computed from regular trading hours for exchanges whose ticker suffix is known (for example .ST, .L, .DE, .HK, .T), which does not know holidays or early closes: "closed" on a weekend or outside hours is reliable, "open" is not a promise. Bare tickers and unknown suffixes show unknown.

The wording is deliberate. **Live prices** appears only when the provider says its quotes are real time and an update arrived recently. **Last update 14:32 UTC** means prices are arriving but are not confirmed real time. **No new prices since …** means the connection is open but nothing recent came in; the market may simply be closed. When a provider states the time of the last quote, the badge reads **Last quote 14:32 UTC**; a stated delay reads **Delayed 15 min** and overrides any real-time claim. A closed market reads **Market closed · last 21:00 UTC** instead of stale, with no Reconnect. Providers that do not send a quote time or delay show those as unknown. Use Reconnect in the badge if the stream seems stuck.

## The selected instrument

The instrument chip in the top bar follows you across pages. Open the search with nothing typed to see your **Favorites** (star the selected instrument) and your **Recent** instruments, each with exchange and provider. Favorites and recent instruments are kept in this browser. On phones, the search button opens a full-screen picker. In live mode, choosing an instrument selects it without starting to track it: the badge says "Not tracked live" and a **Track live** button starts the tracker (up to 16). Pages that do not use the instrument, such as Flows or Settings, mute the chip and say so.

## Fundamentals

When the provider has them, a Fundamentals card appears below the insights. For a stock it has an **Overview** (day high and low, last price, spread and performance over several periods) and **Key ratios** (such as P/E, P/S, P/B, EPS, dividend yield and market cap). For a fund the second tab is **Risk & ESG**, and a **Top holdings** tab is added. Each value is as the provider reports it. A missing value shows a dash or "N/A".

## Financials

The Financials page shows quarterly income statement and cash-flow figures for the selected instrument, with the provider, the period and the filing form. Choose **Recent** for the latest quarters, or **Year range** to set a first and last year. A revenue trend shows the quarters from oldest to newest. "Analyze with agent" hands the statements to the Research Agent.

Statements are as the provider reports them. Quarters that the provider does not have are not estimated.

## World

World gathers current headlines into **themes** (for example energy or interest rates) and shows which sectors and commodities each theme tends to move, and who is exposed when the driver rises or falls.

- Every headline shows its publisher and its **publication date in UTC**. When the source gives no date, the item says "Date unavailable". When only a relative age such as "2 days ago" is known, the item shows it as approximate. A date is never guessed.
- A small source badge on each headline opens the details of where it came from. See [Provenance and trust](provenance.md).
- **Check a company** shows how the active themes reach a company you pick.
- **Refresh** loads the headlines again; **Summarize** sends the themes to the Research Agent.

Headlines need a provider plugin with news, or a web-search key saved by an administrator. Without either, World says there are no headlines yet.

## Limits

- Coverage differs by provider and by market. A symbol that one provider knows may not exist at another.
- Delayed or bar-close data is not live. Read the badge before acting on a price.
- Indicators are calculated from the history shown. A short history gives less meaningful studies.
- None of these screens place orders.
