# Issue #255 – evidence-only abstention and verification limits

Implementation: evidence-only checks now block cited statements that are **not verifiable** (previously only contradicted/insufficient or unlabeled statements raised findings). Merely citing a source is never proof that a recommendation or qualitative claim is verified. A deterministic golden regression covers recommendations, inferences, qualitative assertions and forecasts. The UI no longer calls a no-findings citation audit “passed” or claims every source was opened.

The claim verifier already exposes `elapsed_ms`, `model_calls=0`, the 40-claim cap, skipped counts and evidence spans. The UI now visibly reports a zero-claim cap hit instead of silently suggesting success.

Run `go test ./internal/sources`, `node --test frontend/src/app/core/answer-sources.spec.ts` and full CI.

Remaining before declaring all of #255 complete: end-to-end behavior against actual model responses and a reviewed evaluation set, including cost/latency measurements across realistic workloads. Do not mark the larger acceptance criteria complete from these deterministic tests alone.
