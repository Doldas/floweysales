# Issue #254: focused correctness change

**Status:** proposed in this PR; integration not complete until CI and review. Parent issue remains open.

**Implemented:** Reject unsafe precision-loss Monte Carlo seeds.

**Tests:** `node --test frontend/src/app/core/strategy-montecarlo.spec.ts`; full CI required after creating the PR.

**Outstanding:** Full feature acceptance goes beyond this guard. Verify user journeys and additional criteria before closing #254.
