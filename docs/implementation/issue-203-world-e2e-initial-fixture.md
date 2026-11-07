# World refresh E2E regression — 2026-11-07

Related to #203.

## Incident

[End-to-end run #42402711977, job #128493802800](https://github.com/Doldas/floweysales/actions/runs/42402711977/job/128493802800) failed on desktop and mobile. The new refresh-failure test timed out waiting for `article.theme` before clicking Refresh.

The shared Playwright mock backend returned HTTP 404 for `/api/world`, because the endpoint had no mock response. The app correctly showed its initial-load failure state; the test incorrectly expected a populated first snapshot. This was a test-fixture gap, not evidence that cache-on-refresh had failed.

## Fix

- Add a **clearly synthetic**, deterministic World API response with the typed WorldView shape, one theme/headline, explicit fake source and stable timestamp. Never contact external news providers.
- Assert the first theme really came from that mock rather than silently assuming any prior data.
- After loading, force `/api/world?refresh=1` to return HTTP 503, assert the intercepted response, confirm the accessible warning, and confirm the existing headline stays visible.
- Keep the separate first-load HTTP 503 test: its error/empty state must still have no skeleton and leave Refresh available.

## Verification

Run `cd frontend && npx playwright test --config playwright.config.ts e2e/async-refresh.spec.ts --project=desktop --project=mobile` then full repository CI. This branch must not be merged until the latest Playwright and Go jobs pass.

No production code/API behavior, network credentials, or external provider calls were changed. Broader issue #203 acceptance remains open.

## Accessibility regression exposed by the restored fixture

With World content correctly mocked, the existing responsive audit now sees the actual headline links instead of an error screen. It detected a 17px-tall news link (minimum touch target required: 24px) across seven narrow widths, phone landscape, and the reduced-motion case. The World page now renders headline anchors as inline blocks with a 24px minimum height and vertical padding. The existing responsive audit covers these same widths automatically in PR CI; no audit exclusions were added.

A later focused CI run also identified an ambiguous `World` heading selector (it matched the page h1 and "World events and NVDA" h2). The refresh test now uses an exact match.
