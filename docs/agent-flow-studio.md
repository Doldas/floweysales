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

## Blueprint-style reusable flow calls (next increment)

Add **Call Flow** nodes in the graph editor and select another saved flow by ID. When the parent flow runs, the selected flow is executed as a nested research function, and its serialized output feeds the following node. Nesting is bounded to four levels and a flow cannot call itself recursively, directly or indirectly. Subflows inherit the parent run's time limit and the read-only/no-tools policy. Missing references fail the run safely.

This is the **first building block**, not yet a full Unreal Engine Blueprint-equivalent visual language. Strongly typed value pins, declared input/output ports, independent user-authored function libraries, parameter binding, graph revision pinning, custom trigger constructors and step-by-step debugging remain tracked under #59. Nested flow calls currently share the parent run log rather than creating child run records.

## Typed value nodes (Blueprint milestone)

The graph editor now offers a **Value** node with an explicit `text`, `number`, or `boolean` type. Users can connect its `value` output to an agent/subflow `input` port. The backend validates the declared pin names and disallows direct number/boolean-to-text connections. Text constants can be used as the next node's research context. Numeric and boolean constants can be stored in graphs, but typed conversion, arithmetic/comparison nodes, multi-input evaluation and independently defined reusable function graphs have **not** yet been implemented. This milestone preserves existing execution edge behavior; true independent control and data wires are future work under #59.
