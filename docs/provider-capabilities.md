# Provider safety and freshness (#4)

The authenticated research endpoints `GET /api/providers` and
`GET /api/quote?symbol=GIB&provider=YAHOO` and the agent tools
`get_provider_capabilities` / `get_provider_quote` provide a
provider capability registry plus OHLC-source currency, timestamp,
age, source, requested provider, actual fallback provider and stale flag.
**Prices are OHLC closes, never asserted as real-time executable quotes.**
If a price or its currency is missing, no synthetic zero is returned.
Stale is conservatively >6h for intraday bars, >36h for daily bars;
holidays/weekends may be stale without a provider outage.

Yahoo's chart/fundamentals APIs are unofficial. Finnhub is an official
API with key/plan-dependent permissions and quotas. Binance is an
exchange-operated crypto API, not a company filing source.

Avanza public endpoints are undocumented and unsupported and may
change without notice. Set `AVANZA_UNOFFICIAL_ENABLED=true` in
`.env` only after independently confirming acceptable usage conditions
and licensing. By default Avanza data methods are blocked/empty.
When enabled, public Avanza requests are throttled to approximately
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

## Bounded retries

Idempotent provider HTTP GET calls retry HTTP 429, 502, 503 and 504
at most twice with bounded backoff (three total attempts). 401/403 and
other hard failures are not retried or bypassed. Avanza throttling
still applies to each attempt. Search POST calls are not automatically
retried, to avoid assuming a non-idempotent endpoint is safe. Verify
licensing independently; retries do not confer authorization.
