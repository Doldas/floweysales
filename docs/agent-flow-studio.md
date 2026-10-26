# Agent Flow Studio — user guide and execution model

Authenticated Flow Studio includes a **My Flows** dashboard, visual graph editor, Run History and My Workspace. Saved graphs live in the Liquibase-managed `agent_flows` table, run metadata in `agent_flow_runs`, and bounded node traces and per-run graph snapshots in separate tables. SYS_ADMIN can manage scheduled/event flows and privileged nodes; ordinary users can create and run their own manual-only flows. See [the merged UX progress log](flow-studio-progress.md).

## Supported nodes and triggers

- Exactly one **Trigger** root and 2–16 total nodes / 1–32 edges; the server rejects cycles and disconnected nodes. Besides agents, versioned graphs support conditions, merge, JSON field extraction, bounded data loops, value/transform/output, HTTP/web research and privileged function/subflow and Workspace nodes.
- **Manual:** click Run now on a saved flow.
- **Interval schedule:** enable the flow and set `interval_minutes` to 5–10080. The Go scheduler polls every 30 seconds and, in one MySQL transaction, claims the due interval and queues its run, so replicas never double-run an interval and a crash never loses one.
- **Event key:** enable a flow with a key such as `market.close`; authenticated SYS_ADMIN can call `POST /api/admin/flow-events/market.close`. Other services can deliver the same events without a session through a **signed inbound webhook** (event adapter, see [below](#signed-inbound-webhooks-event-adapters)).

The editor supports a searchable role-aware node picker, free positioning, zoom/pan, **Fit flow**, curved links, accessible click-to-connect ports and bounded **Undo/Redo** of graph changes. Selecting a node opens the **complete floating settings tooltip** (the former permanent Details panel was removed). Graph preflight reports core structural/configuration problems with click-to-node fixes; the server remains authoritative for all execution and permission checks. Version 2 execution routes values along edges and selects conditional branches in topological order.

## Safety and remaining work

- Agent nodes have **no tools** unless a SYS_ADMIN grants some, and only read-only research tools can be granted (see [Research tools for agent nodes](#research-tools-for-agent-nodes)). No tool can place orders, change approvals or modify the system.
- Flows **never execute live trades**. An admin must separately enable authentication and configure an AI endpoint/model.
- Runs go through a durable queue with checkpoint resume and dead-lettering (see below); concurrency is bounded per process.
- The flow graph rejects cycles. Recurring loops happen at the scheduler level, not through unbounded edges.
- **Already available:** conditional/branch nodes, bounded agent retries/timeouts, owner-scoped manual flows, cancellation, recorded node steps, immutable run graph snapshots, execution timestamps, preview-only replay, draft/publish revisions with rollback and call pinning, durable checkpoints and dead-lettering, per-node timing/attempt data, and audited side effects with optional approval (see below), signed inbound webhooks for third-party events, and scoped read-only research tools for agent nodes. An automated accessibility check (axe-core, WCAG 2.1 A/AA) covers every Flow Studio tab, keyboard-opened node settings and the run trace in CI (`npm run smoke:a11y`). **Still planned:** browser UI tests for the main editing paths and adversarial tests (#139).

To start, sign in and open **My Flows → New flow / Edit**. Add nodes with **+ Add node**, select a graph node to configure all fields in its floating tooltip, check **Graph validation**, then **Save flow** before **Run now**. Use **Run history → Inspect nodes** to view recorded steps, status-colored historical graph snapshots, duration and errors. Scheduling and typed event triggers require SYS_ADMIN.

## Branch, merge and type rules

In version 2 graphs a node runs when one of its inputs is active, and fails if several are active at once. The editor and the server therefore check, before a flow is saved or published:

- **Joining branches:** a node may take several inputs only when they come from opposite `true`/`false` branches of the same IF / ELSE (at most one can be active). Inputs that can arrive together, such as two steps that both follow the trigger or the same branch twice, need a **Merge** node, which collects every active input into a JSON array.
- **Typed pins:** each input pin of a function takes one source, and all of a function's pins must be fed from the same branches, so a pin is never missing when only one branch runs.
- **Types:** a pin that expects `number` or `boolean` accepts a constant, condition result or function output pin of that type. `text` pins accept any value. Values whose type is only known at run time, such as agent output or JSON fields, are still checked when the function runs.
- **Duplicates:** the same connection cannot be added twice, in any graph version.

The validation panel lists these problems and selects the affected node. Drawing or adding a connection that breaks a rule shows the problem straight away.

## Side effects, approvals and audit trail

Only two node types change anything outside a run: **Write Workspace File** (capability `workspace.write`) and **Emit Event** (capability `events.emit`). Both are SYS_ADMIN-only. Each one's scope is fixed by the node itself, either one file in the flow owner's workspace or one defined event, so a run can never write another path or emit another event. Agents still run without tools, and reads (HTTP GET, web search, crawling, workspace reads) are not side effects. The editor lists a flow's side effects above Graph validation, and each side-effect node's settings show its capability.

- **Audit trail:** every applied side effect is recorded with its flow, node, run, capability, target, payload size, SHA-256 and a redacted preview. The record survives deletion of the flow and its runs. **Flow Studio → Approvals & audit** lists the trail; flow owners see their own flows and SYS_ADMIN sees all of them.
- **Approval:** tick **Require SYS_ADMIN approval before this runs** on a side-effect node. When a run reaches the node it stores the exact payload, and the run shows as **Awaiting approval** (status `AWAITING_APPROVAL`, not polled, can be cancelled). A SYS_ADMIN reviews the content under Approvals & audit:
  - **Approve:** the run is requeued with a fresh claim budget, resumes from its checkpoint at that node, and applies the stored payload exactly as reviewed. The payload is then dropped and only the digest and preview are kept.
  - **Reject:** the run fails with the reason, which is required. Nothing is applied.
- **Expiry and cancellation:** an undecided request expires after 7 days and fails its run. Cancelling the run or deleting the flow cancels a pending or approved request so it can never be applied. Retrying a failed run asks for approval again.
- **Limits:** approval gates run only in queued top-level runs. A gated node inside a flow used as a Call Flow fails instead of parking its caller. Replay previews never perform side effects. Both decisions are added to the run's step trace.

API: `GET /api/admin/flow-side-effects?status=&flow_id=`, `GET /api/admin/flow-side-effects/{id}` (SYS_ADMIN, includes the payload under review) and `POST /api/admin/flow-side-effects/{id}/decision` with `{"approve": true|false, "note": "…"}` (SYS_ADMIN). This approves research automation output only: there is no trading and no real money.

### Production-readiness gate

A flow with side effects may run unattended, on a schedule or from an event, only when its **published revision** passes the readiness checks. Automation always runs that revision. The checks are:

1. **Published.** The flow has a published revision.
2. **Valid.** The revision validates, and every flow, function and event it calls or emits exists.
3. **Supervised.** If any side effect runs without approval, the revision has completed at least one **manual** run, so a person has seen what it writes or emits. A revision whose side effects all require approval passes, because each unattended run is reviewed anyway.

Flows without side effects are not gated. Where the gate is enforced:

- **Saving:** saving a flow with automation on (Enabled plus a schedule or event key) is refused with `409` until the checks pass. Save it disabled, publish, run it once, then enable it.
- **Publishing or activating:** when you publish or activate a revision of an automated flow and the new revision does not pass, automation is **paused** (Enabled is turned off; the schedule and event key are kept). The response says so with `automation_paused` and the failing checks.

The editor shows a **Production readiness** panel for flows with side effects or automation. `GET /api/admin/flows/{id}/readiness` returns the same checks. Flows that were automated before the gate existed keep running until their next save or publish. None of this enables trading.

### Change log and authorization

Every successful change to Flow Studio is recorded in `flow_audit_log`: creating, updating, deleting, publishing, activating or restoring flows and functions, saving triggers, sending events, starting, cancelling and retrying runs, and side-effect decisions. Each record holds the actor, the action, the target, the matched route, the HTTP status, a SHA-256 of the request body (the body itself is not stored) and a short summary. Refused requests are not recorded. SYS_ADMIN reads it under **Approvals & audit → Change log** or with `GET /api/admin/flow-audit?target_type=&target_id=&action=&before=`.

The audit middleware matches routes by pattern, and a unit test fails if a mutating Flow Studio route is neither audited nor explicitly exempt (the two previews). The MySQL authorization test checks that USER accounts get 403 from every SYS_ADMIN-only Flow Studio route (functions, triggers, events, approvals, change log), and that they can read the side-effect audit of their own flows.

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

## Signed inbound webhooks (event adapters)

An **event adapter** lets a third-party service (a market-data alert, a CI job, a monitoring tool) deliver one custom trigger without a Flowey session. A SYS_ADMIN creates it under *Function library & triggers → Inbound webhooks* (Liquibase 030). Each adapter has:

- a **public URL** `POST /api/hooks/flow-events/{slug}` with a random, unguessable 26-character slug,
- exactly **one event key**. The adapter cannot deliver any other event, and the key cannot change after creation,
- a **signing secret** (`whsec_…`), shown only when created or rotated. It is stored sealed with AES-GCM under `FLOWEYSALES_SECRETS_KEY` and bound to the adapter's slug, so creating adapters requires that key,
- an optional **payload pointer** (RFC 6901, e.g. `/data/price`) that selects the event value from the JSON body. Empty uses the whole body. Text events accept any JSON value (objects and arrays as compact JSON); number and boolean events need a scalar of that type,
- a **rate limit** of 1–600 deliveries per minute (default 30), counted per instance.

**Signing.** Every delivery carries three headers:

| Header | Value |
| --- | --- |
| `X-Flowey-Timestamp` | Unix seconds; refused when more than 5 minutes from server time |
| `X-Flowey-Delivery` | Sender's unique delivery ID, 1–128 characters `[A-Za-z0-9._:-]` |
| `X-Flowey-Signature` | `sha256=` + hex HMAC-SHA256 of `<timestamp>.<delivery id>.<raw body>` with the secret |

```sh
BODY='{"data":{"price":101.5}}'
TS=$(date +%s); ID=$(uuidgen)
SIG=sha256=$(printf '%s.%s.%s' "$TS" "$ID" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | sed 's/^.* //')
curl -X POST "https://flowey.example/api/hooks/flow-events/$SLUG" -H 'Content-Type: application/json' \
  -H "X-Flowey-Timestamp: $TS" -H "X-Flowey-Delivery: $ID" -H "X-Flowey-Signature: $SIG" -d "$BODY"
```

The panel shows the same example for each new secret.

**Responses.** `202 {"status":"accepted","queued":N}` once every enabled subscriber is queued; `200 {"status":"duplicate"}` for a delivery ID already accepted; `401` for a missing or invalid signature or a stale timestamp; `413` above 16 KiB; `422` when the value is missing or has the wrong type; `429` (with `Retry-After`) over the rate limit; `503` when the run queue is full or storage is unavailable, which is safe to retry. Unknown and **disabled** adapters both answer `404`.

**Guarantees.**
- **Exactly once per subscriber:** runs are queued through the durable outbox with a key made of adapter, delivery ID and flow. A sender that retries after a timeout, even one where the first attempt queued runs but failed to answer, never starts a subscriber twice. Because the delivery ID is signed, a captured request cannot be replayed under a new ID, and accepted IDs are kept for 30 days, far longer than the 5-minute timestamp window.
- Runs start with trigger `webhook` at event depth 0, so they may emit further events within the usual chain limit (3). One delivery reaches at most 10 flows.
- The production-readiness gate still applies: an event-subscribed flow with side effects must be published and have run manually before it can be enabled.
- Rate-limited requests are counted before signature checks, so a flood of unsigned requests is refused without writing to the database. Up to the limit, every request to an existing adapter is logged under **Deliveries**: status, reason, delivery ID, SHA-256 of the body (never the body itself) and runs queued. The log is pruned after 30 days.
- Creating, updating, rotating and deleting adapters is recorded in the change log. Rotating takes effect immediately: deliveries signed with the old secret are refused.

Admin API (SYS_ADMIN): `GET|POST /api/admin/flow-event-adapters`, `PUT|DELETE /api/admin/flow-event-adapters/{id}` (`name`, `payload_pointer`, `rate_per_minute`, `enabled`), `POST /api/admin/flow-event-adapters/{id}/rotate-secret`, `GET /api/admin/flow-event-adapters/{id}/deliveries` (latest 50).

## Research tools for agent nodes

A SYS_ADMIN can let an agent node gather its own evidence instead of answering from its prompt alone. In the node's settings, **Research tools** lists the grantable tools. Tick up to 8 and optionally set **Max tool calls** (1–16; default 6).

- **Grantable catalogue (read-only):** symbol search, price history, technical analysis, latest close, provider quote, quarterly financials, financial-ratio and TTM-valuation calculators, provider capabilities/endpoints/research operations, SEC filings, SEC XBRL facts, SEC filing sections, the DCF/WACC/ROIC calculators and web search. `GET /api/admin/flow-agent-tools` returns the list with descriptions. Tools that read the engine's own tracker state (engine config, recorded ticks, market stats) cannot be granted. A tool added to the chat catalogue later is not grantable until it is explicitly added to this list.
- **Enforced on the server:** the model is offered only the granted tools, and a call to any other tool is refused without running it. Saving or publishing a flow that grants an unknown or non-grantable tool, more than 8 tools, or a budget without tools is refused. Granting tools is SYS_ADMIN-only: a flow with a tool grant cannot be saved or run by an ordinary user, and nodes without grants behave exactly as before.
- **Budget:** once the call budget is spent, the tools are withdrawn and the agent must answer with what it has. The node does not fail. A granted agent's default timeout is 90 s instead of 45 s. Retries (0–2) remain allowed because no granted tool has side effects.
- **Trace:** every call is recorded on the node's run step (Liquibase 031, `tool_calls`): attempt, tool name, redacted arguments (300 characters), success or redacted error and result size. Results themselves are not stored. At most 32 calls are kept per node. *Run history → Inspect nodes* lists them, and failed nodes keep their trace too.
- Tool results are passed to the model as untrusted data, and the system prompt asks it to cite the tool and period behind every number. Tools such as SEC facts and web search use the server's configured providers and keys, so their rate limits and costs apply.

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

This is the **first building block**, not yet a full Unreal Engine Blueprint-equivalent visual language. Typed multi-pin functions, revisions, drag-to-wire, custom triggers and the run debugger have since shipped (#114, #118, #131–#133); see the sections above. Nested flow calls currently share the parent run log rather than creating child run records.

## Typed value nodes (Blueprint milestone)

The graph editor now offers a **Value** node with an explicit `text`, `number`, or `boolean` type. Users can connect its `value` output to an agent/subflow `input` port. The backend validates the declared pin names and disallows direct number/boolean-to-text connections. Text constants can be used as the next node's research context. Numeric and boolean constants can be stored in graphs, but typed conversion, arithmetic/comparison nodes, multi-input evaluation and independently defined reusable function graphs have **not** yet been implemented. This milestone preserves existing execution edge behavior; true independent control and data wires are future work under #59.

## Reusable Blueprint functions and custom typed triggers

SYS_ADMIN can use the **Blueprint Function Library** in the Flow Studio dashboard to create a typed single-input/single-output function graph. Nodes are Input, Output, Constant and Operation. Supported operations are `add`, `subtract`, `multiply`, `divide`, `greater`, `less`, `equal`, `and`, `or`, `not`, `if` and `to_text`. Type errors, division by zero, non-finite arithmetic, invalid pin names, unsafe node types and cycles fail closed. Function evaluation is pure, deterministic and requires no AI endpoint.

Use **+ Function** in Flow Studio to add a call node, select a saved function, and connect the preceding value or text context. The function's declared input type must match the current value. It returns its value as context for subsequent nodes. Graphs and functions persist in MySQL.

The **Custom Event Triggers** panel creates registered `event_key` definitions with a `text`, `number` or `boolean` payload type. An enabled flow can subscribe using its event key. To publish an event, an authenticated SYS_ADMIN sends `POST /api/admin/flow-events/{key}` with JSON `{"value":42}` (or a matching text/boolean value). The event's type is validated before matching flows are launched; its value is passed to the downstream flow. Other services publish the same events through signed [event adapters](#signed-inbound-webhooks-event-adapters); there is no unsigned public ingestion.

**Boundaries for this iteration:** this section describes the first single-pin function iteration. Typed multi-pin functions, revision pinning, durable event queues and inter-flow event emission shipped later (see above). Independent control/data wires, loops and end-user auth delegation are not implemented. As in existing Flow Studio, scheduling/execution is research-only with no brokerage or real-money operations.

## Blueprint function library and custom triggers

SYS_ADMIN can open the Function Library under Flow Studio to create bounded, pure typed functions using input/output, constant and operation nodes. Available operations: arithmetic, comparisons, boolean AND/OR/NOT, IF and explicit to-text conversion. Connect source nodes to named ports, save the graph, then select it from a Function node in a flow. Functions cannot access order APIs, side-effecting tools or live trading.

Custom triggers are registered as unique event keys with text, numeric or boolean payloads. An administrator can enable flows with the matching event key and publish a value through `POST /api/admin/flow-events/{key}` with `{"value":"example"}` for text, `{"value":42}` for numbers or `{"value":true}` for boolean triggers. The admin endpoint requires authenticated SYS_ADMIN and matching value types; third parties use a signed [event adapter](#signed-inbound-webhooks-event-adapters) instead. A flow can subscribe by setting its Event key and Enabled toggle.

**Remaining limitations:** Branch logic selects a value rather than controlling separate execution wires. The flow graph uses the previous node's output as function input. Agent nodes have no tools unless a SYS_ADMIN grants read-only research tools (see *Research tools for agent nodes*).

## Current UX delivery

The [October 2026 delivery log](flow-studio-progress.md) covers merged PRs #96–#104: independent History, My Flows, node search, canvas and connection improvements, historical execution graph, run timestamps/failure context, Undo/Redo, Workspace folder deletion, full node tooltip and preflight validation. This does **not** imply automatic node layout, editor draft publishing or complete per-node input/timing capture. The [UX implementation plan](flow-studio-ux-implementation-plan.md) lists remaining work.
