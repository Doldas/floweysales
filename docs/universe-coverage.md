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

Not covered by the check, and still open for global ranking: comparing returns across currencies (no FX source), mixed
accounting standards and fiscal calendars, and percentile comparison within versus across regions (Plan 4, WP3 steps 2 to 5).
