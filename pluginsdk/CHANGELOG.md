# Plugin SDK changelog

Every change to the exported API, the wire protocol types or the manifest
schema adds an entry under **Unreleased**, labeled **Breaking**, **Additive**
or **Fix**, with migration notes for breaking changes. The release workflow
publishes the section for the tagged version as the release notes. See
`docs/releasing.md` and `pluginsdk/compat.json`.

## Unreleased

- Additive: provider types `broker` and `trading_host` (`broker.go`), the
  contract for trading plugins. A broker extension declares
  `provider.broker` (`BrokerInfo`: contract version, environments `paper`
  and/or `live`, order types, time in force, asset classes) and any of the
  capabilities `broker_accounts` (required), `broker_positions`,
  `broker_orders`, `broker_submit_order` and `broker_cancel_order`. A trading
  host declares `provider.trading_host` (`TradingHostInfo`) and
  `trading_order_intents`. Both answer through the existing `domain` method,
  so the wire protocol is unchanged. Their operations take fixed inputs:
  declare them with `StandardOperations`; a manifest with other inputs is
  rejected. Results are typed (`BrokerAccount`, `BrokerPosition`,
  `BrokerOrder`, `OrderIntent` and their result envelopes), each with a
  `Validate` method. `BrokerContractVersion` is 1. FloweySales routes every
  order to the broker and account the caller names and only ever sends
  orders to `paper` accounts; a `live` account is listed but refused.
  Existing plugins keep working. A backend from before these types rejects a
  plugin that declares them, so declare them together with a
  `min_app_version` that knows them once a release version is known.
- Fix (documentation of the same contract): `broker_orders` is called with a
  `client_order_id` by FloweySales' background reconciliation and must return
  the order for that id with the broker's real status. Fills never decrease, an
  order that ended keeps its status, `FILLED` is filled in full and
  `PARTIALLY_FILLED` is partial. No wire or API change.

## v0.5.0

- Additive: capability `profile` (method `profile`, `ProfileParams`,
  `InstrumentProfile` with optional `CompanyProfile`, `FundProfile`,
  `CommodityProfile` and `CryptoProfile` parts). A provider states what it
  knows about one instrument: kind, name, codes (ISIN, MIC, currency,
  country), its own sector and industry, kind-specific facts, and an as-of
  time. Fields the provider does not state are left out, never guessed.
  Existing plugins keep working. A backend from before this capability
  rejects a plugin version that declares it, so declare it together with a
  `min_app_version` that knows `profile` once a release version is known.
  Finnhub 1.3.0 and CoinGecko 1.1.0 are the first official plugins to
  answer it.

## v0.4.0

- Additive: optional manifest field `provider.markets`, the markets a provider
  covers as ISO 3166-1 alpha-2 codes (`SE`, `US`), and
  `ProviderInfo.CoversMarket`. FloweySales warns in Settings when a source
  chosen for a market does not list it; it never changes routing. Older
  backends ignore the field, so existing plugins and backends keep working.
  A plugin built with an earlier SDK drops the field from `describe`, so a
  plugin that states markets needs v0.4.0. Avanza 1.1.0 and Stooq 1.1.0 are
  the first official plugins to state them.

## v0.3.0

- Additive: capability `macro`. It has no method of its own: it says the
  provider's `history` answers the macro series ids listed in `docs/plugins.md`,
  and it requires `history`. Existing plugins keep working. A backend from
  before this capability rejects a plugin version that declares it: the store
  index skips that version with a warning and keeps offering earlier ones. Once
  a FloweySales release that knows `macro` exists, a plugin can name it in
  `min_app_version`. FRED 1.1.0 is the first official plugin to declare it.

## v0.2.0

- Additive: package `feed` parses RSS 2.0 and Atom headline feeds (DTDs and
  external entities are never loaded, 2 MiB cap) and converts them to
  `NewsItem`s with the feed's publication date. An unreadable date stays 0 and
  is never guessed. Plugins can use it where a source's JSON search omits dates.
- Additive: capabilities `news` (method `news`, `NewsItem`) and `events`
  (method `events`, `CorporateEvent`), the `tools` capability (plugin-contributed
  UI tab, method `tool`) and domain providers with saved-transaction exports.
  Existing plugins keep working; a plugin declares only what it implements.
- Fix: `httpx` waits for a 429 `Retry-After` hint of up to 5 seconds before
  retrying; a longer hint fails fast with the value in `StatusError.RetryAfter`.
- Fix: `httpx.ToFloat` is strict. It rejects trailing text such as `12abc`,
  NaN and Inf instead of returning a partial number. Plugins that relied on the
  lenient parse must clean the value first.
- Additive: release management gates (`compat.json`, API compatibility check,
  changelog-driven release notes). No API or protocol change.

## v0.1.0

- First tagged SDK. Wire protocol 1. Packages `pluginsdk`, `httpx`,
  `research` and `plugintest`.
