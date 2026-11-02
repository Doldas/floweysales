# Investor growth plan (November 2026)

An implementation plan for making FloweySales a goal-driven investing
workspace: it knows which **phase** an investor is in, shows what that phase
needs, proves whether a strategy has worked before anyone relies on it,
watches the portfolio for risk, and adds new data sources (insider trades,
macro regime, dividends and earnings dates, crypto, free fallback prices).

The plan is written so that each work package (WP) can be implemented on its
own, in order, by a contributor or a smaller model that has not seen the
design discussion. Every WP lists the files to touch, the types and function
signatures, the exact algorithms, the tests and the acceptance criteria.

> **Product line we hold.** FloweySales helps people build wealth
> deliberately: clear goals, sized risk, evidence before confidence
> ([vision](../vision.md)). No feature promises returns, predicts prices or
> trades automatically. "Fast" growth is offered only as a capped,
> explicitly labelled *satellite* sleeve inside a plan (WP2). Every new page
> and node output carries the existing "research aid, not investment advice"
> disclosure.

---

## 0. Rules for whoever implements this

Read these before writing code. They come from how the repository already
works; breaking them fails CI or review.

1. **One WP = one branch = one PR**, branched from `main`, named
   `feature/<wp-slug>` (for example `feature/strategy-backtest`). Do the WPs
   in the order in section 2; later WPs use earlier ones.
2. **Deterministic finance in Go, prose in the model.** Every figure is
   computed in Go. Agents only describe rows they are given (vision
   principle 4). No `math/rand` without an explicit seed.
3. **Absent is not zero.** A figure that cannot be computed is left out of
   the row (or is `nil` in a struct with `omitempty`), never `0`. Follow
   `fundamentalsRow` in `internal/server/flow_investor.go`.
4. **Match the surrounding code.** Same comment density (short "why"
   comments, package doc comment at the top of each new file), same naming
   (`snake_case` JSON fields, `camelCase` Go locals), same error style
   (`fmt.Errorf("node %s: …", n.ID)` in validators).
5. **Database changes** are Liquibase changesets in `db/changelog/`, numbered
   after the last one (currently `032-flow-node-state.xml`; WP2 uses 033,
   WP3 uses 034). Add each to `db.changelog-master.xml`. Copy the shape of
   `032-flow-node-state.xml` (MySQL `dbms`, remarks, a `<rollback>`).
   MySQL-backed tests skip locally and only run in CI: keep DB logic thin
   and put the math in pure functions that are tested without a database.
6. **New flow node types** must be added in **all** of these places, or the
   graph is rejected or the editor cannot show them:
   - `investorNodeTypes` in `internal/server/flow_investor.go`
   - `flowAllowedForNonAdmin` in `internal/server/flows.go` (only if the
     node is safe for non-admins; admin-only nodes stay out)
   - `validateInvestorNode` (setting limits) and `runInvestorNode` (dispatch)
   - `flowNode` fields in `internal/server/flows.go` if the node needs new
     settings (`json:"…,omitempty"`)
   - `InvestorNodeType` and `INVESTOR_NODES` in
     `frontend/src/app/components/investor-nodes.ts`, plus the node
     settings form in `frontend/src/app/components/flow-studio.ts`
   - the node table in `docs/investor-automations.md`
7. **New playbooks** go in `PLAYBOOKS` and `buildPlaybook` in
   `investor-nodes.ts`, are covered in `investor-nodes.spec.ts` and in
   `internal/server/flow_playbooks_test.go` (the server validates every
   playbook graph), and get a row in the playbook table of
   `docs/investor-automations.md`.
8. **New agent tools** are added to `ToolDefs` and `Execute` in
   `internal/ai/tools.go`. If the tool needs engine data, add a small
   optional interface next to `ProviderCatalog` (for example
   `type PlanSource interface { … }`) and type-assert it in `Execute`, so
   `DataSource` stays unchanged. Add the tool to the read-only grant list in
   `internal/server/flow_agent_tools.go` only when it is read-only.
9. **Frontend**: Angular standalone components with signals, Bootstrap 5
   classes and the existing `fs-*` utility classes (`fs-page`, `fs-kpi`,
   `fs-eyebrow`, `fs-segmented`). Pure helpers go in a framework-free
   `*.ts` file with a `*.spec.ts` run by `node --test`. **Add every new spec
   file to the `node --test` line in `.github/workflows/ci.yml`** (the list
   is explicit). New pages are lazy routes in `app.routes.ts` and get an
   entry in the command palette (`shell/command-palette.ts`) and the side
   nav (`shell/shell.html`).
10. **Accessibility and responsiveness are tested.** Every new page must be
    added to the mocked backend in `frontend/smoke-fixtures.mjs` and to the
    page lists in `smoke-a11y.mjs` and `smoke-responsive.mjs`. Use real
    `<table>` markup with `<th scope>` for tables, `aria-label` on icon
    buttons, and no colour-only signals (pair red/green with an icon or
    sign).
11. **Before pushing** run: `go vet ./...`, `go test ./...`,
    `npm run build --prefix frontend`, the `node --test` line from CI, and
    for UI changes `npm run smoke:a11y` and `npm run smoke:responsive`
    against the production build (see README "UI smoke test").
12. **Plugin repository** (`Doldas/floweysales-plugins`, checked out at
    `../floweysales-plugins`) pins `pluginsdk` by pseudo-version. After an
    SDK change in this repo: merge/push floweysales first, then in the
    plugins repo `go get github.com/Doldas/floweysales/pluginsdk@<commit>`.
    Locally a gitignored `go.work` (`go work init . ../floweysales/pluginsdk`)
    builds against unpushed SDK changes.
13. Commit messages and PR bodies: describe behaviour, not process; no model
    names.

---

## 1. What exists today (so you do not rebuild it)

