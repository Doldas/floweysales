---
title: Markets
order: 3
summary: The price chart, indicators, watchlist, feed status badge, fundamentals, quarterly financials and the World view.
last-reviewed: 2026-11-15
---

# Markets

Markets is where you look at one instrument at a time: its price history, key figures, indicators and fundamentals. The same page hosts the [Leaderboard](leaderboard.md). Quarterly statements have their own page, Financials, and news themes are on World.

Everything on these screens comes from the market-data provider plugins your administrator installed. Without one, the screens are empty and say so.

## Pick an instrument

Use the search box in the top bar or on the page. Results show the symbol, the name where the provider has it, the exchange and the **provider**. Press Enter to try the exact symbol you typed. Recently used instruments are listed when you focus the search box.

The selection is kept in the address of the page, so a copied link opens the same instrument. The selected instrument's price chart appears above the group dashboard.

## Watchlist and trackers

The row under the search box lists the instruments you follow, up to 16 (the counter shows `n/16`). Select one to view it. The cross stops tracking it. On a phone, tap **Active trackers** or **Watchlist** to open the row. Watchlist prices, the paper order ticket when available, and key figures open from panels below the chart.

What this row is depends on your role:

- **Most users** get a **watchlist** and a research view. The key figures are the last close, the change over the chosen range, and the high and low of that range.
- **Administrators** get **active trackers** with a live feed: the latest price, the stop-loss level, fast and slow EMAs and the unrealized profit or loss of the paper portfolio. Administrators also see a button that deletes the stored tick history; it asks you to click twice.

If a figure is missing, the screen shows a dash, not zero.

To keep larger sets of instruments, grouped by theme and stored on the server, use [Collections](collections.md).

## Groups

The **Groups** card below the price chart shows your [collections](collections.md) as groups. On a phone, tap **Groups** to open it. Choose a group to list its instruments, then select one to return to its price chart. **Add {symbol} to a group** adds the instrument on screen to one of your groups, or to a new watch collection you name there. Groups that already hold it are marked. Adding never changes the watchlist or the trackers. **Manage groups** opens Collections.

### Group dashboard

A group with instruments gets a dashboard, computed in your browser from each instrument's daily price history at its own data provider. Choose 1M, 3M, 1Y or 3Y.

- **Group**: the change of the group index over the range. The index buys every instrument on the first day all of them have a price and holds without rebalancing, rebased to 100. A watch collection weights the instruments equally; a model portfolio uses its hypothetical weights, and instruments without a weight are left out of the index and named.
- **Volatility** (the annualized deviation of daily returns), **Max drawdown** (the largest fall from a high), **Breadth** (how many instruments rose and fell), and the **best** and **worst** instrument.
- **Group index** chart with a labeled color and line pattern for each plotted instrument. Select a ticker in the legend to highlight it, then select it again to show all lines at full strength. Hover or tap the chart to read a day, or focus it and use the arrow keys, Home and End.
- **Composition** donut by weight, asset type, exchange, currency, country, data provider or another stored dimension. Model portfolios show the unallocated rest as its own slice.
- **Return per instrument**, **Risk and return** (volatility against return) and a **Correlation** grid of daily returns. A pair with fewer than 20 shared trading days shows a dot instead of a number.

Every chart has a table view or a text equivalent. Instruments without history are named, not filled in. Prices are in each instrument's own currency and are not converted, so a group that mixes currencies says which ones it holds.

## The price chart

- **History range:** 1D, 5D, 1M, 3M, 1Y, 3Y, 5Y, 10Y or MAX. The default is 1M.
- **Studies** are switched on and off with the chips above the chart. On a phone, tap **Chart options** for studies, bar length, chart style and drawing tools.
- **Reset zoom** returns the chart to the full range after you zoomed or panned.
- **Analysis insights** below the chart show RSI (14), MACD (12, 26, 9), the order of the 9, 21, 50 and 200 EMAs and the distance from the 200 EMA, when the history is long enough.
- **Analyze with agent** sends a prepared question about the instrument, range and studies to the [Research Agent](agents.md). The answer is AI-generated.

Daily bars carry the date of the bar in UTC. Axis labels and tooltips use UTC.

### Panes: fold, resize, full screen

The **Price chart**, **Analysis insights** and the leaderboard **Ranking** are panes. Each pane header has a chevron button that folds the body, and the chart and ranking have a full-screen button (Escape leaves it).

