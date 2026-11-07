# Async feedback acceptance – issue #203

The reusable status and skeleton primitives already cover key asynchronous flows and reduced-motion behavior. This change fixes an observable regression: when a World refresh fails the page previously replaced valid cached data with an error page, despite loading-state guidance that refreshes preserve content. The revised UI retains the last successful World snapshot and announces that refresh failed (role=alert). The first-load error is still shown if no data ever arrived.

The Markets chart previously displayed an unlabelled 2px animated strip. It now uses the common accessible ProgressStatusComponent with an explicit "Loading price history" stage, and a reserved-height wrapper so toggling progress does not shift the studies/chart controls.

A browser regression injects a 503 during refresh and confirms the cached headline/theme remains visible.

Validation: `cd frontend && npx playwright test --config playwright.config.ts e2e/async-refresh.spec.ts`, plus CI responsive and reduced-motion suites.

Remaining for complete closure: a visual audit of loading/empty/error conditions on *all* major screens and realistic slow/error provider paths, including manual review of layout transitions. These changes do not by themselves prove a complete platform-wide UX program.