| Piece | Where | Notes |
| --- | --- | --- |
| Leaderboard: six factors, six strategies, five universes, cache | `internal/server/leaderboard.go` | `leaderboardMetricsRow(symbol, series, fin, now)` builds a metrics row from a price series and financials; `scoreLeaderboard(rows, strategy)` ranks rows. **Both are pure and reusable for backtests.** |
| Investor flow nodes | `internal/server/flow_investor.go` | `quoteRow`, `technicalRow`, `fundamentalsRow`, `flowRows`, `rowField`, `numberOf`, `screenRows`, `reportTable` |
| Screen rule parser | `internal/server/flow_screen_rule.go` | Reuse for any user-written condition |
| Only-new memory | `internal/server/flow_node_state.go`, Liquibase 032 | Per-node JSON state, pattern for new state |
| Notify | `internal/server/flow_notify.go` | SYS_ADMIN, side effect `notify.send` |
| Technicals | `internal/market/technical.go` | RSI, MACD, EMA, SMA, Bollinger, ATR |
| Paper trading | `internal/trading/trading.go`, `internal/server/orders.go` | `portfolioState()` returns orders, positions and `trading.Summary`; `GET /api/positions` |
| SEC EDGAR client | `internal/sec/edgar.go` | `Client.Resolve`, `Filings`, `CompanyFacts`; honours `SEC_USER_AGENT` |
| Agent tools | `internal/ai/tools.go` | `ToolDefs`, `Execute`, `DataSource`, `ProviderCatalog` |
| Plugin SDK | `pluginsdk/` | capabilities `quote history search financials research forum news`; `knownCapabilities` in `protocol.go` |
| Official plugins | `../floweysales-plugins/{yahoo,binance,finnhub,avanza,fred,ecb}` | `internal/yahooapi` shared by Yahoo |
| Frontend investor catalogue | `frontend/src/app/components/investor-nodes.ts`, `leaderboard-view.ts` | Framework-free, tested with `node --test` |

---

## 2. Work packages and order

| # | Work package | Size | Depends on |
| --- | --- | --- | --- |
| WP1 | Strategy backtest for the leaderboard | L | — |
| WP2 | Wealth plan: phases, goals, projection | L | — |
| WP3 | Portfolio analytics and rebalancing | M | WP2 (targets) |
| WP4 | Position sizing in the order ticket | S | — |
| WP5 | SDK capability `events` (dividends, splits, earnings dates) | M | — |
| WP6 | New investor nodes: insider activity, macro regime, portfolio check, events | L | WP3, WP5 |
| WP7 | New playbooks | S | WP6 |
| WP8 | New plugins: CoinGecko and Stooq; `events` in Yahoo and Finnhub | M | WP5 |
| WP9 | Agent tools and the Command Center "Today" briefing; PWA install | M | WP1–WP6 |

S ≈ under 600 changed lines, M ≈ 600–1,500, L ≈ 1,500+ including tests.

---

## WP1. Strategy backtest for the leaderboard

**Why.** A ranking nobody has tested is a guess. Investors should see how a
strategy would have done, monthly rebalanced, against a benchmark, with the
biases stated, before they automate it. This is the release gate the
roadmap sets for "P2 Investment ranking".

### Backend

New package `internal/backtest` (pure, no HTTP, no DB):

```go
// Package backtest replays a ranking strategy over past prices and reported
// quarters with monthly rebalancing. Inputs are point in time: a quarter is
// only used once it would have been published.
package backtest

type Config struct {
	Start         time.Time // first rebalance, first trading day on/after
	End           time.Time
	TopN          int     // 1–25 holdings, equal weight
	RebalanceDays int     // 21 (monthly) or 63 (quarterly); default 21
	CostBps       float64 // per side, default 10
	ReportLagDays int     // default 60: a quarter counts from period_end+lag
}

// Scorer ranks the symbols as of a date. The server passes a closure over
// leaderboardMetricsRow + scoreLeaderboard so backtest stays independent of
// the server package.
type Scorer func(asOf time.Time, series map[string]market.HistorySeries,
	fin map[string]market.FinancialsResponse) []string // symbols, best first

type Rebalance struct {
	Date        string   `json:"date"`
	Holdings    []string `json:"holdings"`
	Turnover    float64  `json:"turnover_pct"`
	FundCovPct  float64  `json:"fundamentals_coverage_pct"`
}

type Point struct {
	Date      string  `json:"date"`
	Equity    float64 `json:"equity"`     // starts at 100
	Benchmark float64 `json:"benchmark"`  // starts at 100, absent if none
}

type Metrics struct {
	CAGRPct        float64  `json:"cagr_pct"`
	VolatilityPct  float64  `json:"volatility_pct"`   // annualized, daily returns × √252
	SharpeRatio    float64  `json:"sharpe"`           // (CAGR − RiskFreePct) / vol, RiskFree = 0 unless given
	SortinoRatio   float64  `json:"sortino"`
	MaxDrawdownPct float64  `json:"max_drawdown_pct"` // negative number
	CalmarRatio    float64  `json:"calmar"`
	HitRatePct     float64  `json:"hit_rate_pct"`     // % of rebalance periods beating the benchmark
	AvgTurnoverPct float64  `json:"avg_turnover_pct"`
	BenchCAGRPct   *float64 `json:"benchmark_cagr_pct,omitempty"`
	ExcessCAGRPct  *float64 `json:"excess_cagr_pct,omitempty"`
}

type Result struct {
	Config     Config      `json:"config"`
	Metrics    Metrics     `json:"metrics"`
	Curve      []Point     `json:"curve"`      // one point per trading day, thinned to ≤ 400 points
	Rebalances []Rebalance `json:"rebalances"`
	Warnings   []string    `json:"warnings"`
}

func Run(cfg Config, series map[string]market.HistorySeries,
	fin map[string]market.FinancialsResponse, bench *market.HistorySeries,
	score Scorer) (Result, error)

// PointInTime returns the series cut at asOf and the reports whose
// period_end + lag ≤ asOf. Exported for tests and for the server's Scorer.
func PointInTime(asOf time.Time, s market.HistorySeries, f market.FinancialsResponse,
	lagDays int) (market.HistorySeries, market.FinancialsResponse)
```

Algorithm for `Run`:

1. Build the trading calendar from the union of daily bar dates of all
   series (use `dailyBars`-style one-bar-per-day normalization; copy that
   helper into the package rather than importing `server`).
2. Starting at the first calendar day ≥ `Start` where at least `TopN`
   symbols have ≥ 260 bars before it, rebalance every `RebalanceDays`
   trading days.
