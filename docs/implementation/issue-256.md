# Issue #256: deterministic hardening

Status: implemented in PR branch; pending review and CI. Parent issue remains open.

Delivered: Bound malformed provider retry-after hints and typed nil rate-limit errors.

Validation: `go test ./internal/market/...` plus CI. No live providers are contacted by these regressions.

Outstanding: The full issue's upstream provider/license tests and feature acceptance remain separate. Do not close the parent issue based only on this fix.
