# Agent Flow Studio (first increment)

The authenticated SYS_ADMIN dashboard now includes a visual graph editor. It saves graphs in the Liquibase-managed `agent_flows` table and preserves each execution in `agent_flow_runs`.

## Supported nodes and triggers

- One **Trigger** root plus 1–15 **Research Agent** nodes connected by directed edges; no cycles or disconnected nodes.
- **Manual:** click Run now on a saved flow.
- **Interval schedule:** enable the flow and set `interval_minutes` to 5–10080. The Go scheduler polls every 30 seconds, and the DB atomically claims due intervals across service replicas.
- **Event key:** enable a flow with a key such as `market.close`; authenticated SYS_ADMIN can call `POST /api/admin/flow-events/market.close`. This is an administrator-triggered event hook, not a public unauthenticated webhook. Automated source adapters can be added later behind a separately secured event-ingress layer.

The editor supports positioning nodes, editing prompts, linking nodes, saving, running and reading recent run traces. The initial executor validates that the graph is a DAG, executes research nodes in topological order, and passes the previous agent's output to the next. Execution uses the configured OpenAI-compatible model with a two-minute timeout and a maximum of two concurrent runs per server instance.

## Safety and remaining work

- Agent tools are **disabled** in flow execution initially. This prevents order placement or other side effects, but also limits agents to prompt-based research instead of tool-driven autonomous investigation.
- Flows **never execute live trades**. An admin must separately enable authentication and configure an AI endpoint/model.
- Schedule polling and execution are bounded, with durable completed/failed run history. Scheduling claims are durable but not an at-least-once queue: a process death after claiming a run but before inserting it can miss that interval. Runs interrupted after insertion are eventually marked failed.
- The flow graph rejects cycles. Recurring loops happen at the scheduler level, not through unbounded edges.
- Next iterations should include per-step persisted checkpoints, retries/dead-letter queues, condition/branch nodes, configurable evidence-backed read-only tools, trigger adapters, replay and cancellation controls, versioned graph revisions, run ownership and more adversarial integration tests.

To start, log in as SYS_ADMIN, scroll to Agent Flow Studio at the top of the terminal, create a flow, adjust prompts and graph links, save it, and either run it manually or enable a schedule.