3. On a rebalance day `d`: call `score(d, cut series, cut financials)` with
   data from `PointInTime` for each symbol. Take the first `TopN`. Weights
   are equal. Turnover = Σ|w_new − w_old| / 2. Equity is multiplied by
   `1 − turnover × 2 × CostBps/10000`.
4. Between rebalances, daily portfolio return = Σ w_i × r_i, where weights
   drift with prices (buy-and-hold inside the period). A symbol with no bar
   on a day keeps its last close (return 0).
5. Benchmark is held 100 % with no costs.
6. Metrics from the daily equity curve. CAGR = (E_end/E_start)^(252/days) − 1.
   Sortino uses downside deviation of daily returns below 0. Max drawdown is
   the largest peak-to-trough fall. Round with `round2f`-equivalent.
7. Warnings (always included when true):
   - `"survivorship_bias"`: always, text: "The universe is today's list of
     companies. Companies that fell out of it are missing, which flatters
     past returns."
   - `"price_index_benchmark"`: benchmark has no dividends.
   - `"thin_fundamentals"`: average `fundamentals_coverage_pct` < 60 —
     "Few reported quarters were available for the early years, so early
     picks lean on price factors."
   - `"short_history"`: fewer than 24 rebalances.
   - Return an error (not a warning) when fewer than 6 rebalances fit.

Server wiring, new file `internal/server/backtest.go`:

- Add `Benchmark string` to `leaderboardUniverse`: `us_mega`, `us_tech`,
  `us_dividend` → `SPY`; `se_large` → `^OMX`; `eu_leaders` → `^STOXX50E`.
  Expose it in the universes JSON, update `TestLeaderboardOptionsMatchFrontend`
  and the frontend mirror.
- `func (e *Engine) leaderboardScorer(strategy leaderboardStrategy) backtest.Scorer`:
  for each symbol builds `leaderboardMetricsRow(sym, cutSeries, &cutFin, asOf)`,
  skips errors, calls `scoreLeaderboard`, returns symbols by `rank`.
  `fundamentals_coverage_pct` = share of symbols whose cut financials have
  ≥ 4 reports; compute it inside the scorer closure and report it through a
  small struct the closure captures, or change `Scorer` to also return the
  coverage — choose the second (`func(...) ([]string, float64)`), it is
  simpler.
- Data: history with `market.Range5Y` through `e.flowHistory` (it caches) and
  `e.GetFinancials` for each symbol, fetched with bounded concurrency 4 (copy
  the worker pattern in `leaderboardMetrics`). Benchmark history the same way;
  a failed benchmark is a warning, not an error.
- Route `GET /api/market/backtest?universe=&strategy=&top=10&rebalance=monthly|quarterly&cost_bps=10&years=3|5`
  (or `symbols=` up to 25, same parsing as the leaderboard via
  `leaderboardSymbols`). Authorization identical to the leaderboard route
  (`research.read`). Cache results 6 h in a map keyed by the normalized
  query, max 16 entries, reusing the leaderboard cache-eviction approach.
  Deduplicate concurrent identical requests like `leaderboardCalls`.
- Register in `NewEngine` next to `registerLeaderboardRoutes`.

### Frontend

- `models.ts`: `BacktestResult`, `BacktestMetrics`, `BacktestPoint`.
- `services.ts`: `backtest(params)` → `GET /api/market/backtest`.
- New pure helper `components/backtest-view.ts` (+ `backtest-view.spec.ts`):
  `curvePath(points, width, height, key)` → SVG path string (scales to the
  min/max of both series), `metricTiles(metrics)` → `{label, value, tone}[]`,
  `warningText(id)`.
- New component `components/backtest-panel.ts`: inputs `universe`,
  `strategy`; a "Backtest this strategy" button on the Markets leaderboard
  card (`markets-page.html`) opens it inline under the table. Shows: metric
  tiles (CAGR, excess vs benchmark, max drawdown, Sharpe, hit rate,
  turnover), an inline SVG line chart (strategy vs benchmark, two colours
  **and** a dashed line for the benchmark, legend, `role="img"` with an
  `aria-label` summarizing the result), a collapsible table of rebalances,
  and the warnings as a Bootstrap `alert-warning` list. Controls: top N
  (5/10/15), monthly/quarterly, 3Y/5Y.
- Loading state: skeleton tiles; error state: the server message.

### Tests

- `internal/backtest/backtest_test.go`:
  - `TestPointInTimeHidesUnpublishedQuarters`: a quarter with period_end
    2025-03-31 and lag 60 is invisible on 2025-05-29, visible on 2025-05-30.
  - `TestRunEqualWeightTwoSymbols`: synthetic series (A +1 %/day, B 0 %),
    TopN 1, scorer always picks A → CAGR matches the closed form within
    0.01, turnover only at the first rebalance.
  - `TestCostsReduceEquity`: same run with `CostBps` 0 vs 50.
  - `TestMaxDrawdownAndSortino` on a hand-computed curve.
  - `TestTooShortHistoryErrors`, `TestWarningsAlwaysIncludeSurvivorship`.
- `internal/server/backtest_test.go`: endpoint with a fake provider (copy
  the fake used in `leaderboard_test.go`): 200 with metrics, 400 on bad
  `top`, cache hit returns the same object.
- `backtest-view.spec.ts`: path generation, tile tones, empty curve.

### Docs

New section "Backtest" in `docs/investor-automations.md` after "Leaderboard":
method, point-in-time rule, the four warnings, and that past results do not
predict future ones. Add the route to `docs/api-reference.md`.

### Acceptance

- Markets → Leaderboard → "Backtest this strategy" shows a 5-year
  monthly-rebalanced result for US mega caps in under 30 s on a warm cache.
- No quarter is ever used before `period_end + 60 days` (unit-tested).
- Survivorship warning is always visible.

---

## WP2. Wealth plan: phases, goals and projection

**Why.** "Getting rich" happens in phases with different jobs. A plan page
tells each investor which phase they are in, what that phase needs, a
target allocation, and how likely their goal is, and it drives the rest of
the app (portfolio drift in WP3, phase playbooks in WP7, the briefing in WP9).

### Phases (deterministic classifier)

