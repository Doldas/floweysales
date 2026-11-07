# Issue #255 – evidence-only claim verification and honest limits

The deterministic verifier already distinguishes supported, contradicted, insufficient and not-checked claims. In strict mode it raises findings for contradictions, insufficient evidence and *unlabeled* consequential judgements. A not-checked qualitative description or an explicit answer that abstains is **not proof** but must not be treated as a fresh violation: doing so caused correction loops and rejected correct refusals in the AI/evaluation integration suite.

The new regression explicitly preserves that distinction for forecasts, recommendations and qualitative excerpts. The frontend's empty-findings banner no longer claims that all cited sources were opened or that claims were proven. When the per-answer cap prevents all checks, the UI says that verification is incomplete rather than presenting an empty success.

The existing verifier exposes `elapsed_ms`, `model_calls=0`, 40-claim cap and skipped-count metadata. This has no additional model or search charge.

## Validation

- `go test ./internal/sources ./internal/ai ./internal/eval`
- `node --test frontend/src/app/core/answer-sources.spec.ts`
- Full GitHub Actions including end-to-end model stubs

Remaining before #255 can be closed: complete real-world financial semantic evaluation and measured latency/cost envelopes across model outputs. A deterministic ruleset must not claim to verify qualitative claims it does not understand.
