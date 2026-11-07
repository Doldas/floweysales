# Playwright visual evidence

The normal pull-request CI runs `frontend/e2e/visual-evidence.spec.ts` against the compiled FloweySales UI with deterministic mock API data. It captures:
- **My Flows overview** and **Flow editor** with its graph.
- **Research Agent** layout.
- Both desktop Chromium and emulated Pixel 7 viewport (each test scenario runs in both projects).

The screenshots are attached on **successful runs too**. Failed tests additionally keep their failure screenshot, WebM video and Playwright trace. The HTML report, screenshots and failure evidence are uploaded under each CI run's **Artifacts** as `playwright-visual-evidence-<run-id>`, retained for **two days**.

## Review evidence

1. Open the pull request's **Checks** and select the **frontend** job.
2. Open the workflow run and scroll to **Artifacts**.
3. Download `playwright-visual-evidence-<run-id>` and open `playwright-report/index.html` locally to view results and attached screenshots. If your browser restricts local HTML assets, serve the unpacked report with a static HTTP server or use `npx playwright show-report` on a local report directory.
4. If a test failed, open the associated `video.webm` or inspect `trace.zip` with `npx playwright show-trace path/to/trace.zip` or [Playwright Trace Viewer](https://trace.playwright.dev/).

Run locally after the Angular build:

```sh
cd frontend
npm ci
npm run build
npx playwright install chromium
npx playwright test --config playwright.config.ts e2e/visual-evidence.spec.ts
npx playwright show-report
```

## Security and limits

**Never upload credentials or user records into ordinary PR artifacts.** This suite uses mocked data, not the real deployed instance, so failure traces are safe to collect. Live E2E continues to use `trace: 'off'` because Playwright traces can include cookies, model output and request bodies. Live screenshots/videos can also contain account data and remain under the separate protected `live-e2e` environment, with short retention.

The attached screenshots are human/AI **review evidence**, not pixel-difference golden baselines. Pixel-perfect screenshot regression (with reviewed, stable baseline PNGs) can be introduced separately after the UI stabilizes; it is intentionally not asserted in this PR, to avoid misleading pass/fail results.

Related: #289 (end-to-end workflow journeys and UI/UX checks).


### Financial Markdown rendering journey

The Workspace Markdown suite now exercises a complete mobile/desktop DCF note against the mocked file API. It checks preserved dollar amounts, fractions, a four-column valuation table, literal inline code, no page-level horizontal overflow and exact original source after Preview → Edit source. Every run attaches a screenshot (`workspace-financial-markdown.png`) to the Playwright HTML report. This verifies rendering and layout only, **not the arithmetic correctness or financial provenance**. As the parser grows, update the shared sample and add independent calculation checks rather than treating the visual screenshot as verified financial data.


### Protected mathematical source in Workspace

A desktop and Pixel 7 mock-backed Playwright journey opens a financial Markdown file, checks that Rich text is disabled for unsupported LaTeX source, verifies the Preview still displays a formula, attaches a screenshot, and confirms an exact round-trip back to Source without page-wide overflow. This protects document integrity; it does not verify financial arithmetic.