Inputs (all per user, all optional except `monthly_expenses`):

| Field | Type | Meaning |
| --- | --- | --- |
| `monthly_expenses` | number | Living costs per month |
| `emergency_fund` | number | Cash set aside, not invested |
| `high_interest_debt` | number | Debt above 8 % APR |
| `invested` | number | Invested assets today (or use the paper portfolio equity when `use_paper_portfolio` is true) |
| `monthly_contribution` | number | Added each month |
| `age` | int | 16–100 |
| `retirement_age` | int | > age, ≤ 100 |
| `goal_amount` | number | Optional; default = FI number |
| `risk_profile` | `conservative \| balanced \| growth \| aggressive` | |
| `retired` | bool | Already living off the portfolio |
| `currency` | string | 3-letter code, display only |

FI number = `monthly_expenses × 12 × 25` (4 % rule; label it as a rule of
thumb). FI ratio = `invested / FI number`.

Phase, checked top to bottom, first match wins:

| ID | Name | Condition | Job of the phase |
| --- | --- | --- | --- |
| `foundation` | Foundation | `high_interest_debt > 0` or `emergency_fund < 3 × monthly_expenses` | Pay off expensive debt, build 3–6 months of buffer. Investing is optional. |
| `income` | Income | `retired` | Withdraw sustainably (3.5–4 %), keep 2 years of spending in cash/bonds |
| `preservation` | Preservation | FI ratio ≥ 1 or `retirement_age − age ≤ 5` | Protect what is built: lower drawdown, glide toward bonds |
| `starter` | Starter | FI ratio < 0.10 | Build the habit: automate contributions into broad index funds |
| `accumulation` | Accumulation | FI ratio < 0.50 | Raise the savings rate, keep costs low, add a small satellite |
| `acceleration` | Acceleration | otherwise (0.50–1.00) | Compounding does the heavy lifting; rebalance, tax efficiency, concentration checks |

Target allocation (core equity / bonds / cash / satellite), percent. The
satellite is the only place for high-risk picks (momentum leaders, small
caps, crypto); it is capped and labelled.

| Phase \ profile | conservative | balanced | growth | aggressive |
| --- | --- | --- | --- | --- |
| foundation | 0/0/100/0 | 0/0/100/0 | 0/0/100/0 | 0/0/100/0 |
| starter | 50/40/10/0 | 70/25/5/0 | 85/10/5/0 | 85/5/0/10 |
| accumulation | 50/40/10/0 | 65/25/5/5 | 80/10/0/10 | 80/5/0/15 |
| acceleration | 45/45/10/0 | 60/30/5/5 | 75/15/0/10 | 75/10/0/15 |
| preservation | 30/55/15/0 | 45/45/10/0 | 55/35/5/5 | 60/30/5/5 |
| income | 25/50/25/0 | 40/45/15/0 | 50/35/10/5 | 55/30/10/5 |

Each phase also has a fixed checklist (3–5 items, strings in Go) and a list
of recommended playbook IDs (from `investor-nodes.ts`, including the new
ones from WP7), for example `starter → ["phase-checkin","morning-brief"]`,
`accumulation → ["top10-buy-now","value-bargains","phase-checkin"]`,
`acceleration → ["rebalance-check","watchlist-health","drawdown-guard"]`,
`preservation → ["drawdown-guard","rebalance-check","dividend-calendar"]`,
`income → ["dividend-calendar","drawdown-guard"]`.

### Projection (Monte Carlo, seeded)

Capital-market assumptions as named constants in Go, shown on the page as
"assumptions" with a short note that they are long-run estimates:

| Asset | Expected nominal return | Volatility |
| --- | --- | --- |
| Core equity | 7.0 % | 16 % |
| Bonds | 3.5 % | 6 % |
| Cash | 2.5 % | 1 % |
| Satellite | 8.0 % | 35 % |

Correlations: equity–bond 0.2, equity–satellite 0.7, others 0. Inflation
2.5 %. Portfolio μ = Σ w·μ; σ = √(wᵀΣw).

- 2,000 paths, monthly steps, from today to `retirement_age` (or 30 years
  when `retired`/no age): monthly log-return ~ N((μ − σ²/2)/12, σ/√12).
  Contribution added at the end of each month (none in `income`; in
  `income` withdraw `monthly_expenses` instead, inflation-indexed).
- RNG: `rand.New(rand.NewPCG(seed, 0x5eed))` from `math/rand/v2`; seed from
  the request (default 1) so results are reproducible and testable.
- Output per year: p10, p50, p90 in nominal and real (deflated) money.
- `probability_goal_pct`: share of paths ≥ goal (real) at the horizon.
  In `income`: share of paths that never hit 0.
- `years_to_goal_p50`: first year the median real path reaches the goal,
  absent if never.
- `required_contribution_75`: monthly contribution that gives ≥ 75 %
  probability, by bisection between 0 and 20 × current contribution
  (or 10 × monthly_expenses when contribution is 0), 25 iterations, 500
  paths per probe with the same seed. Absent in `income`.
- Milestones: first year the p50 real path crosses 10k, 100k, 250k, 500k,
  1M, and the FI number (only those above `invested`).

### Backend

- New package `internal/planner`:

```go
// Package planner classifies an investor's phase, proposes a target
// allocation and projects outcomes with a seeded Monte Carlo simulation.
// It gives a plan to discuss, not advice.
package planner

type Profile struct { /* fields from the table, json snake_case, validate() */ }
type Allocation struct{ CoreEquity, Bonds, Cash, Satellite float64 } // percents, sum 100
type Phase struct {
	ID, Name, Job string
	Checklist     []string
	Playbooks     []string
}
type YearBand struct{ Year int; Age int; P10, P50, P90, RealP10, RealP50, RealP90 float64 }
type Plan struct {
	Phase                  Phase       `json:"phase"`
	Target                 Allocation  `json:"target_allocation"`
	FINumber               float64     `json:"fi_number"`
	FIRatioPct             float64     `json:"fi_ratio_pct"`
	Bands                  []YearBand  `json:"bands"`
	ProbabilityGoalPct     float64     `json:"probability_goal_pct"`
	YearsToGoalP50         *int        `json:"years_to_goal_p50,omitempty"`
	RequiredContribution75 *float64    `json:"required_contribution_75,omitempty"`
	Milestones             []Milestone `json:"milestones"`
	Assumptions            Assumptions `json:"assumptions"`
}

func (p Profile) Validate() error
func Classify(p Profile) Phase
func TargetAllocation(phase string, risk string) Allocation
func Project(p Profile, a Allocation, seed uint64, paths int) Plan
```

