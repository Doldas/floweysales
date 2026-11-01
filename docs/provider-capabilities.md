# Provider safety and freshness (#4)

The authenticated research endpoints `GET /api/providers` and
`GET /api/quote?symbol=GIB&provider=YAHOO` and the agent tools
`get_provider_capabilities` / `get_provider_quote` provide a
capability registry of the **installed provider plugins** (see
[plugins](plugins.md)) plus OHLC-source currency, timestamp,
age, source, requested provider, actual fallback provider and stale flag.
**Prices are OHLC closes, never asserted as real-time executable quotes.**
If a price or its currency is missing, no synthetic zero is returned.
Stale is conservatively >6h for intraday bars, >36h for daily bars;
holidays/weekends may be stale without a provider outage.

Each plugin declares its capabilities, official/unofficial status and
minimum poll interval in its manifest; a provider whose plugin is not
installed does not appear, and one missing a required setting (such as an
API key) reports `enabled: false`.

Yahoo's chart/fundamentals APIs are unofficial. Finnhub is an official
API with key/plan-dependent permissions and quotas. Binance is an
exchange-operated crypto API, not a company filing source.

Avanza public endpoints are undocumented and unsupported and may
change without notice. The Avanza plugin is experimental and is never
installed by default; installing it (like the unofficial Yahoo plugin)
requires accepting its terms, which ask you to independently confirm
acceptable usage conditions and licensing first. This replaces the former
`AVANZA_UNOFFICIAL_ENABLED` setting, which no longer has any effect.
Public Avanza requests are throttled to approximately
one per second per process, responses bounded. No BankID login,
account access, broker orders, anti-bot bypass or reverse-engineered
authentication is added. Yahoo fallback for Avanza histories is
explicitly identified as Yahoo in the returned source.

Symbol normalization is constrained to ticker-safe punctuation.
For financial statements use sourced SEC EDGAR figures when available;
never infer an equity's trading currency from the issuing company's
accounting/reporting currency.

Further hardening/backoff/retry policy is future work; provider capability
declarations are deliberately conservative rather than claiming
real-time access we cannot substantiate.

## Corporate events

A provider plugin that declares the `events` capability serves dividends,
splits and earnings dates through `GET /api/events` (see [plugins](plugins.md)).
Earnings dates from a provider that only estimates them carry
`confirmed: false`; an unknown dividend amount is left out, never `0`.

## Bounded retries

The bundled plugins' idempotent HTTP GET calls retry HTTP 429, 502, 503 and 504
at most twice with bounded backoff (three total attempts). 401/403 and
other hard failures are not retried or bypassed. Avanza throttling
still applies to each attempt. Search POST calls are not automatically
retried, to avoid assuming a non-idempotent endpoint is safe. Verify
licensing independently; retries do not confer authorization.
