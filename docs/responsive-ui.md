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

Verification: the Angular production build and the Chromium `smoke:responsive`
test run in CI. The test emulates a Samsung S24 (360 px, devicePixelRatio 3),
other phones (320/384/600), tablets (768/1024) and desktop (1440). It checks
every page for horizontal overflow, the drawer and sidebar, the Markets controls
and chart height, the paper portfolio, and the agent dock bounds. A separate USER
session checks research mode: no paper portfolio, history-based chart, and custom
financial year ranges. A test on a real Samsung S24 is still recommended,
especially with the on-screen keyboard open or font scaling enabled.