- Liquibase `033-investor-profile.xml`: table `investor_profile`
  (`user_id BIGINT PK`, FK to `auth_users(id)` with `onDelete CASCADE`,
  `profile MEDIUMTEXT` holding the profile JSON, `updated_at DATETIME(6)`).
- Storage `internal/storage/investor_profile.go`:
  `LoadInvestorProfile(ctx, userID) (string, error)` (empty string when
  none), `SaveInvestorProfile(ctx, userID, json string) error`.
- Routes in `internal/server/plan.go`, any signed-in user, own profile only:
  - `GET /api/plan` → `{ "profile": …|null, "plan": …|null }`
  - `PUT /api/plan/profile` → validates, stores, returns the plan
  - `POST /api/plan/preview` → plan for an unsaved profile (what-if sliders),
    rate-limited to 30/min per user (reuse the limiter pattern in
    `flow_event_adapters.go` if a generic one is not available)
  - When `use_paper_portfolio` is true and the user may read the paper
    portfolio, `invested` = `summary.Equity` from `portfolioState()`.
  - With `AUTH_ENABLED=false` store under user ID 0, like other single-user
    features do (check `currentUser`).

### Frontend

- Route `plan` ("Wealth plan"), page `pages/plan-page.ts` with sections:
  1. **Phase hero**: phase name, its job in one line, a 6-step phase track
     (Foundation → Starter → Accumulation → Acceleration → Preservation →
     Income) with the current step highlighted (text + `aria-current="step"`).
  2. **Profile form**: the inputs, with a "What if" mode where changing
     contribution, return profile or retirement age calls `/api/plan/preview`
     debounced 400 ms. Save button persists.
  3. **Projection chart**: inline SVG fan chart (p10–p90 band, p50 line),
     real vs nominal toggle, goal as a horizontal dashed line, milestones as
     labelled dots.
  4. **Tiles**: probability of goal, years to goal, FI number, required
     monthly contribution for 75 %.
  5. **Target allocation**: stacked bar + table; satellite row labelled
     "High-risk sleeve, capped at N %".
  6. **Checklist** and **Recommended automations** (buttons that open the
     playbook in Flow Studio, same hand-off the Command Center uses).
  7. Assumptions (collapsible) and the disclosure.
- Pure helpers `components/plan-view.ts` (+ spec): `fanPaths(bands, w, h, real)`,
  `phaseSteps(currentId)`, `formatMoney(value, currency)` (new; `ui/format.ts` has no money
  formatter, add it there with a spec case in `format.spec.ts`).
- Command Center: a small "Your phase" card linking to `/plan` (empty state:
  "Set up your plan").

### Tests

- `planner_test.go`: `TestClassifyOrder` (table-driven over every phase
  boundary, including debt overriding FI ratio), `TestTargetAllocationSumsTo100`
  for all 24 cells, `TestProjectDeterministicWithSeed`, `TestProjectZeroVolatility`
  (σ = 0 assumptions injected → closed-form future value of an annuity within
  0.5 %), `TestRequiredContributionMonotone`, `TestIncomeNeverNegative`,
  `TestValidateRejects` (age ≥ retirement age, negative money, NaN).
- `plan_test.go` (server): preview works without a DB; profile round trip is
  a MySQL test (CI-only).
- `plan-view.spec.ts`.

### Acceptance

- A new user sees the empty state, fills five fields and gets a phase,
  allocation and fan chart.
- Same inputs + seed → identical numbers (unit-tested).
- Foundation phase shows 100 % cash and no investing playbooks.

---

## WP3. Portfolio analytics and rebalancing

**Why.** Wealth is lost through concentration and drawdowns more than
through missed picks. Show the risk of the paper portfolio and how far it
drifted from the plan.

### Backend

New package `internal/portfolio`:

```go
// Package portfolio measures the risk of a set of holdings from daily
// closes: weights, concentration, volatility, beta, correlation, risk
// contribution and drift from a target allocation.
package portfolio

type Holding struct{ Symbol string; Value float64; Class string } // class: equity|bond|cash|satellite
type Analytics struct {
	TotalValue        float64            `json:"total_value"`
	Weights           map[string]float64 `json:"weights_pct"`
	TopWeightPct      float64            `json:"top_weight_pct"`
	HHI               float64            `json:"hhi"`               // Σ w², 0–1
	EffectiveHoldings float64            `json:"effective_holdings"` // 1 / HHI
	VolatilityPct     *float64           `json:"volatility_pct,omitempty"`
	Beta              *float64           `json:"beta,omitempty"`
	MaxDrawdown1YPct  *float64           `json:"max_drawdown_1y_pct,omitempty"` // current weights held for the past year
	Correlation       [][]float64        `json:"correlation"`  // same order as Symbols
	Symbols           []string           `json:"symbols"`
	RiskContribution  map[string]float64 `json:"risk_contribution_pct"` // w_i(Σw)_i / σ²
	ClassWeights      map[string]float64 `json:"class_weights_pct"`
	Drift             map[string]float64 `json:"drift_pct,omitempty"` // class weight − target
	Suggestions       []Suggestion       `json:"suggestions"`
	Alerts            []string           `json:"alerts"`
}
type Suggestion struct {
	Symbol string  `json:"symbol"`
	Side   string  `json:"side"`   // buy | sell
	Amount float64 `json:"amount"` // money
	Reason string  `json:"reason"`
}

func Analyze(h []Holding, closes map[string][]market.HistoryPoint,
	bench []market.HistoryPoint, target *planner.Allocation, cash float64) Analytics
func ClassOf(symbol string) string
```

Rules:

- Returns: daily log returns over the last 252 aligned trading days (only
  days where every holding and the benchmark have a bar). Fewer than 60
  aligned days → leave volatility, beta, drawdown, correlation absent and
  add alert `"short_history"`.
