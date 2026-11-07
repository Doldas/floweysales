# Universe coverage check

Issue #209 asks for more markets on the Leaderboard. No Asian, African, South American or Oceanian list has been added,
because nothing here has been checked against a market-data provider: symbols, currencies and history are unknown until
someone runs a provider. This page describes the routine that does the check, and what it needs.

## What it checks

For each candidate market (`internal/universecheck`, `Candidates()`): the share of candidate symbols that have

- a price (the symbol resolves),
- a year of daily prices (a 12-month change can be computed), and
- four reported quarters (trailing figures can be computed),

together with the currency the prices are quoted in, the newest and oldest price date and whether any price is more than
7 days old.

The bar was set before any result: **at least 80% of candidates with a year of prices and four reported quarters** is a
`full` verdict. Prices at 80% or more with fewer reports is `prices_only` (at most a low-confidence ranking; not offered
today). Anything less is `unqualified`. Fewer than 5 candidates is `too_few`; a market with no symbols is `no_symbols`.

The candidates are Tokyo (`.T`), Hong Kong (`.HK`), India (`.NS`), Korea (`.KS`), Johannesburg (`.JO`), Sao Paulo (`.SA`),
Sydney (`.AX`) and Toronto (`.TO`), eight widely held large companies each, written from memory and not verified. Lagos, Nairobi and Cairo have **no
symbols** because the provider's ticker format for them is not known; inventing one would produce a false "no data". A
maintainer who learns the real format adds the market to `Candidates()`.

## Running it

```
go run ./cmd/universe-coverage                      # offline: validates the candidate lists, measures nothing
go run ./cmd/universe-coverage -input saved/        # offline: judges saved leaderboard responses (saved/tokyo.json, ...)
go run ./cmd/universe-coverage -url http://localhost:8080 -cookie "$FLOWEY_SESSION" -save saved/ -out report.json
```

- The offline default checks that every candidate symbol is well formed, unique and ends in its market's suffix, and that each
  market names the currency to expect. It exits with status 2 on a problem.
- `-url` asks a running server for `GET /api/market/leaderboard?symbols=...&strategy=best&preferences=off` per market. **This
  needs a server whose market-data provider plugin can reach those exchanges**, and the session cookie when authentication is
  on. `-provider` picks a provider other than the default. `-save` keeps the raw responses so the verdict can be re-judged offline.
- `-input` judges saved responses without any access, so the measurement and the judgment can happen in different places.
- `-markets tokyo,korea` limits the run; `-strict` exits 1 unless every market with symbols reached `full`.

Check the provider's terms for the exchanges and for automated use before enabling a market (see the safety rules in
`CONTRIBUTING.md`); the check does not.

## After a run

A `full` verdict does not enable anything. To offer a market, a maintainer still writes a reviewed list into
`internal/server/universes.json` (with its source, rule, limitations and date), shows the run's date and results in the
description, and updates [provenance-coverage.md](provenance-coverage.md). A region with no such list keeps showing
"No supported market yet". Two tests guard this: no universe may sit in a region the check has not cleared, and every candidate
suffix must be one the Leaderboard can read an exchange from.

The Leaderboard now has a global scope that combines the reviewed lists, region/country/exchange/currency filters, and a choice between
percentiles within each region and across all companies (Plan 4, WP3 steps 2 and 4). Still open for global ranking: converting returns
across currencies (no FX source: returns stay in local currency and the page says so), per-company accounting standards, sector and
liquidity filters, and an Asian and an African market, which need a maintainer to run this check with provider access.

## User-supplied index constituents (#258)

The S&P 500, Nasdaq-100 and OMXS30 index lists are licensed, so FloweySales does not ship, commit or redistribute any of them.
Each user supplies their own, and the index becomes `available` for that user only (a registry entry in `internal/server/indices.json`
stays `unavailable` and empty; `TestShippedIndicesAreUnavailableAndEmpty` guards it).

| Index | CSV import | Financial Modeling Prep | Past membership |
|---|---|---|---|
| S&P 500 | yes | yes (`stable/sp500-constituent`, `stable/historical-sp500-constituent`) | from FMP's add/remove records |
| Nasdaq-100 | yes | yes, **unverified** (`stable/nasdaq-constituent`, `stable/historical-nasdaq-constituent`) | from FMP's records, unverified |
| OMXS30 | yes | no known API: CSV only | none (today's members only) |

**CSV.** Columns `symbol` (required), `name`, `weight`, `exchange`; plus the as-of date (UTC), the source name and a licence or
terms acknowledgement the user must confirm. Validation refuses duplicates (also `BRK.B` against `BRK-B`), malformed symbols,
symbols from the wrong exchange (a `.ST` suffix or an `ST` exchange is required for OMXS30; a US list carries no suffix), a
count far from the index's size (under half, or over one and a half times, the nominal count) and bad weights; a blank weight
stays blank, never 0. A modest gap from the nominal count is a warning. No row is ever invented.

**Financial Modeling Prep.** The user's own key is sealed with the server secrets key and stored under `index_fmp:<user id>` in
`api_integration_secrets`; it is never logged or returned, and errors from the HTTP client are scrubbed of it. The HTTP client is
injectable and tests use fixtures only. Verified from FMP's documentation search results: the endpoint **paths** above. **Not
verified:** the response field names. FMP's documentation pages answered 403 to the build environment, so the parser accepts
`symbol`/`name` for current members and `symbol` (added), `removedTicker`, and `date` or `dateAdded` for changes, and refuses a
response with none of them rather than guessing. The fixtures in the tests are hand-written to that shape, not recorded from FMP.
Someone with a key should compare a live response before the Nasdaq-100 flag is lifted. FMP's terms, rate limits and which plan
includes constituent data are for the user to check; the UI and guide say so.

**Point-in-time.** `Snapshot.MembersAt(date)` starts from the list as of its date and undoes every change dated after the
requested day. It is known only from the earliest change record onward; before that, or with no records, today's members stand in
and the backtest says so (`membership.mode`, a note, and the survivorship warning). Companies that left an index often have no
price history from the market-data provider; they are skipped and named, and the survivorship warning stays in that case.
