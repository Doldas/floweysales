# Plan 5: Strategy validation

**Issue:** #195. Uses the provenance contract from Plan 3 WP2. #172, which the issue mentions, is closed, and this builds on what it delivered.

## What happens today

There are **two engines**, and the issue concerns the first:

| | `internal/strategy` | `internal/backtest` |
| --- | --- | --- |
| Tests | Rules on one instrument | A factor ranking over a universe, with rebalancing |
| UI | Strategies page: 3Y, 5Y, 10Y | Leaderboard panel: 3Y, 5Y |
| Costs | `CostBps`; slippage explicitly not modeled | `CostBps` |
| Warnings | Fill rules, no dividends | Survivorship and others |

- `strategy.Run` needs daily bars and has no date concept of its own, which makes custom dates simple: fetch a range that
  covers the period, then clip. `Range` allows only the three presets (`strategies.go:269`).
- `walkForward` cuts history into equal windows and assigns trades to them. **This is a sub-period view, not walk-forward
  testing**, since nothing is fitted on one period and tested on the next. It should say so.
- The AI analysis is a client-built prompt (`strategyPrompt`) sent to the agent as text. It says "use only the figures below"
  but nothing checks that.
- Results are not stored. A rate limit of 20 runs per minute applies (`strategyBacktestPerMin`).

## Work packages

### WP1: Flexible periods (S, `feature/strategy-date-range`)

- Request takes `from` and `to` alongside `range`, as UTC dates, and the date fields say so. Keep presets, and add 1Y, YTD and Max. Check the minimum bar count, and
  report the data actually available: "you asked from 2005; data starts 2011".
- Reusable named presets per user, in local storage first.
- Same request model for `internal/backtest`, replacing `years must be 3 or 5`. Check `backtestKey` includes the dates.

**Tests:** clipping at boundaries; data starting later than requested; `from` after `to`; no look-ahead from indicators warmed
on clipped data, since warm-up bars must come from before `from` where they exist.

### WP2: Many instruments in one run (L, `feature/strategy-batch`)

- `POST /api/strategies/batch`: one definition, a list of instruments or a universe, and a period. Max 30 instruments, 4 at a time,
  and it streams progress over SSE. The limit counts instruments, not calls, so it cannot get around the rate limit.
- Failures are per instrument and never hidden.
- Result: per-market row and an aggregate of median return, share beating buy and hold, spread between best and worst, and total trades
  with the number of instruments behind it. **Do not report a plain mean as "the result"**: one outlier dominates it.
- Currency: returns are compared as percentages in each instrument's own currency, stated clearly. Reuse the FX decision from Plan 4 if
  one exists; otherwise no conversion.
- UI: comparison table, sorted and linked to per-instrument drill-down using the existing result view.

**Tests:** one failing instrument leaves the rest; aggregates computed by hand; cancellation stops work.

### WP3: Robustness (L, `feature/strategy-robustness`)

Each is an option on a run:
- **Slippage:** `Config.SlippageBps`, applied at fills. Removes the "not modeled" warning only when set.
- **Cost sensitivity:** a grid such as 0, 10, 25, 50 bps per side.
- **Parameter sensitivity:** vary numeric rule parameters by ±10% and ±20%. A result that holds only at one setting gets a clear warning.
- **Real walk-forward**, for parameterized strategies: fit on one window, test on the next, report only test periods. Rename the current
  view "Sub-periods".
- **Regimes:** each period labeled from the benchmark's own trend and volatility, computed deterministically, with results shown per regime.
  Labels describe the past; nothing predicts a regime.
- **Stress periods:** named date windows (2008, 2020, 2022), only where data covers them, with the regional caveat that these windows were
  picked with hindsight.
- **Warnings from counts:** few trades (below 30), results dependent on a few trades, and a multiple-testing warning, since running many
  variants and choosing the best is the main way to fool yourself here.
- **Bias notes:** user-picked instruments carry selection bias, which the page should say. Dividends remain excluded unless the provider
  supplies adjusted data, and that is stated.

**Tests:** seeded, deterministic results; slippage reduces returns by the expected amount; walk-forward uses no test-window data when fitting,
which is the most important test here.

### WP4: Saved, reproducible runs (M, `feature/strategy-runs`)

- Table `strategy_runs (id, user_id, definition, definition_hash, params, data_hash, engine_version, result, created_at)`, with a per-user cap.
- Each run exposes provider, as-of, bar count, gaps and the warnings as a `provenance.Fact`. Same definition, data hash and engine version
  must give the same result, and a test asserts it.
- Export as JSON and CSV, including assumptions.

### WP5: Grounded AI summary (M, `feature/strategy-ai-summary`)

- `POST /api/strategies/runs/{id}/summary`. The server assembles the facts from the stored run, so the client no longer supplies the numbers.
- Output sections: where it worked; where it failed; drawdown, volatility and risk-adjusted figures; sample size; limitations; what to test next.
- Check with `grounding.Check` against the stored result, plus the audit from Plan 3 WP3 when available. On a failed check, retry once, then show
  the metrics with a note that no summary could be verified.
- Citations point to run id and metric. Adds the existing "research, not advice" line.

**Tests:** an invented figure fails; a thin sample must be stated; fake-model tests, in the style of `internal/eval`.

## Acceptance mapping

| Criterion | WP |
| --- | --- |
| Custom dates and reusable presets | WP1 |
| Several markets, comparable aggregate and per-market results | WP2 |
| AI summary based on recorded metrics | WP5 |
| Assumptions, coverage and reproducible parameters | WP3, WP4 |
| Citations and data lineage | WP4 |

## Out of scope

Optimizers that search for the best parameters. They raise overfitting risk, and the plan's aim is the opposite.
