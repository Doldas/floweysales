# Issue #204: scoped hardening

Status: implemented in this PR; not merged. Parent issue remains open pending its remaining acceptance criteria.

## Delivered

Reject ambiguous whitespace in displayed provenance links.

## Verification

Run `node --test frontend/src/app/core/provenance.spec.ts` and the repository CI workflow. Regression cases are deterministic and do not depend on external providers.

## Remaining

This focused change does not finish all acceptance criteria in #204; validate the whole feature before closing the issue.