- Beta = cov(r_p, r_b) / var(r_b).
- `ClassOf`: bond ETF allowlist (`BND, AGG, TLT, IEF, SHY, BNDX, VGIT, VCIT,
  LQD, TIP, GOVT, SCHZ, IUSB`), crypto pairs (`*USDT`, `*-USD`, symbols from
  the BINANCE or COINGECKO provider) → `satellite`, everything else `equity`;
  free cash → `cash`.
- Alerts: `top_weight_pct > 25` → `"concentration"`; `effective_holdings < 5`
  with ≥ 5 positions → `"low_diversification"`; any pair correlation > 0.85 →
  `"highly_correlated:<A>/<B>"`; drawdown < −20 → `"deep_drawdown"`;
  satellite above its target cap → `"satellite_over_cap"`.
- Suggestions (only with a target): for each class outside a ±5-point band,
  move it back to target. Sells come from the largest positions of an
  overweight class; buys go to the existing holdings of an underweight class
  pro rata, or a placeholder symbol `"<core index fund>"` when the class has
  none. Round amounts to whole money units. Suggestions are text; nothing is
  ordered.

Server `internal/server/portfolio_analytics.go`:
`GET /api/portfolio/analytics` (same authorization as `GET /api/positions`):
holdings from `portfolioState()` positions (`MarketValue`), `cash` from the
summary, 1Y history via `e.flowHistory`, benchmark `SPY`, target from the
user's plan if any.

### Frontend

Portfolio page: new "Risk" tab beside positions: tiles (total, volatility,
beta, max drawdown, effective holdings), class-weight vs target bars with the
drift number, a correlation heatmap as a `<table>` (cell text shows the
value, colour is secondary), risk contribution bars, alerts as a list, and
suggestions with a **"Prefill ticket"** button that fills the existing order
ticket (symbol, side, quantity = floor(amount / last price)) without
submitting. Helpers in `components/portfolio-risk.ts` (+ spec).

### Tests

`portfolio_test.go`: two perfectly correlated series → correlation 1;
identical-to-benchmark holding → beta 1; HHI of four equal weights = 0.25;
drift and suggestions with a 60/40 target; short history leaves fields
absent. Server test with a fake provider and an in-memory paper state.

### Acceptance

A portfolio of 70 % in one stock shows the concentration alert and a sell
suggestion when a plan exists; with no plan there are no suggestions.

---

## WP4. Position sizing in the order ticket

**Why.** Size decides survival. Let the ticket size a trade from the risk
the investor accepts.

- `internal/trading/sizing.go`:

```go
type SizeRequest struct {
	Equity     float64 `json:"equity"`
	RiskPct    float64 `json:"risk_pct"`     // 0.1–5, default 1
	Entry      float64 `json:"entry"`
	Stop       float64 `json:"stop,omitempty"`
	ATR        float64 `json:"atr,omitempty"`
	ATRMult    float64 `json:"atr_mult,omitempty"` // default 2
	MaxWeight  float64 `json:"max_weight_pct,omitempty"` // default 20
	Side       string  `json:"side"` // buy | sell (short)
}
type SizeResult struct {
	Quantity     float64 `json:"quantity"`
	Stop         float64 `json:"stop"`
	RiskAmount   float64 `json:"risk_amount"`
	PositionPct  float64 `json:"position_pct"`
	CappedBy     string  `json:"capped_by,omitempty"` // max_weight | buying_power
}
func Size(req SizeRequest, buyingPower float64) (SizeResult, error)
```

  Stop = given stop, else entry ∓ ATRMult × ATR. Per-share risk = |entry −
  stop| (> 0 or error). Qty = floor((equity × risk%) / per-share risk), then
  capped by max weight and buying power. Crypto (fractional) symbols round
  to 8 decimals instead of floor to integer — reuse `round8`.
- `POST /api/trading/size`: fills `equity` and buying power from the summary,
  ATR on 3M of daily bars when no stop is given. `internal/market/technical.go`
  only has the unexported `atrPct`; add an exported
  `func ATR(points []HistoryPoint, period int) (float64, bool)` next to it and
  make `atrPct` use it.
- Ticket UI: a "Size by risk" toggle with a risk % input; shows "Risk 1.0 % =
  $1,000 · stop $182.40 (2× ATR) · 12 % of equity" and fills quantity.
- Tests: table tests for long/short, ATR stop, both caps, zero distance
  error, fractional rounding.

---

## WP5. SDK capability `events` (dividends, splits, earnings dates)

**Why.** Income investors need dividend dates; everyone needs to know when
earnings are due.

In this repository:

- `pluginsdk/protocol.go`: `MethodEvents = "events"`, `CapEvents = "events"`,
  add to `knownCapabilities`.
- `pluginsdk/types.go`:

```go
// CorporateEvent is a dividend, split or earnings date. Date is "2026-11-14".
// Amount is the dividend per share or nil; Ratio is "4:1" for splits.
// Confirmed is false for estimated earnings dates.
type CorporateEvent struct {
	Symbol    string   `json:"symbol"`
	Kind      string   `json:"kind"` // dividend | split | earnings
	Date      string   `json:"date"`
	Amount    *float64 `json:"amount,omitempty"`
	Currency  string   `json:"currency,omitempty"`
	Ratio     string   `json:"ratio,omitempty"`
	Confirmed bool     `json:"confirmed"`
	Provider  string   `json:"provider,omitempty"`
}
```

  Request params: `{symbol, from, to}` (ISO dates, window ≤ 400 days).
  Add the handler hook in `serve.go` the same way `News` was added
  (commit `8a6fbcf` is the template: follow every file it touched, including
  `testdata/contract` golden files and `plugintest` conformance checks).
- Backend mirror: `market.CorporateEvent`, `Backend.Events(ctx, p, symbol,
  from, to)` with `NoProviders` returning not-installed, the plugin
  backend call in `internal/plugins/backend.go`, and the capability in
  `docs/provider-capabilities.md` and `docs/plugins.md`.
- `GET /api/events?symbol=&from=&to=&provider=` (research.read).
- Financials page: an "Upcoming" strip (next earnings, next ex-dividend,
  trailing-12-month dividend total and yield = Σ dividends / last price).

