# Flow agent output, JSON-only mode, MCP tracker control

## Flow nodes
- Agent output may be connected to a downstream node using the Node Details "Connect output from" picker. An explicit **Output Result** node can display the final value, and adding it while an agent is selected automatically connects it.
- An unconnected agent continues to appear in the normal run history. This is backward compatible with existing saved flows.
- Agent Output format: `Markdown / Default` (unchanged) or `JSON only`. JSON mode adds a strict output-only system instruction and rejects invalid JSON or scalar values **after** the model responds. It does not silently strip prose or markdown fences.
- Agent tools remain disabled within scheduled Flow Studio research executions. Tracker mutations are **not** enabled for research flow agents.

## MCP finance tracker tools
New optional MCP tools: `tracker_list`, `tracker_status`, `tracker_start`. They operate against the same persisted tracker registry and 16-tracker quota as the dashboard, with no trading orders.

### Security and configuration
Tracker mutation is opt-in and only exposed in **local stdio MCP transport**. Define the same high-entropy secret (minimum 32 characters) on the FloweySales backend and the stdio MCP process:

```shell
export FLOWEY_MCP_WRITE_TOKEN="<a-unique-secret-at-least-32-characters>"
./floweysales_mcp -transport stdio -backend http://127.0.0.1:8080
```

The backend rejects missing/wrong tokens in constant time for `/api/mcp/trackers` endpoints. No MCP mutation tools are exposed over unauthenticated public HTTP MCP transport. The model should invoke `tracker_start` only on an explicit user request.

This token is an **administrative capability**. Protect it in a secrets manager, never in source control, and restrict MCP process access. Production hardening should add per-user capability grants, audit events and short-lived delegation before exposing writes over network-based MCP.
