# Flow Engine v2: staged rollout

This PR implements the first backward-compatible increment toward FloweySales automation orchestration.

## Graph versioning
- Graphs without a version remain **v1**, preserving existing saved flows and execution behavior.
- Graphs with `"version":2` use explicit edge-delivered inputs, selective conditional routes, and skipped inactive nodes.
- V2 additions: `condition` (equals, not-equals, contains, numeric greater/less), `json_get` (one flat object property), and `merge` (JSON array of active inputs).
- Each conditional output edge carries `from_port: "true"` or `from_port: "false"`.
- Existing workflow execution limits, authentication and read-only AI policies remain unchanged.

## Known limits / work not claimed complete
This is a foundational versioned graph engine, **not** the whole enterprise roadmap. Durable execution checkpoints, cancellation, per-node DB traces, JSON Schema, parallel scheduling, approval workflows, database tools, agent tool grants, finance scanning/backtesting, report exports and template versioning require separate reviewed changes. Branch execution runs in a single process; independent nodes are still sequential within their topological order. Conditional inputs to multiple branches must be routed carefully; merges are bounded by existing node count and result limits.

## Planned increments
1. Comprehensive node lifecycle state and durable run-event journal, replay/cancellation, retry and timeout policies.
2. Schema-aware typed data ports and a visual mapper, with strict validation and migration testing.
3. Bounded foreach/parallel fan-out and aggregate; concurrency quotas and cost budgets.
4. Scoped MCP grants plus human-in-the-loop approvals and auditing for mutations.
5. SQLite userspace broker, financial strategy scanner/backtests/reports, reusable flow templates and rollback.

Do not enable production workflows without passing CI, integration tests and security review.
