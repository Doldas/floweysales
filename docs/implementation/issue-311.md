# Issue #311: isolated security or configuration regression

Status: scoped change implemented on this PR branch; CI/review required. Issue remains open.

Delivered: Reject non-finite paid search ceilings and noninteger free quotas.

Validation: `node --test frontend/src/app/components/web-search-view.spec.ts`; run full Angular build and GitHub Actions. No external search/API charge is required.

Remaining: This change does not implement all provider/rendering capabilities specified in #311.
