# Plan 1: Flow output capacity

**Issue:** #193. Independent of the other plans.

## What happens today

- The failure comes from `flowRowsJSON` (`internal/server/flow_investor.go:137`),
  which rejects investor-node output over `flowInvestorMaxJSON = 16000`.
- **The error's advice is wrong.** A leaderboard node passes on at most 25 rows
  (`validateLeaderboardNode`), and its size is set by that row limit, not by the
  universe. 18,883 bytes is about 755 bytes a row at 25 rows, so ordinary use can
  hit this. "Use fewer symbols" may not help; the setting that matters is the row
  limit. **To verify:** the row count of the failed run.
- 12 places call `flowRowsJSON`, so the same ceiling applies to all investor nodes.
  `report_table` has its own check against the same constant (`flow_investor.go:778`).

**Raising the constant alone would cause new, quieter failures:**

| Where | Today | What a larger output does |
| --- | --- | --- |
| Checkpoint (`flowCheckpointState`) | Each output is held several times: `Previous`, `NodeResults`, `Results`, `V2.Values`, `V2.Steps` | Hits `maxCheckpoint = 1 << 20` quickly |
| `flowCheckpointer.save` | Save errors are only logged, and the run continues | Resume stops working, and nobody is told |
| Run history (`stepPreviewLimit = 4000`) | Keeps a 4 KB preview | The UI shows a fragment with no sign that it is one |
| Agent node input | `previous` is pasted into the prompt | May overflow the model's context window |
| Email (`notifyEmailMaxBody = 20000`) | `truncateText` cuts the body | The reader gets a shortened report |
| Subflow (`flows.go:1456`) | `output = output[:6000]` with no error | **Already silent data loss**, and it can cut JSON in half |

`len(f.Graph) > 16000` in several files limits graph size, which is a different
limit. Leave it alone.

## Solution

Raise the limit now, then store full outputs once, so previews and full data are
separate things. Target limit: 1 MiB per node, with a run-level budget.

## Work packages

### WP1: Stop the failures (S, `fix/flow-output-limit`)

- Replace `flowInvestorMaxJSON` with `flowNodeOutputMax`, default 256 KiB.
- Add `FLOW_NODE_OUTPUT_MAX_BYTES` to the config, clamped to 16 KiB–4 MiB and
  documented in `config.properties.example`.
- Raise `maxCheckpoint` to 8 MiB. A node at the new limit can still take over 1 MiB
  of checkpoint, so about six such nodes can exceed the cap until WP2. Save failures
  other than a lost lease therefore add a visible run note ("this run cannot resume
  from step N"). **To verify:** the database's `max_allowed_packet` allows 8 MiB.
- New error text: name the node, the size, the limit and what to do (lower the row
  limit, select fewer columns, or raise the setting).
- Replace the subflow's silent cut with the same limit and a clear error.
- Update `docs/investor-automations.md`, which states "at most 16 KB".

**Tests:** 18,883 bytes passes and reaches the downstream node intact; exactly at
the limit passes; one byte over fails with the new text; a 25-row leaderboard
passes; a subflow over the limit errors rather than truncates.

### WP2: Store each output once (M, `feature/flow-run-outputs`)

- New table `agent_flow_run_outputs (run_id, node_id, attempt, bytes, content)`,
  with `content` redacted like previews, removed with the run (cascade).
- Checkpoints refer to outputs by node id, so each output is stored once. Old
  checkpoints must still restore: keep reading the current shape.
- Add a run budget, `FLOW_RUN_OUTPUT_MAX_BYTES`, default 16 MiB. Exceeding it fails
  the run with a clear message.
- Then raise the default node limit to 1 MiB.

**Tests:** a checkpoint from before the change resumes; a run with several 1 MiB
outputs resumes after a simulated worker loss; the budget is enforced. These are
database-backed, so verify them in CI.

### WP3: Full output in the UI (S, `feature/flow-output-viewer`)

- `GET /api/flows/{id}/runs/{run}/steps/{node}/output`, with the same access checks
  as the run view. Steps gain `output_bytes` and `output_truncated`.
- Run history labels previews ("first 4 KB of 18.9 KB") and adds "Show full" and
  "Download". Rendering stays lazy, so a large output does not slow the list.

**Tests:** spec for the preview label; an API test that another user cannot read
the output.

### WP4: Downstream consumers (S, `fix/flow-large-inputs`)

- Agent nodes: over `flowAgentInputMax` (default 64 KiB, configurable per node),
  fail with a message suggesting a `screen` or `report_table` node before the
  agent. Never cut silently: truncated JSON misleads the model.
- Email: when cut, say so in the body and link to the run.

**Tests:** an oversized agent input gives the error and makes no model call; a cut
email contains the notice.

## Acceptance mapping

| Criterion | WP |
| --- | --- |
| 18,883 bytes completes and downstream gets all of it | WP1 |
| Regression tests for boundaries and large universes | WP1, WP2 |
| Limit, configuration and fallback documented | WP1, WP4 |
| Large outputs do not break history or the UI | WP2, WP3 |

## Out of scope

Pagination between nodes. Row limits and `screen` nodes cover this until a real
case needs more.
