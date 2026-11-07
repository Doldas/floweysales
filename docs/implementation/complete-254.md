# Acceptance work for issue #254

The strategy robustness panel already provides seeded deterministic runs, percentile bands for return/drawdown, explicit replay observations, sparse-trade refusal and export of inputs and limitations. This PR adds uncertainty around finite simulation sampling using Wilson score intervals (95%) and visible disclosure that this is **not** a forecast/market confidence bound. The table is tied to its simulation provenance via aria-describedby.

Regression covers 0%, 100%, invalid probabilities, seed disclosure and uncertainty wording. Run `node --test frontend/src/app/core/strategy-montecarlo.spec.ts`, `go test ./internal/montecarlo ./internal/server`, CI, and inspect Strategy UI on mobile/desktop.

Remaining before calling the whole issue fully done: visual accessibility review with a rendered Monte Carlo report and realistic end-to-end integration evidence; the above assertions and previous unit tests do not alone verify all client behaviors.
