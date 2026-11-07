# Issue #258: deterministic hardening

Status: implemented in PR branch; pending review and CI. Parent issue remains open.

Delivered: Validate actual calendar dates for index membership snapshots.

Validation: `node --test frontend/src/app/components/index-sources-view.spec.ts` plus CI. No live providers are contacted by these regressions.

Outstanding: The full issue's upstream provider/license tests and feature acceptance remain separate. Do not close the parent issue based only on this fix.
