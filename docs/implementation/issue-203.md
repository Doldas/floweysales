# Issue #203: scoped hardening

Status: implemented in this PR; not merged. Parent issue remains open pending its remaining acceptance criteria.

## Delivered

Treat NaN/Infinity timing as unknown rather than a stalled request.

## Verification

Run `node --test frontend/src/app/ui/async-state.spec.ts` and the repository CI workflow. Regression cases are deterministic and do not depend on external providers.

## Remaining

This focused change does not finish all acceptance criteria in #203; validate the whole feature before closing the issue.
