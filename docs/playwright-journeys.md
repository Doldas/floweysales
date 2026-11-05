# Playwright Flow Studio journeys

The regular PR pipeline now runs `frontend/e2e/flow-journeys.spec.ts` against the built Angular app in desktop Chromium and a phone viewport.

These tests **simulate UI actions** against deterministic, seeded API fixtures:
- Open **My Flows** → select an existing saved workflow → verify the graph is visible → inspect its persisted successful run and tool trace.
- Open a failed workflow → inspect the failure and confirm the UI exposes an accessible error.
- Open a draft in the Flow editor on both desktop and phone → verify editor controls, graph, and document bounds.

These browser journeys are intentionally safe and deterministic. They do **not** prove a live model used a prior step's output, nor do they execute a real Go worker. That requires the next, isolated backend integration phase in [#289](https://github.com/Doldas/floweysales/issues/289).

To run locally:

```sh
cd frontend
npm ci
npm run build
npx playwright install chromium
npx playwright test --config playwright.config.ts e2e/flow-journeys.spec.ts
```

The broader `smoke-flow-editor.mjs` scenario already covers building, linking, preflight, saving, publishing, running and unsaved drafts with a stateful backend stub. The next step is consolidating that scenario into the standard Playwright runner and adding concrete structured-data handoff assertions (Research → Analysis → Output). Live-provider calls remain manually gated and should run against disposable staging, not ordinary user flows or real trading data.
