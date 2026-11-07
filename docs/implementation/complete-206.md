# Documentation acceptance evidence — issue #206

The existing Markdown builder validates internal guide links and headings, static pages, themes/brand copies, and performs a separately scheduled external link check. Pages publishes from `main` on changed documentation.

New: Playwright renders six high-value guide pages on desktop and a real phone viewport, asserting headings, safe navigation, no body horizontal overflow and capturing full-page screenshots in CI artifacts. Navigation from the landing page is checked. A post-deploy Pages job verifies that the public guide actually responds after upload.

**How to validate**

`node scripts/build-docs.mjs --check`
`node --test scripts/build-docs.test.mjs`
`cd frontend && npx playwright test --config playwright.docs.config.ts`

The pages have not yet been deployed from this PR. The post-deploy check runs only when merged and the Pages workflow publishes. Human screenshot review is still required; screenshot existence alone does not establish visual quality. An exhaustive review of every obsolete sentence or screenshot remains editorial work.
