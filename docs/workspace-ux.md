# Workspace UX patterns

FloweySales is an analyst's tool workspace. The design goal is **high information density with clarity**: nothing valuable is removed to make a screen look empty, and nothing critical is hidden. This page is the reference for how dense screens are built. Use it before adding a panel, table or chart; do not add one-off layouts.

## Information hierarchy

Every dense screen answers, in this order:

1. **Context:** which instrument, market, provider, as-of time (UTC) and active strategy or settings.
2. **Decision data:** the figures and the ranking, with units.
3. **Interpretation:** scores, insights and agent text, always marked as calculated or AI-written, never as reported.
4. **Uncertainty and warnings:** stale data, missing values (shown as a dash, never zero), biases. These stay visible; no fold or density setting hides them.
5. **Evidence and detail:** methodology, sources, calculations, raw tool input and output, export. One or two interactions away.

Data, interpretation, uncertainty and actions are visually and textually distinct (source badge, "calculated here", "not a prediction").

## Shared patterns

| Pattern | Component or rule | Used by |
| --- | --- | --- |
| Pane: titled region that folds, resizes and opens full screen | `ui/pane.ts` (`fs-pane`) | Markets chart and insights, leaderboard ranking |
| Point-of-use metadata | `[paneMeta]` slot of a pane: always visible, also when folded; `core/chart-meta.ts` builds the chart facts | Chart, ranking |
| Source popover | `ui/source.ts` (`fs-source`): opens on click or keyboard, never hover only | Leaderboard, backtests |
| Evidence drill-down | Button under the row (`Score breakdown`), `aria-expanded`, content in the page, not a new page | Leaderboard |
| Compact data table | `data-density='compact'` tokens in `styles.scss`; sticky header; `rowWindow` in `core/workspace-prefs.ts` for long lists | Leaderboard |
| Loading | `fs-skeleton`, `fs-progress-status` ([responsive-ui.md](responsive-ui.md)) | All |
| Per-user preferences | `core/workspace.service.ts`, saved in this browser under `fs.workspace` | Density, pane state |

Rules for a pane:

- A fold hides the body only. Title, summary line and `[paneMeta]` stay. The body stays rendered, so charts and inputs keep their state.
- `paneId` is stable (lower case, digits, dot, dash). State is saved per id; a corrupt saved value falls back to defaults.
- The splitter is a real `separator` with `aria-valuenow`; it works with arrow keys, Page Up and Page Down, Home and End and with a pointer. Double-click resets.
- Full screen is a focus mode: Escape leaves and focus returns to the button.
- Tables inside a sized pane scroll in a focusable, labeled group so keyboard users can scroll them.
- A pane is never the only way to reach information. Anything in a folded pane is one click away.

Density is a **user** choice (compact or comfortable), saved in the browser and applied as `data-density` on the document root. An administrator sets nothing for it. Compact changes spacing and type size only; it never drops a column, figure or warning.

## Responsive rules by task

| Viewport | Layout rule |
| --- | --- |
| 1440 px and wider | Multi-panel workstation. Sidebar, page and agent dock side by side. Panes can be enlarged or opened full screen. |
| 992 to 1439 px | Fixed sidebar; the agent dock overlays the page. Same panes. |
| Tablet, 576 to 991 px | The sidebar is a drawer. Content reflows into one or two columns; KPI cards wrap. Tables scroll inside their pane. Pane headers wrap, actions on a second line. |
| Phone, under 576 px | One column. Pane actions take their own row and the summary follows. The chart starts at 270 px (resizable). Tables scroll inside their pane. Full screen opens a pane over the whole viewport. Density and theme live in the More actions menu. |

Motion is limited to color transitions under 200 ms and stops under `prefers-reduced-motion`. No essential detail is hover-only. Contrast is checked with axe-core in both themes. See [responsive-ui.md](responsive-ui.md) for the phone checks.

## Audit of dense screens

| Screen | Density pain point | Priority task | Status |
| --- | --- | --- | --- |
| Markets chart | Fixed 380 px chart; source and bar facts not stated at the chart | Read price action with its source and as-of | Pane: resize, fold, full screen, facts always shown |
| Leaderboard | Long tables, inline evidence, methodology above the table | Compare companies, then inspect one | Ranking pane, sticky header, windowed long lists, metadata line, compact density |
| Strategies | Long result page with many sections | Judge a backtest with its assumptions | Next candidate for panes; assumptions already shown with the result |
| Agent and Flow traces | Tool input and output | Verify an answer | Collapsible evidence exists; next candidate for panes |
| Financials, Portfolio | Wide tables | Read statements per period | Phone cards exist; next candidate |

New dense screens use `fs-pane` and the point-of-use metadata slot from the start.

## Desktop usability assessment

Measured with the browser tests in `frontend/e2e/workspace.spec.ts` against the mock backend with realistic shapes (an 11-row board with full breakdowns and a generated 400-row board):

- A 400-row ranking draws under 80 rows at any scroll position and reaches row 400 by scrolling; `aria-rowcount` and `aria-rowindex` stay correct for screen readers.
- Compact density makes ranking rows measurably shorter (at least 6 px) with the same columns.
- Chart facts and the ranking metadata line are visible with no interaction; a score breakdown with provider and price date takes one interaction.
- No horizontal page scroll at 1440 px or at phone width with compact density and a folded chart.

## Tests that protect the patterns

- `core/workspace-prefs.spec.ts`: preference parsing, bounds, keyboard resize math and the row-window math (also for 20,000 rows).
- `core/chart-meta.spec.ts`: the point-of-use facts, UTC time, missing values named and never zero.
- `e2e/workspace.spec.ts`: fold keeps metadata and persists, keyboard and pointer resize, full screen and focus return, density persistence and fallback, windowed list, axe-core WCAG A and AA in both themes, no sideways scroll, reduced motion and a click-count check for discoverability. Runs on desktop and phone profiles.

## Usability study protocol

Automated tests prove that the patterns work. They cannot show that people find them. Before the next release candidate, run this study with 5 to 8 participants who analyze companies (at least 2 who have never used FloweySales), on a laptop, with real provider data:

1. Tasks: (a) find the source and newest-bar time of the chart; (b) fold the chart and say what is still known about it; (c) make the chart taller; (d) find why the second-ranked company scores as it does and where its price date comes from; (e) switch to a denser layout and find a metric in a 100-row ranking.
2. Measures per task: completion without help, time, number of interactions, wrong turns, and whether the participant noticed the fold, resize and density controls before being told (discoverability).
3. Targets: at least 80% unaided completion of each task, a median of 2 or fewer interactions for (a), (b) and (d), and at least 60% finding density or resize unaided.
4. Record results in this directory and open issues for any target missed. This step needs human participants and is not done by this repository's automation.