Tests: manifest validation accepts `events`; contract golden payload;
backend routes a call to a fake plugin (`plugintest`).

---

## WP6. New investor nodes

All four follow rule 0.6. Settings limits go in `validateInvestorNode`.

### 6a. `insider_activity` (non-admin OK)

- `internal/sec/form4.go`:
  `func ParseForm4(xml []byte) ([]InsiderTrade, error)` reading
  `nonDerivativeTable/nonDerivativeTransaction`: `transactionDate`,
  `transactionCoding/transactionCode` (`P` = open-market buy, `S` = sale;
  ignore others), `transactionAmounts/transactionShares`,
  `transactionPricePerShare`, `reportingOwner/reportingOwnerRelationship`
  (`isDirector`, `isOfficer`, `officerTitle`).
  `func (c *Client) InsiderTrades(ctx, ticker string, lookbackDays, maxFilings int) ([]InsiderTrade, error)`
  lists Form 4 filings via `Filings`, fetches each primary XML through the
  existing `get` (it applies the user agent and rate limit), max 20 filings.
  Find the XML document name from the filing index JSON
  (`/Archives/edgar/data/<cik>/<acc>/index.json`, first `.xml` that is not
  the `FilingSummary`). Golden fixture tests with two real-shaped XMLs in
  `internal/sec/testdata/`.
- Row per symbol: `buy_count`, `sell_count`, `buyers` (distinct owners),
  `net_shares`, `net_value`, `largest_buy_value`, `largest_buyer`,
  `officer_buy` (bool), `cluster_buy` (≥ 3 distinct buyers in the window),
  `latest`, `url` (latest filing). Settings: `symbols`, `lookback_days`
  (default 30, max 90). Needs `SEC_USER_AGENT` (same error text as
  `sec_filings`).

### 6b. `macro_regime` (non-admin OK)

- Reads FRED series through the FRED plugin (`provider` = `FRED`,
  history `5Y`): `T10Y2Y` (10y–2y spread), `UNRATE`, `FEDFUNDS`,
  `BAMLH0A0HYM2` (high-yield spread), `VIXCLS`.
- One output row with each latest value, its 3-month change, and:
  - `curve_inverted` = T10Y2Y < 0; `curve_uninverting` = was < 0 within the
    last 12 months and now ≥ 0 (historically the riskier moment).
  - `sahm_rule` = 3-month average UNRATE − minimum of the 3-month averages
    over the previous 12 months ≥ 0.5.
  - `credit_stress` = high-yield spread ≥ 5 or up ≥ 1.0 in 3 months.
  - `vol_regime` = `calm` (VIX < 15), `normal` (< 25), `stressed`.
  - `regime` = `risk_off` if two or more of (curve_uninverting, sahm_rule,
    credit_stress, vol_regime == stressed), `caution` if one, else `risk_on`.
  - `reasons`: short strings for every triggered signal.
- When FRED is not installed, fail with "Install the FRED plugin
  (Settings → Plugins) and set FRED_API_KEY." A missing series leaves its
  fields absent and is listed in `missing`.
- Pure `macroRegime(series map[string]market.HistorySeries, now) map[string]any`
  with table tests.

### 6c. `portfolio_check` (admin only, like the paper portfolio)

- Output: one row per alert/suggestion from WP3's `Analyze` plus a summary
  row (`kind: "summary"`, total, volatility, drawdown, drift per class), and
  `drawdown_from_peak_pct` = equity / peak − 1, with the peak from the
  `paper_equity_peak` table (Liquibase 009, read in
  `internal/storage/paper_equity_peak.go`).
- No settings beyond `skip_empty`. Not in `flowAllowedForNonAdmin`.

### 6d. `corporate_events` (non-admin OK, needs WP5)

- Rows from `Backend.Events` for `symbols`, window `lookback_days` back and
  `limit` days ahead (reuse the existing fields: document that for this node
  `limit` is "days ahead", max 90), one row per event with `id` =
  `symbol|kind|date` so Only-new works without key fields, and `days_until`.

### Frontend

Add the four specs to `INVESTOR_NODES` with tags `⚑ INSIDERS`, `◎ MACRO`,
`◔ PORTFOLIO`, `☷ EVENTS`; field suggestions for the Screen node's rule
editor come from the same row-field lists (find where the existing nodes
declare their output fields for suggestions and extend it).

### Tests

Server: one test per node with fakes (`e.secFilings`-style seam for insider
trades: add `insiderTrades func(...)` next to `secFilings` in `Engine`),
validation tests for bad settings, a playbook-graph test per node type.

---

## WP7. New playbooks

Add to `PLAYBOOKS`/`buildPlaybook`, the server playbook test and the docs.
Admins get Notify, others Output (existing behaviour).

