# Durable research tasks

A chat becomes a **task** when `POST /api/ai/chat` carries `deliverables`, a list of up to 12 required parts of the answer (`["Latest price", "DCF", "Risk note"]`).

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

Statuses: `running`, `complete`, `incomplete`, `failed`, `cancelled`. A user keeps at most 100 tasks, each checkpoint at most 4 MiB. Tasks follow the same tool rules as chat: accounts other than SYS_ADMIN get no tools.

## Not covered yet

The web UI does not show tasks, and the local-model VRAM benchmark from #5 is not built. `cmd/agent-eval` ([agent evaluation](agent-evaluation.md)) is its base.
