# Agent Flow Studio — user guide and execution model

Authenticated Flow Studio includes a **My Flows** dashboard, visual graph editor, Run History and My Workspace. Saved graphs live in the Liquibase-managed `agent_flows` table, run metadata in `agent_flow_runs`, and bounded node traces and per-run graph snapshots in separate tables. SYS_ADMIN can manage scheduled/event flows and privileged nodes; ordinary users can create and run their own manual-only flows. See [the merged UX progress log](flow-studio-progress.md).

## Supported nodes and triggers

- Exactly one **Trigger** root and 2–16 total nodes / 1–32 edges; the server rejects cycles and disconnected nodes. Besides agents, versioned graphs support conditions, merge, JSON field extraction, bounded data loops, value/transform/output, HTTP/web research and privileged function/subflow and Workspace nodes.
- **Manual:** click Run now on a saved flow.
- **Interval schedule:** enable the flow and set `interval_minutes` to 5–10080. The Go scheduler polls every 30 seconds and, in one MySQL transaction, claims the due interval and queues its run, so replicas never double-run an interval and a crash never loses one.
- **Event key:** enable a flow with a key such as `market.close`; authenticated SYS_ADMIN can call `POST /api/admin/flow-events/market.close`. This is an administrator-triggered event hook, not a public unauthenticated webhook. Automated source adapters can be added later behind a separately secured event-ingress layer.

The editor supports a searchable role-aware node picker, free positioning, zoom/pan, **Fit flow**, curved links, accessible click-to-connect ports and bounded **Undo/Redo** of graph changes. Selecting a node opens the **complete floating settings tooltip** (the former permanent Details panel was removed). Graph preflight reports core structural/configuration problems with click-to-node fixes; the server remains authoritative for all execution and permission checks. Version 2 execution routes values along edges and selects conditional branches in topological order.

## Safety and remaining work

- Agent tools are **disabled** in flow execution initially. This prevents order placement or other side effects, but also limits agents to prompt-based research instead of tool-driven autonomous investigation.
- Flows **never execute live trades**. An admin must separately enable authentication and configure an AI endpoint/model.
- Runs go through a durable queue with checkpoint resume and dead-lettering (see below); concurrency is bounded per process.
- The flow graph rejects cycles. Recurring loops happen at the scheduler level, not through unbounded edges.
- **Already available:** conditional/branch nodes, bounded agent retries/timeouts, owner-scoped manual flows, cancellation, recorded node steps, immutable run graph snapshots, execution timestamps, preview-only replay and draft/publish revisions with rollback and call pinning (see below). **Still planned:** durable checkpoints/dead-letter queues, general trigger adapters, richer per-step timing/attempt data and adversarial/accessibility tests.

To start, sign in and open **My Flows → New flow / Edit**. Add nodes with **+ Add node**, select a graph node to configure all fields in its floating tooltip, check **Graph validation**, then **Save flow** before **Run now**. Use **Run history → Inspect nodes** to view recorded steps, status-colored historical graph snapshots, duration and errors. Scheduling and typed event triggers require SYS_ADMIN.

## Branch, merge and type rules

In version 2 graphs a node runs when one of its inputs is active, and fails if several are active at once. The editor and the server therefore check, before a flow is saved or published:

- **Joining branches:** a node may take several inputs only when they come from opposite `true`/`false` branches of the same IF / ELSE (at most one can be active). Inputs that can arrive together, such as two steps that both follow the trigger or the same branch twice, need a **Merge** node, which collects every active input into a JSON array.
- **Typed pins:** each input pin of a function takes one source, and all of a function's pins must be fed from the same branches, so a pin is never missing when only one branch runs.
- **Types:** a pin that expects `number` or `boolean` accepts a constant, condition result or function output pin of that type. `text` pins accept any value. Values whose type is only known at run time, such as agent output or JSON fields, are still checked when the function runs.
- **Duplicates:** the same connection cannot be added twice, in any graph version.

