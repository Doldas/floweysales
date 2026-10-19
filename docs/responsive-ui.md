# Responsive UI – phones and tablets

FloweySales uses responsive Angular Material breakpoints with **no changes to users,
roles, server settings, MySQL or trading data**. Layouts preserve existing actions.

| Viewport | Layout |
| --- | --- |
| <=340 CSS px | One-column KPI and controls for ultra-narrow phones |
| 341–390 CSS px | Galaxy S24 class portrait: compact two-column KPIs, stacked inputs |
| 391–700 CSS px | Large phones: two-column KPI cards, wrapped chart controls |
| 701–1024 CSS px | Tablets: three KPI columns, header/action wrapping, ledger single column |
| >1024 CSS px | Desktop multi-column dashboard and full financial tables |

The phone assistant opens full-screen using `100dvh`, with keyboard-aware
flex layout, large input and OS safe-area insets. The launcher remains anchored
near the thumb zone when collapsed. Financials use labeled per-quarter cards;
the positions, orders and order history are labeled rows with all columns intact
rather than unreadable tiny tables. Chart range and study controls are
**horizontally scrollable within their own areas** so the main page never needs
sideways scrolling. All administrative controls remain available on narrow views.

Verification: Angular production build and Chromium `smoke:responsive` test run
in CI. Tests emulate Samsung S24 at 360px with devicePixelRatio 3, other phones
(320/384/600), tablets (768/1024), desktop (1440) and a separate USER research
session. They check page overflow, component bounds, chart dimensions, launcher,
mobile fullscreen chat, and role-specific screen visibility. Real-device test
on a Samsung S24 is still recommended, especially when the on-screen keyboard
is open or browser font scaling/accessibility settings are changed.
