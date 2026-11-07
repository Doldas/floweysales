# Issue #325: isolated security or configuration regression

Status: scoped change implemented on this PR branch; CI/review required. Issue remains open.

Delivered: Reject credential-bearing external Markdown links in shared rendering.

Validation: `node --test frontend/src/app/markdown.spec.ts`; run full Angular build and GitHub Actions. No external search/API charge is required.

Remaining: This change does not implement all provider/rendering capabilities specified in #325.
