# Wealth plan

The **Wealth plan** page (`/plan`) tells an investor which phase of building
wealth they are in, what that phase needs, a target allocation and how likely
their goal is. Everything is computed in Go (`internal/planner`); nothing here
trades, promises returns or replaces advice.

## Phases

The first matching row wins. The FI number is `monthly_expenses × 12 × 25` (the
4 % rule of thumb) and the FI ratio is `invested / FI number`.

| Phase | When | Job |
| --- | --- | --- |
| Foundation | high-interest debt, or emergency fund under 3 months of expenses | Pay off expensive debt, build a buffer. Investing is optional. |
| Income | already retired | Withdraw 3.5–4 %, keep two years of spending in cash and bonds |
| Preservation | FI ratio ≥ 1, or 5 years or less to retirement | Protect what is built |
| Starter | FI ratio < 10 % | Automate contributions into broad index funds |
| Accumulation | FI ratio < 50 % | Raise the savings rate, add a small capped satellite |
| Acceleration | otherwise | Rebalance, tax efficiency, concentration checks |

Target allocations (core equity / bonds / cash / satellite) depend on the phase
and the risk profile; the satellite is the only place for high-risk picks and
never exceeds 15 %. Foundation is always 100 % cash.

## Projection

A seeded Monte Carlo simulation (2,000 paths, monthly steps) from today to the
retirement age, or 30 years. Capital-market assumptions are named constants
(`planner.DefaultAssumptions`) and shown on the page: core equity 7 % / 16 %,
bonds 3.5 % / 6 %, cash 2.5 % / 1 %, satellite 8 % / 35 % (expected return /
volatility), equity–bond correlation 0.2, equity–satellite 0.7, inflation 2.5 %.
Contributions stay the same in nominal money; in the income phase spending is
withdrawn and rises with inflation. The same profile and `seed` always give the
same numbers.

The result has per-year 10th / 50th / 90th percentile bands in nominal and
today's money, the probability of reaching the goal in today's money (in the
income phase: of never running out), the median year the goal is reached, the
monthly contribution that gives 75 % probability (found by bisection) and
milestones (10k, 100k, 250k, 500k, 1M and the FI number).

## API

All three routes are for the signed-in user's own plan only.

- `GET /api/plan` returns `{ "profile": …|null, "plan": …|null }`.
- `PUT /api/plan/profile` validates, saves and returns the plan.
- `POST /api/plan/preview` returns the plan for an unsaved profile (what-if);
  limited to 30 per minute per user.

`?seed=` fixes the random seed (default 1). With `use_paper_portfolio` set,
administrators get the paper account's equity as the invested amount. Without
authentication the single user is stored as user 0. The profile lives in the
`investor_profile` table (Liquibase `033`) with no foreign key, because user 0
has no account row; deleting an account removes its profile.