- **A fold hides only the body.** The title, a one-line summary and the facts that say where the data came from stay visible. Under the chart these are the source, the bar size and range, the currency, the time of the newest bar in UTC and, when studies are on, that they are calculated here from the bars. Above the ranking they are the provider, the price date and what a score means. The chart keeps its zoom and your studies while folded.
- **Resize** the chart and the ranking by dragging the bar under them, or focus it and use the arrow keys (24 pixels), Page Up and Page Down (120), Home and End. Double-click it to return to the default height.
- Folds and heights are saved in this browser, per pane. Nothing is removed from the page and nothing is saved on the server.

### Compact density

The density button in the top bar (or **Use compact density** in the More actions menu on phones) tightens padding and type size across the app so more rows and figures fit on a screen. No column, figure, warning or source line is dropped. The choice is yours alone and is saved in this browser.

### Long ranking lists

**Show all** on a long ranking draws only the rows in view while you scroll; every row stays in the list, in the CSV and JSON downloads and in the row count. Click or press Enter on a row to open it in Markets, or use **Score breakdown** on a row to see its factors, provider and price date.

## The feed status badge

The badge in the top bar describes four separate things, and you can open it for details:

- **The connection:** whether the app is connected to the price stream.
- **Freshness:** how long ago the last update arrived.
- **Data kind:** what the provider delivers, such as real-time quotes or bar closes.
- **Market session:** open or closed. If the provider states it, that is used. Otherwise it is computed from regular trading hours for exchanges whose ticker suffix is known (for example .ST, .L, .DE, .HK, .T), which does not know holidays or early closes: "closed" on a weekend or outside hours is reliable, "open" is not a promise. Bare tickers and unknown suffixes show unknown.

The wording is deliberate. **Live prices** appears only when the provider says its quotes are real time and an update arrived recently. **Last update 14:32 UTC** means prices are arriving but are not confirmed real time. **No new prices since …** means the connection is open but nothing recent came in; the market may simply be closed. When a provider states the time of the last quote, the badge reads **Last quote 14:32 UTC**; a stated delay reads **Delayed 15 min** and overrides any real-time claim. A closed market reads **Market closed · last 21:00 UTC** instead of stale, with no Reconnect. Providers that do not send a quote time or delay show those as unknown. Use Reconnect in the badge if the stream seems stuck.

## The quote header

Above the chart, the instrument's price and day change are followed by the day's figures where the provider has them: **Prev. close**, **Open** and a **Day range** bar with a dot at the price now. A pill shows the market session. The watchlist shows each tracked instrument's latest price, day change, a five-day trend line and how fresh the price is; prices refresh once a minute while the tab is visible. The ticker tape under the top bar shows the same prices on every page; pause it with its button.

## Price alerts

The bell beside the instrument name sets price alerts. The level starts a little above the price; an alert must be above or below the price now, and the panel says which and how far away it is. When a price reaches the level you get a message and, if you allowed notifications, a browser notification while the tab is in the background. A hit alert stays listed, with its price and time in UTC, until you clear it. Alerts are kept in this browser only, are checked only while FloweySales is open and never place an order. Command Center lists all of them with how far the price has to go.

## Price lines on the chart

The chart draws the average entry of an open paper position as a solid blue line and each alert level as a dashed line, amber while waiting and green once hit, each with a tag at the right edge. A level outside the visible range shows as a tag with an arrow at the top or bottom edge. Screen readers get the same lines as a list.

## The selected instrument

The instrument chip in the top bar follows you across pages. Open the search with nothing typed to see your **Favorites** (star the selected instrument) and your **Recent** instruments, each with exchange and provider. Favorites and recent instruments are kept in this browser. On phones the top bar shows only the ticker, on one row and not cut off at normal text sizes; tap it to open a full-screen picker with the full ticker, name, exchange and provider, the **Add to favorites** / **Remove from favorites** button and the search. There is no separate star in the top bar. Less-used actions (Search or run, theme) sit in the **More actions** menu, and the page name is shown by the page heading and the highlighted entry in the navigation. In live mode, choosing an instrument selects it without starting to track it: the badge says "Not tracked live" and a **Track live** button starts the tracker (up to 16). Pages that do not use the instrument, such as Flows or Settings, mute the chip and say so.

## Fundamentals

When the provider has them, a Fundamentals card appears below the insights. On a phone, tap **Fundamentals** to open it. For a stock it has an **Overview** (day high and low, last price, spread and performance over several periods) and **Key ratios** (such as P/E, P/S, P/B, EPS, dividend yield and market cap). For a fund the second tab is **Risk & ESG**, and a **Top holdings** tab is added. Each value is as the provider reports it. A missing value shows a dash or "N/A".

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
- The group dashboard asks for the history of at most 25 instruments of a group; the rest are named. Its figures are hypothetical, not holdings or advice.
- None of these screens place orders.
