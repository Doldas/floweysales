# Plan 6: Market context

**Issues:** #210, #211.

**Why one plan:** both live in the topbar and in `MarketState`, and both depend on knowing exactly which instrument and which data is being shown.

## What happens today

**#210: "Live" does not mean live.** `MarketState.statusLabel` returns `Live` whenever a tracked symbol exists and the SSE connection is open,
with no look at the data (`frontend/src/app/core/market-state.ts`). More precisely:

- `LiveSseService.connected` turns true on `es.onopen`, with no tick needed. Keep-alive comments do not reach JavaScript, so a healthy
  connection and a silent feed look identical.
- **A tick's time is not the quote's time.** `tracker_runtime.go:146` sets `Timestamp: time.Now().Unix()`, so it is when *we* polled. A
  "last quote" based on it would be false.
- Server-side, `QuoteSnapshot` already has source, as-of, age, stale flag and `IsRealtime`. It always sets `IsRealtime: false` with type
  `ohlc_bar_close`, and the note says it is not a verified live quote. So the backend is more honest than the UI.
- `Stopped out`, a tracker's signal, shares the badge with connection states.
- `es.onerror` does not separate "the browser is retrying" from "gave up".
- No market-session data exists in the code. The first-tick timeout message guesses: "the market may be closed".
- The label appears in the topbar and the sidebar.

**#211: the header control partly exists.** `fs-symbol-search` is already in the topbar, and the sidebar footer shows the active instrument.
What is missing:

- The topbar search is empty when not in use, so **it does not show the current selection**. Several pages add their own search
  (Markets, World, Financials, Team, Strategies).
- **Selecting has side effects.** In live mode `launch()` posts to `/api/config` and starts a tracker (limit 16). In research mode it
  adds to the watchlist. Browsing should do neither.
- **Stale responses:** `loadHistory` has a symbol guard only, with no cancellation. A slow response for the same symbol with an older range or
  provider can overwrite a newer one.
- **No deep links:** no page reads `?symbol=`.
- Identity is `{symbol, provider}`. Persisted as loose `lastTrackedSymbol` and `lastProvider` keys.
- Good news: the agent context is computed from `MarketState`, so it follows automatically.

## Work packages

### WP1: Honest feed status (M, `fix/feed-status`) — #210

**Backend:**
- Add to `TradingRecord`: `quote_time` (from the provider, absent when unknown), `poll_time` (today's timestamp), `quote_type`, `delay_seconds`, and `source`.
  Providers that don't report a time leave it absent. **To verify:** what each plugin returns; an SDK change may be needed.
- Add `market_state` (`open`, `closed`, `unknown`) only when the provider states it. **Do not compute it from a calendar in this issue**: holidays make that wrong.

**Frontend:** a pure `feedStatus(input, now)` in `core/feed-status.ts`, with four independent parts:

| Part | Values |
| --- | --- |
| Connection | connecting, connected, reconnecting, offline |
| Freshness | fresh, stale (threshold from the provider's poll interval), no data yet, unknown |
| Data type | realtime (only if the provider says so), delayed N min, last bar close, unknown |
| Session | open, closed, unknown |

The headline comes from the worst relevant fact:

| State | Label |
| --- | --- |
| Connected, recent quote, realtime confirmed | Live prices |
| Connected, recent, not confirmed realtime | Last quote 14:32 UTC |
| Connected, delayed | Delayed 15 min |
| Connected, old data | No new prices since 14:32 |
| Provider says closed | Market closed · last 17:30 |
| SSE retrying | Reconnecting… |
| Offline | Prices not updating |
| Research mode | Prices from history |

- **The pulse dot only shows for the first row.** The issue's central criterion follows from this: a connection alone never produces "Live".
- Move `Stopped out` to its own badge.
- Status becomes a button opening a popover: each of the four parts in plain words, plus provider, official or unofficial, and quote time in UTC. "Reconnect" appears
  only when offline or stale with an actual connection problem.
- `role="status"` announces changes, throttled so that it is not chatty. Same component in topbar and sidebar.
- `es.onerror` separates `CONNECTING` from `CLOSED`.

**Tests:** spec table for connected but stale, delayed, closed, reconnect and recovery, and unknown; a test that no input without a confirmed realtime source and
fresh quote yields `Live`. Add it to the `node --test` list.

### WP2: Instrument in the header (M, `feature/global-instrument`) — #211

- **Separate selecting from tracking (owner decision 5).** `MarketState.select(ref)` loads history and updates context, with no server changes. `track(ref)` remains an explicit
  action ("Track live" for those allowed to, "Add to watchlist" for everyone). Ticks flow only for tracked instruments, and WP1's status says so.
  This changes behavior for existing users, so note it in the PR: **to verify** what depends on `/api/config` following the selection, such as paper trading on Markets.
- **Selector.** `fs-instrument-switcher` in the topbar: idle it shows name, ticker and exchange, and opening it gives search with recent and favorite instruments. Uses
  the combobox pattern already in `fs-symbol-search`. Remove page-level searches where they only duplicate it. On mobile, it collapses to the ticker and opens full screen.
- **URL.** `?symbol=VOLV-B.ST&provider=YAHOO` on instrument-aware routes. Order on load: URL, then stored, then none. Selection updates the URL with `replaceUrl`, so Back
  still leaves the page. Unknown symbols show an error and do not fall back.
- **Stale responses.** Replace `loadHistory` with a `switchMap` keyed on provider, symbol and range. Apply the same request-key check in instrument-aware panels: Financials, World impact, Strategies.
- **Page behavior.** Each route declares `data: { instrument: 'used' | 'ignored' }`. Ignored pages show the selector as muted, saying "Not used on this page". Unsupported instruments show a message
  in that panel, never a different ticker.
- **Agent.** A running chat keeps the instrument it began with; the next message uses the new one, and the context chip shows which applies. Teams likewise.

**Tests:** spec for URL-versus-stored priority; a race test where a slower older request cannot overwrite a newer one; same ticker on two exchanges remains distinct; keyboard navigation
with `smoke.mjs` or Playwright.

### WP3: `InstrumentRef` (M, `feature/instrument-ref`) — shared with Plan 4

```go
type InstrumentRef struct {
    Provider, Symbol string
    Name, Currency   string // optional
    Exchange, MIC    string // optional
    ISIN             string // optional, only when the provider states it
}
```

- Key: provider + symbol. Display key adds exchange.
- A small table maps Yahoo suffixes to MIC (`.ST` to XSTO, `.T` to XTKS, `.JO` to XJSE and so on), tested and in one file. Unknown suffixes stay unknown.
- Add optional `mic`, `isin` and `currency` to the SDK's `SearchResult`. Needs contract testdata, a `pluginsdk` tag, and a plugin repo bump. Old plugins keep working.
- `TrackedAsset` keeps its shape on the wire, with new optional fields. Migrate `lastTrackedSymbol` and `lastProvider` to one stored ref, reading the old keys once.

**Order:** WP3 can come before or after WP2. If WP2 ships first, it uses `{symbol, provider, exchange}` and the search result's exchange, and WP3 extends it.

## Acceptance mapping

| Issue | Covered by | Note |
| --- | --- | --- |
| #210 | WP1 | Delayed and session states appear only where plugins report them; others show "unknown" as the issue asks |
| #211 | WP2, WP3 | |

## Out of scope

An exchange-calendar service for session state, until a reliable source is chosen. Recent and favorite lists synced across devices.