The validation panel lists these problems and selects the affected node. Drawing or adding a connection that breaks a rule shows the problem straight away.

## Typed functions with several inputs and outputs

A Blueprint function can declare up to 8 named, typed **input pins** and 8 **output pins** (`number`, `text` or `boolean`). In the function library choose **New typed function**, declare the pins, add internal Value, Operation and Branch nodes (and optional extra exits) and wire them with data wires (values between pins) and execution wires (order, or `true`/`false` from a branch). Pin renames and removals update the wires that use them. The server validates types, ports, cycles and that every exit provides every output (graph version 2); older single-input functions keep working unchanged.

In a flow, the **Function** node of a typed function shows its pins on the canvas and in its settings:

- **Inputs:** bind each declared input pin to an upstream node. Every input must be connected, and a node cannot mix typed pins with a plain input. A function with a single input can also take the ordinary connection.
- **Outputs:** the function returns a JSON object keyed by output name with real JSON types (numbers and booleans are not turned into text). Route an output pin to a downstream node and that node receives only that value. Unrouted outputs are simply not used.
- **Checks:** pin names and types are checked against the exact revision the node will call, both in the editor and when the flow is saved or published, so a revision that changes its pins can be caught before it breaks a run. Pins require graph version 2. Pinning a function node to a revision (see above) pins its pin layout too.
- A node whose input pin is fed from a branch that was not taken is skipped like any other version 2 node.

### Drawing wires on the canvas

Drag from an output port (or a typed output pin) to an input port (or input pin) to connect them. A dashed wire follows the pointer and the input port it would snap to is highlighted. A pin-to-pin wire must agree on type, an input pin takes one source (a new wire replaces the old one), and a node cannot connect to itself; refused drops explain why in the status line. Clicking an output port and then an input port still works, and the node settings panel remains the keyboard route for binding pins.

## Flow-to-flow events (Emit Event)

A SYS_ADMIN can add an **Emit Event** node (graph version 2) that names a custom trigger defined under *Function library & triggers*. When it runs, it queues one run of every **other** enabled flow subscribed to that event key, with the node's input as the payload. The payload is checked against the trigger's declared type (text ≤ 2000 characters, number or boolean), exactly as for `POST /api/admin/flow-events/{key}`. The input then passes to the next node unchanged.

- **Durable and exactly-once:** subscriber runs are inserted in one transaction through the durable queue (Liquibase 027). Each carries a key made of parent run, flow, node and subscriber. If the emitting run's worker is lost and the node runs again after resume, no subscriber is queued twice; the trace says how many were already queued.
- **Loop protection:** a flow never triggers itself. Runs started by an emission carry their chain depth, and a run at depth 3 cannot emit further. One emission reaches at most 10 flows.
- **Lineage:** Run History shows "triggered by run #N" on runs an emission started (`parent_run_id` in run listings).
- Saving or publishing a flow that emits an undefined event is refused; the editor's graph validation flags it too.

## Durable run queue, checkpoints and dead-letter

Every run (manual, scheduled, event or retry) is stored as **QUEUED** together with the graph snapshot it will execute (Liquibase change 025). Nothing is lost when workers are busy or the process restarts:

