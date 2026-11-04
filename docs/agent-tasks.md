# Durable research tasks

A chat becomes a **task** when `POST /api/ai/chat` carries `deliverables`, a list of up to 12 required parts of the answer (`["Latest price", "DCF", "Risk note"]`).

## In the chat

Under the composer, **Required parts** takes the deliverables, one per line (up to 12). A question sent with them is saved as a task, and a checklist card shows each part as done, reported missing or not yet reported while the run goes on. **Saved tasks** lists your tasks with their status, rounds, tool calls and last update (UTC), and offers **Resume** and **Delete**. Resume continues from the last checkpoint on the server; the chat then shows only the continuation, since the saved conversation stays on the server. The resumed run uses the default model and persona, not the ones chosen for the original question.

## What a task adds

- **Checklist.** The agent ends its answer with `[done] <deliverable>` or `[missing] <deliverable> - <reason>` per line. A deliverable the answer leaves out counts as missing.
- **No false completion.** Missing deliverables send the model back twice to finish them. If they are still missing the task ends `incomplete`, never `complete`. An answer with tools enabled but no tool call behind it is also `incomplete`.
- **Evidence ledger.** Every tool call is recorded with its arguments, a preview of the result, any error and a timestamp.
- **Checkpoints.** After each model round the conversation, ledger, checklist and spent budgets are saved in `agent_tasks`. Failed, cancelled (client disconnect) and incomplete tasks keep their last checkpoint.
- **Resume.** The saved conversation, ledger and tool-call count are restored. The task continues without dropping evidence.

## API

| Request | Purpose |
| --- | --- |
| `POST /api/ai/chat` with `deliverables` | Starts a task. The SSE stream gains `task` (id) and `checkpoint` (status, checklist) events |
| `GET /api/ai/tasks` | Your tasks, newest first |
| `GET /api/ai/tasks/{id}` | One task with checklist and evidence ledger |
| `POST /api/ai/tasks/{id}/resume` | Continues from the checkpoint; body `{"message": "..."}` is optional. Refused (409) for a `complete` task |
| `DELETE /api/ai/tasks/{id}` | Removes a task |

Statuses: `running`, `complete`, `incomplete`, `failed`, `cancelled`. A user keeps at most 100 tasks, each checkpoint at most 4 MiB. Tasks follow the same tool rules as chat (see below).

## Not covered yet

The local-model VRAM benchmark from #5 is not built. `cmd/agent-eval` ([agent evaluation](agent-evaluation.md)) is its base.

## Agent tools by role

Every account can use the agent's tools; what differs is which tools.

| Class | Tools | Who |
| --- | --- | --- |
| Public data | Quotes, history, technicals, financials, SEC filings and sections, provider research, calculators (DCF, WACC, ROIC, ratios), web search, leaderboard, backtests, rule strategies, insider activity, macro regime, corporate events, reference portfolios | Everyone |
| Your own data | Wealth plan, domain providers (they read your saved files) | Everyone; each run reads only the caller's data |
| Server state | Engine config, recorded ticks, market stats, the paper portfolio | `SYS_ADMIN` only: they show the server's trackers and stop levels, which are shared |

Tools that spend something shared (web search, provider and SEC calls, backtests) are limited to 60 calls a minute per ordinary account; the agent is told when the budget is used up. Administrators are not limited.

Every tool in the catalogue has an explicit class in `internal/server/ai_tool_scope.go`, and a test fails when a tool is added without one. `GET /api/ai/config` lists the tools the caller can use.

## Tool call events

Every `tool_start` and `tool_result` of a chat, task or team run carries `call_id` (it ties a result to its start even when calls overlap), `seq` (the order in the run) and, on the result, `duration_ms`. What the user is shown is bounded and redacted: `arguments` up to 8 KB and `result` up to 64 KB, with `truncated` and `original_bytes` when it was cut and `redacted` when credentials were masked. The model itself receives the full result, up to the run's tool-result limit.

Before a call runs it is checked against the tool's schema (`internal/ai/preflight.go`): an unknown tool, a missing required argument or a value of the wrong kind comes back to the model as a `tool_error` that says what to fix, and the tool does not run. Numbers sent as text are accepted. A run's final `error` event never contains the model endpoint address.