| ID | Label | Graph | Schedule |
| --- | --- | --- | --- |
| `insider-cluster` | Insider buying clusters | Insider activity (watchlist) → Screen `cluster_buy or (officer_buy and net_value > 250000)` → Only new (14 days) → table → Notify | Daily |
| `macro-regime` | Macro regime shift | Macro regime → Only new, key `regime` → agent "explain what changed and what it usually means for a long-term investor, grounded" → Notify | Daily |
| `drawdown-guard` | Portfolio drawdown guard | Portfolio check → Screen `drawdown_from_peak_pct <= -10 or kind == "deep_drawdown"` → Only new (72 h) → table → Notify | Hourly, admin only |
| `rebalance-check` | Monthly rebalance check | Portfolio check → Screen `kind == "suggestion"` → table → agent rebalance notes (grounded) → Notify | Monthly (first weekday), admin only |
| `dividend-calendar` | Dividend & earnings calendar | Corporate events (30 days ahead) → Only new → table → Notify | Weekly |
| `phase-checkin` | Monthly phase check-in | **`wealth_plan`** node (new, non-admin: the flow owner's saved plan, output = the `Plan` JSON) → agent "progress note: phase, probability of goal, next checklist item" (grounded) → Output/Notify | Monthly |
| `earnings-week` | Earnings this week, scored | Corporate events (kind earnings, 7 days) → Leaderboard of those symbols → table → memo | Weekly |
| `strategy-scorecard` | Strategy scorecard | `backtest` node (new, non-admin: universe, strategy, top, years; output = metrics row per strategy when `strategy` = `all`) → table → agent comparing strategies with the bias warnings → Output | Monthly |

`wealth_plan` and `backtest` are two small extra nodes; implement them in
this WP following rule 0.6 (they call WP2's `Project` and WP1's `Run`). The
backtest node is expensive: allow at most one per graph and set its timeout
to 120 s.

Update the Markets `STRATEGY_PLAYBOOK` map only if a strategy gets a better
playbook (no change required).

---

## WP8. New plugins (repository `floweysales-plugins`)

Follow the existing plugin layout (`<id>/main.go`, `main_test.go`,
`plugin.json`), register in `scripts/build-plugins.sh` and the store index
generator, and add to `manifests_test.go`. Use `pluginsdk/httpx` for HTTP
(timeouts, retries, user agent) and `plugintest` for conformance.

1. **`coingecko`** (official free API, optional `COINGECKO_API_KEY` demo key
   sent as `x-cg-demo-api-key`): capabilities `quote history search`.
   Symbols are CoinGecko IDs (`bitcoin`, `ethereum`); search maps names to
   IDs. `quote` → `/simple/price?vs_currencies=usd&include_24hr_change=true`;
   `history` → `/coins/{id}/market_chart?vs_currency=usd&days=…` (daily
   points become OHLC with o=h=l=c, say so in `notes`). `min_poll_seconds`
   60, `cache_ttl_seconds` 60. Notes: rate limits.
2. **`stooq`** (free daily CSV, no key, `terms` required because it is an
   unofficial interface): capabilities `history quote`. Maps Yahoo-style
   tickers: `AAPL` → `aapl.us`, `VOLV-B.ST` → not supported (return a clear
   error), indices `^SPX`. Purpose: a fallback when Yahoo is rate-limited;
   the plugin must say `provider: "STOOQ"` in every series.
3. **`events` capability in `yahoo`**: dividends and splits from the chart
   endpoint with `events=div|split` (extend `internal/yahooapi`, add
   `ParseEvents` with a fixture test). Earnings dates are not reliable
   there; leave them out.
4. **`events` capability in `finnhub`**: earnings from `/calendar/earnings`
   (`confirmed` = has `hour` or is within 7 days of a reported date) and
   dividends from `/stock/dividend2` if the key's plan allows it; skip with
   a note otherwise.

Bump versions (minor), update the plugins README table and the site page.
Order: merge WP5 in floweysales → bump the SDK pin in the plugins repo →
this WP.

---

## WP9. Agent tools, "Today" briefing and installable app

### Agent tools (`internal/ai/tools.go`)

Add read-only tools, each through an optional interface on the data source
(rule 0.8), implemented by the engine's agent data source:

| Tool | Args | Returns |
| --- | --- | --- |
| `get_leaderboard` | `universe`, `strategy`, `symbols`, `limit` | Ranked rows (the existing endpoint's rows) |
| `run_strategy_backtest` | `universe`, `strategy`, `top`, `years` | WP1 `Result` without the curve (metrics, rebalances count, warnings) |
| `get_wealth_plan` | — | The signed-in user's plan (phase, target, probability), or "no plan yet" |
| `get_portfolio_analytics` | — | WP3 analytics (admin sessions only; others get a refusal string) |
| `get_insider_activity` | `symbol`, `lookback_days` | WP6a row + trades |
| `get_macro_regime` | — | WP6b row |
| `get_corporate_events` | `symbol`, `days_ahead` | WP5 events |

Add each to the agent evaluation fixtures (`internal/eval`, see
`docs/agent-evaluation.md`) with at least one grounded question, The MCP server
(`internal/mcp/mcp.go`) already advertises `ai.ToolDefs()`, so the tools
reach MCP clients automatically; make sure each new tool degrades to a clear
error string when the MCP data source does not implement its interface.

### Command Center "Today" briefing

A card at the top of the Command Center, built only from deterministic data
(no model call on page load):

- Phase chip and probability of goal (WP2), link to the plan.
- Macro regime chip (WP6b) when FRED is installed.
- Portfolio: day change, drawdown from peak, top alert (WP3), admin only.
- "3 things worth a look": top new leaderboard entrant, the largest insider
  buy on the watchlist, the next earnings date on the watchlist.
- One button "Brief me" sends these figures to the agent dock with a
  grounded prompt (existing hand-off pattern on the Markets page).

New endpoint `GET /api/today` assembling it with a 10-minute per-user
cache; each part is optional and fails soft (absent part, never a 500).

### Installable app (PWA)

- `ng add @angular/pwa` equivalent by hand (no network in CI for schematics):
  add `@angular/service-worker` at the same version as `@angular/core`,
  `ngsw-config.json` caching the app shell and fonts only (**never** cache
  `/api/*`), `manifest.webmanifest` with name, icons (reuse
  `frontend/public` assets), `theme_color` from the CSS tokens, and
  `provideServiceWorker('ngsw-worker.js', { enabled: !isDevMode(),
  registrationStrategy: 'registerWhenStable:30000' })` in `app.config.ts`.
- Serve `ngsw-worker.js` and the manifest with `Cache-Control: no-cache` in
  `internal/server/website.go`.
- Smoke: `smoke-responsive.mjs` checks the manifest link exists.

### Docs

README workspace table: rows for Wealth plan, Backtest, Portfolio risk and
Today; a line on the new nodes and plugins. `docs/roadmap.md`: mark the
backtest gate as delivered for P2 ranking and position sizing/approvals
progress for P3.

---

## 3. Definition of done (every WP)

- [ ] Go: `go vet ./...` and `go test ./...` pass; new packages have tests
      for every rule in their section.
- [ ] Frontend: build passes; new pure helpers have specs **listed in CI**.
- [ ] New pages pass `smoke:a11y` (light and dark) and `smoke:responsive`.
- [ ] Docs updated in the files named in the WP.
- [ ] Every new figure has a source (provider, SEC, FRED, or a stated
      assumption); absent data stays absent.
- [ ] No feature places an order or sends a message without the existing
      approval and side-effect gates.
- [ ] The PR body lists what ran only in CI (MySQL tests).
