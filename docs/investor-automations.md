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
