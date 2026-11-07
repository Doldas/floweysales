# Responsive audit: phones and tablets (#257)

Priority: **P3 / low**. This audit never displaces financial-data accuracy, source verification or
backtest robustness. A defect is escalated only when it blocks a core workflow or an accessibility
requirement (for example an action that cannot be reached).

The compact mobile header was audited and fixed separately (#268, `e2e/mobile-layout.spec.ts`). This
document covers every other page.

## Method

`frontend/e2e/responsive-audit.spec.ts` opens each page against the deterministic mock backend and checks:

- the page never scrolls sideways (`scrollWidth` against `clientWidth`);
- no element sticks out of the viewport unless a scroll area (`overflow: auto`) contains it;
- on screens under 1024 px, buttons, links, tabs and form controls are at least 24 by 24 CSS px
  (WCAG 2.5.8; links inside a sentence are exempt);
- text cut off without an ellipsis (`text-overflow: clip`).

Run it with `npm run build && npx playwright test e2e/responsive-audit.spec.ts` in `frontend/`. Each run attaches
a full-page screenshot per page at 360 px and at 360 px with 200% text to the HTML report (the "after" evidence in CI).
Set `AUDIT_DUMP=1` to print each page's findings.

## Matrix

| Dimension | Values |
| --- | --- |
| Pages | Command Center, Research Agent, Agent team, Flows, Inbox, Markets (with Leaderboard), Strategies, World, Wealth plan, Financials, Paper portfolio, Workspace, Community, Notifications, Settings, Help, About |
| Phone widths | 320, 360, 390, 412 px (portrait), 844 x 390 px (landscape) |
| Tablet widths | 600, 768, 820, 1024 px |
| Text size | 100% and 200% (root font size doubled) at 360 px |
| Theme and motion | Dark theme with `prefers-reduced-motion: reduce` at 360 px (no infinite animations) |
| Browser | Chromium (desktop and Pixel 7 profiles in CI). WebKit and real devices are not covered (see below) |

## Findings and status

Severity: **S2** = content unreachable or the page scrolls sideways at 100% text; **S3** = layout breaks
only at 200% text, or a target under 24 px; **S4** = cosmetic.

| # | Where | Defect (viewport) | Sev. | Status |
| --- | --- | --- | --- | --- |
| 1 | Strategies | Strategy panel and run bar 67 px wider than the screen at 320 px, "Customize a copy" and "Run test" off screen (grid column sized by its content) | S2 | Fixed: single `minmax(0, 1fr)` column; cost labels wrap |
| 2 | Markets, Leaderboard | Preferences select 364 px wide, outside the card at 320 to 390 px | S2 | Fixed: controls grid and selects shrink |
| 3 | Wealth plan | Allocation and assumptions tables not scrollable, 4 px outside at 320 px | S2 | Fixed: tables sit in scroll areas |
| 4 | Command Center | 2 px sideways scroll at 320 px caused by a visually hidden label escaping the table scroll area | S3 | Fixed: scroll areas are positioned |
| 5 | All pages | Help buttons 20 by 20 px, small buttons and selects 19 to 22 px high, standalone links 17 to 19 px | S3 | Fixed: 24 px minimum on touch-size screens |
| 6 | All pages | Header agent button 22 px high; header search input 23 px | S3 | Fixed |
| 7 | About | Detail list pushes the page 49 px wider at 200% text | S3 | Fixed: one column on phones, long values wrap |
| 8 | Community | Search field and post list wider than the screen at 200% | S3 | Fixed |
| 9 | Financials | Period controls 33 px wider at 200% | S3 | Fixed |
| 10 | Help, Flows, Settings, Inbox | Buttons with a nowrap label wider than the screen at 200% | S3 | Fixed: labels may wrap on phones |
| 11 | Team, Plugin tools, Wealth plan, Portfolio risk, Events | Card grids with a minimum column of 9 to 20 rem overflow at 200% | S3 | Fixed: `min(100%, N rem)` |
| 12 | Markets | Whole page 189 px wider at 200% (page grid column sized by content; instrument search box and provider select) | S3 | Fixed |
| 13 | Agent dock, pages | `viewport-fit=cover` without safe-area padding: content under notches and the home indicator in landscape | S3 | Fixed: `env(safe-area-inset-*)` padding |
| 14 | Strategies, Leaderboard | Wide tables, charts | S4 | Already contained in their own scroll areas; no change |

Defects 1 to 13 are fixed in this change and are guarded by the regression tests. Dense tables and
density preferences are tracked separately (#259) and were not touched.

## Tablet assessment

768, 820 and 1024 px show no overflow, clipping or unreachable action after the fixes. The sidebar is a
drawer up to 991 px and fixed from 992 px; the agent dock overlays the page below 1440 px. Tablets work with the
phone layouts, so no tablet-only layout is needed now. Possible later polish, all low priority: use the free
width on a 768 px portrait tablet for two-column Strategy results; keep the dock docked beside the page at 1024 px.

## Staging and triage

- **Do now (done):** blockers and accessibility defects above (S2, S3).
- **Later (P3):** the tablet polish above; Safari/WebKit and real-device passes (virtual keyboard resize, iOS
  safe areas, Android font scaling). These need hardware or a WebKit runner that CI does not have; the
  layout uses `dvh` units, 16 px form fonts on phones and `env(safe-area-inset-*)`, which are the usual causes.
- **Escalate only if** a core workflow is unusable or an accessibility requirement fails on a supported phone.
  Substantial view-specific defects found later get their own child issue and a row in the table above.
