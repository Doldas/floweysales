# Responsive UI: phones, tablets and desktop

FloweySales uses Bootstrap 5 breakpoints and CSS grid with **no changes to users,
roles, server settings, MySQL or trading data**. Every action stays available on
narrow screens.

| Viewport | Layout |
| --- | --- |
| < 576 CSS px | Phones: the agent dock is full screen, KPI cards in one or two columns, tables become labelled rows, search moves into Markets and the command palette |
| 576–991 CSS px | Large phones and portrait tablets: the sidebar becomes a slide-in drawer (hamburger in the top bar) |
| 992–1439 CSS px | Fixed sidebar; the agent dock overlays the page |
| ≥ 1440 CSS px | The agent dock docks beside the page instead of covering it |

The agent dock is pinned to the viewport edges, so its composer stays at the
bottom of the visible area. Financials show labelled per-quarter cards on phones. Chart
ranges, studies and tab bars scroll **within their own areas**, so the page never
scrolls sideways.

See [Workspace UX patterns](workspace-ux.md) for panes, density and the tablet and phone rules.

Verification: the Angular production build and the Chromium `smoke:responsive`
test run in CI. The test emulates a Samsung S24 (360 px, devicePixelRatio 3),
other phones (320/384/600), tablets (768/1024) and desktop (1440). It checks
every page for horizontal overflow, the drawer and sidebar, the Markets controls
and chart height, the paper portfolio, and the agent dock bounds. A separate USER
session checks research mode: no paper portfolio, history-based chart, and custom
financial year ranges. A test on a real Samsung S24 is still recommended,
especially with the on-screen keyboard open or font scaling enabled.

## Loading states

Use `fs-skeleton`, `fs-progress-status` and the phases in `ui/async-state.ts` instead of one-off spinners.

- Loading shows a shape that matches the content, so nothing jumps when it arrives. A refresh keeps the data it has.
- A load under 150 ms shows nothing, to prevent flicker.
- Every spinner has a label. A wait over 8 seconds says it is still working.
- A progress bar is determinate only when `done` and `total` are measured; the app never shows a percentage it cannot measure.
- A wait with a known timeout ends in a message and a retry, never a spinner forever.
- Motion only marks a state change, stays under 200 ms for transitions, and uses opacity and transform. Shimmer stops under `prefers-reduced-motion`.
- A long run that the server can count reports it: the client names the run with a `progress_id`, and `GET /api/market/backtest/progress?id=…` returns `{stage, done, total, finished}`. Today only the backtest batch does ("Backtesting · 3 of 12"). When the figure is missing the status shows the stage alone, never a zero. Counts are kept in memory for ten minutes.
