# Flow trigger diagram E2E failure — 2026-11-07

Related to #262.

Incident: [Actions run 42395297708, job 128471280696](https://github.com/Doldas/floweysales/actions/runs/42395297708/job/128471280696) failed on both mobile and desktop. The E2E test waited 30 seconds for an SVG node with `data-node-id="event:filing.published"`, but the shared `triggerDiagram` adapter purposely assigns stable IDs `event:<database integer>` and `payload:<database integer>`.

The UI had a second defect: the selected trigger details computed the suffix of the stable ID but compared it with `event_key`, so clicking a genuine diagram event never displayed the expected payload explanation. The fix compares numeric IDs, retains stable ID identity, and changes the browser test to select the event semantically instead of assuming an arbitrary database ID. The regression clicks the event, asserts selected state and details, then clicks the corresponding typed payload and confirms the same details.

The PR CI now runs `e2e/flow-diagrams.spec.ts` on **desktop and mobile**. This previously ran only in the separate full end-to-end workflow after merging; consequently the regression could go unnoticed before merge.

Run locally: `npm ci --prefix frontend && npm run build --prefix frontend && (cd frontend && npx playwright install chromium && npx playwright test --config playwright.config.ts e2e/flow-diagrams.spec.ts)`.

Do not close issue #262 based on this single test; the full cross-surface design acceptance has broader scope. Merge only after the new CI run is green.