- **Workers:** each process runs two workers that claim the oldest queued run with `SELECT … FOR UPDATE SKIP LOCKED`, so several replicas can share the queue without claiming a run twice. A claim is a 60-second **lease** renewed every 20 seconds; each attempt is limited to 2 minutes.
- **Checkpoints:** before each top-level node, the executor saves its state (completed outputs, variables, branch values). Only the lease holder can write them.
- **Resume:** a run whose lease expires (crash, restart, lost connection) is requeued with a backoff and the next worker **resumes from the last checkpoint**, so completed nodes, including agent calls, are not repeated. Run History marks the resume and shows the attempt (`attempt 2 of 3`). The node that was executing when the worker died runs again, so nodes with side effects (Workspace writes, saved results) must tolerate a repeat; Workspace writes overwrite the same file.
- **Dead-letter:** a run whose worker is lost on every allowed claim (3) becomes **DEAD** instead of retrying forever. **Retry from checkpoint** requeues a FAILED or DEAD run with a fresh claim budget; it resumes before the node that failed.
- **Cancellation** works for queued runs (removed before any worker starts) and running ones, including on another replica: the worker's next lease renewal fails and it stops.
- **Outbox:** event triggers queue one run per subscribed flow; the queued rows are the durable outbox, so an accepted event is not lost on restart. The queue holds at most 200 queued runs; beyond that new manual and event runs get HTTP 429 and due schedules stay due until there is room.
- Runs left RUNNING by versions before the queue (no lease) are still failed after 5 minutes.

API: `POST /api/admin/flows/{id}/run` returns `run_id` and status `queued`; `POST /api/admin/flows/{id}/runs/{run}/retry` requeues a FAILED/DEAD run (`flows.run`). Run listings include `attempts`, `max_attempts` and `queued_at`.

## Drafts, published revisions and rollback

Every flow and Blueprint function has an editable **draft** (what **Save** writes) and, once published, immutable numbered **revisions** (Liquibase change 024: `agent_flow_revisions`, `blueprint_function_revisions`).

- **Publish revision** freezes the saved draft as the next revision (1, 2, 3, …) with an optional note and makes it **active**. It is refused when the draft has unsaved changes in the editor, matches the latest revision, fails validation, or references a missing flow, function or pinned revision. Concurrent publishes of the same draft produce one revision.
- **What runs:** scheduled, event and manual runs execute the **active revision**. A flow that was never published keeps running its draft, so existing flows behave as before until their first publish. **Run draft** tests unpublished edits of a published flow without publishing them (`POST /api/admin/flows/{id}/run` with `{"target":"draft"}`).
- **Rollback:** **Activate** on any older revision makes it the active one immediately. **Restore to draft** copies a revision into the draft for further editing without changing what runs.
- **Pinning calls:** a Call Flow or Function node follows the target's active revision by default (or its draft if never published). Select a specific revision in the node settings to pin it (`flow_revision` / `function_revision`); later publishes or rollbacks of the target then no longer change that call. Missing pinned revisions fail validation on save/publish and fail the run safely.
- **Run History** shows which revision each run executed (`flow_revision`, or *draft*), next to the graph snapshot it already stores.
- **Not versioned:** the enabled flag, schedule interval and event key are live flow settings; changing them takes effect without publishing. Saving now also rejects call nodes whose target flow/function or pinned revision does not exist.
- **Permissions:** owners with `flows.write` publish, activate and restore their own flows under the same restrictions as saving. Functions remain SYS_ADMIN-only. Revisions are deleted with their flow or function.

API: `POST …/publish` (`{"note": "…"}`, ≤500 characters), `GET …/revisions`, `GET …/revisions/{rev}`, `POST …/revisions/{rev}/activate`, `POST …/revisions/{rev}/restore`, under both `/api/admin/flows/{id}` and `/api/admin/blueprint-functions/{id}`. Listings report `active_revision`, `latest_revision` and `has_unpublished_changes`.

## Blueprint-style reusable flow calls (next increment)

Add **Call Flow** nodes in the graph editor and select another saved flow by ID. When the parent flow runs, the selected flow is executed as a nested research function, and its serialized output feeds the following node. Nesting is bounded to four levels and a flow cannot call itself recursively, directly or indirectly. Subflows inherit the parent run's time limit and the read-only/no-tools policy. Missing references fail the run safely.

This is the **first building block**, not yet a full Unreal Engine Blueprint-equivalent visual language. Strongly typed value pins, declared input/output ports, independent user-authored function libraries, parameter binding, custom trigger constructors and step-by-step debugging remain tracked under #59. Nested flow calls currently share the parent run log rather than creating child run records.

