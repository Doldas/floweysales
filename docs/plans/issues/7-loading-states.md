# Plan 7: Loading states

**Issue:** #203. It can start at any time. Plans 1, 4 and 5 feed its long-running cases.

## What happens today

- No shared loading component. Each page uses its own: Bootstrap spinners, Bootstrap `placeholder` (backtest panel), a private `.skeleton` in `forum.scss`, a striped bar and text on the
  leaderboard, and text such as "Working…" and "Waiting…" on teams.
- `.fs-empty` is a shared empty and error style, but it is used inconsistently.
- `styles.scss:317` already reduces all animation for `prefers-reduced-motion`, so individual components need no special handling.
- The topbar has a thin progress bar for market data only.
- Requests have no "taking longer than expected" stage. Team and chat runs have real status events; leaderboard and backtests report no progress.

## Work packages

### WP1: Shared components (S, `feature/ui-async-state`)

- **`asyncState()`**, a small pure helper: `idle`, `loading`, `slow` (after 8 s), `success`, `empty`, `error`, with last good data kept during a refresh. Tested with `node --test`.
- **`fs-skeleton`**: `line`, `block`, `kpi`, `table-rows`, sized with the same dimensions as the real content, so nothing jumps. Shimmer uses tokens and CSS only, and is calm in both themes.
- **`fs-async`**: wraps a section using the state helper and shows the right skeleton, then an empty or error state with a retry button. Sets `aria-busy`, and announces completion politely. Short loads under
  150 ms show nothing, to prevent flicker.
- **`fs-progress-status`**: stage text, optional determinate bar, and elapsed time. It never shows a percentage it can't measure.

**Rules, added to `docs/responsive-ui.md`:** loading shows a shape that matches content; every spinner has a label; a wait over 8 s says it's still working; a wait with a known timeout ends in a message and a retry;
motion only for state changes, under 200 ms, on opacity and transform.

### WP2: Roll out, most visible first (M, one PR per group, `feature/ui-async-<area>`)

| Group | Surfaces | Note |
| --- | --- | --- |
| 1 | Command Center, Markets, Leaderboard | Keep the table visible during refresh |
| 2 | Financials, World, Strategies, Wealth plan, Portfolio | |
| 3 | Flows list and run history, Inbox, Notifications, Settings, Files | |
| 4 | Agent chat and team | Mostly fine. Use Plan 2's status line, and add placeholders for team cards |

Each PR removes what it replaces, including the private forum skeleton.

### WP3: Progress for slow work (M, `feature/long-run-progress`)

- **Leaderboard and backtest:** real counts ("Scoring 12 of 30 companies") from an SSE or polling variant of the endpoint. Cached answers stay immediate.
- **Strategy runs:** stage text, and batch counts from Plan 5 WP2.
- **Flows:** live per-node state in the run view. It exists as records; **to verify** whether the view updates while running.
- **Timeouts:** each has a known limit, reaching the user as a message, never as a spinner forever.

## Tests

- Spec for state transitions, the slow stage, and keeping data during refresh.
- A Playwright smoke test over main pages with delayed responses: skeleton appears, no spinner remains after an error, and layout shift stays under a set value, measured with the browser's CLS API.
- Reduced motion emulated in Playwright: shimmer doesn't move.
- Light and dark screenshots of the skeletons.

## Acceptance mapping

| Criterion | WP |
| --- | --- |
| Immediate feedback and clear success, empty and error states | WP1, WP2 |
| Stage and progress where measurable | WP1, WP3 |
| No layout jumps where avoidable | WP1 |
| Reduced motion respected | Already global; tested here |
| Animation doesn't hurt readability | WP1 rules |
