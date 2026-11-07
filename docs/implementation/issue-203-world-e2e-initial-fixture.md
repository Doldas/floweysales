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