## Typed value nodes (Blueprint milestone)

The graph editor now offers a **Value** node with an explicit `text`, `number`, or `boolean` type. Users can connect its `value` output to an agent/subflow `input` port. The backend validates the declared pin names and disallows direct number/boolean-to-text connections. Text constants can be used as the next node's research context. Numeric and boolean constants can be stored in graphs, but typed conversion, arithmetic/comparison nodes, multi-input evaluation and independently defined reusable function graphs have **not** yet been implemented. This milestone preserves existing execution edge behavior; true independent control and data wires are future work under #59.

## Reusable Blueprint functions and custom typed triggers

SYS_ADMIN can use the **Blueprint Function Library** in the Flow Studio dashboard to create a typed single-input/single-output function graph. Nodes are Input, Output, Constant and Operation. Supported operations are `add`, `subtract`, `multiply`, `divide`, `greater`, `less`, `equal`, `and`, `or`, `not`, `if` and `to_text`. Type errors, division by zero, non-finite arithmetic, invalid pin names, unsafe node types and cycles fail closed. Function evaluation is pure, deterministic and requires no AI endpoint.

Use **+ Function** in Flow Studio to add a call node, select a saved function, and connect the preceding value or text context. The function's declared input type must match the current value. It returns its value as context for subsequent nodes. Graphs and functions persist in MySQL.

The **Custom Event Triggers** panel creates registered `event_key` definitions with a `text`, `number` or `boolean` payload type. An enabled flow can subscribe using its event key. To publish an event, an authenticated SYS_ADMIN sends `POST /api/admin/flow-events/{key}` with JSON `{"value":42}` (or a matching text/boolean value). The event's type is validated before matching flows are launched; its value is passed to the downstream flow. No public event ingestion or unauthenticated webhooks are enabled.

**Boundaries for this iteration:** function graphs support one typed argument and one typed return value. More than one typed parameter, reusable version pinning, independent control/data wires, loops, output-of-a-branch selective execution, end-user auth delegation, durable event queues and inter-flow event emission remain under issue #59. As in existing Flow Studio, scheduling/execution is research-only with no brokerage or real-money operations.

## Blueprint function library and custom triggers

SYS_ADMIN can open the Function Library under Flow Studio to create bounded, pure typed functions using input/output, constant and operation nodes. Available operations: arithmetic, comparisons, boolean AND/OR/NOT, IF and explicit to-text conversion. Connect source nodes to named ports, save the graph, then select it from a Function node in a flow. Functions cannot access order APIs, side-effecting tools or live trading.

Custom triggers are registered as unique event keys with text, numeric or boolean payloads. An administrator can enable flows with the matching event key and publish a value through `POST /api/admin/flow-events/{key}` with `{"value":"example"}` for text, `{"value":42}` for numbers or `{"value":true}` for boolean triggers. Triggers require authenticated SYS_ADMIN and matching value types; they are not public inbound webhooks. A flow can subscribe by setting its Event key and Enabled toggle.

**Remaining limitations:** A function graph currently exposes one typed input and one typed output; branch logic selects a value rather than controlling separate execution wires. The flow graph uses the previous node's output as function input. Graph revisions are not pinned; trigger delivery and downstream starts are best-effort and are not durable outbox events. Agent research is read-only, with tool access disabled. Extensible trigger adapters, multiple typed pins and durable propagation remain in #59.

## Current UX delivery

The [October 2026 delivery log](flow-studio-progress.md) covers merged PRs #96–#104: independent History, My Flows, node search, canvas and connection improvements, historical execution graph, run timestamps/failure context, Undo/Redo, Workspace folder deletion, full node tooltip and preflight validation. This does **not** imply automatic node layout, editor draft publishing or complete per-node input/timing capture. The [UX implementation plan](flow-studio-ux-implementation-plan.md) lists remaining work.
