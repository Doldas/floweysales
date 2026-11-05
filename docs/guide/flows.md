---
title: Flow Studio
order: 6
summary: Build saved, repeatable research automations with agents, data steps and approvals, and understand what runs when and who may do what.
last-reviewed: 2026-11-04
---

# Flow Studio

A **flow** is a saved sequence of steps: fetch data, rank companies, ask an agent, wait for your approval, send a message. You build it on a canvas, run it by hand or on a schedule, and read exactly what happened afterward.

Flows are research automation. No node can place a trade. The sections below describe the screen; the full technical model is in [Agent Flow Studio](../agent-flow-studio.md) and the ready-made investor flows are in [Investor automations](../investor-automations.md).

You see Flows in the sidebar only if your role has the flow read permission. Which actions you can take depends on your permissions, as explained under [Who can do what](#who-can-do-what).

## The tabs

| Tab | What it is for |
| --- | --- |
| **My Flows** | A searchable list of your flows with status, trigger and publish state. Start a new flow or open one. |
| **Flow editor** | The canvas. Add nodes, wire them, set each node's options and check the graph. |
| **Run history** | Every run of a flow, with its status, timing and the graph as it was when it ran. Inspect a run node by node. |
| **Approvals & audit** | Steps that wait for a decision, and the record of every change a flow made outside itself. |
| **Function library & triggers** | Reusable functions and custom triggers. Administrators only. |

## Build a flow

1. Open **My Flows**, then **New flow**, or open the editor and pick a template under **Starter templates**. A template is copied as a new, disabled draft.
2. Add nodes with **+ Add node**. A search box helps you find a node. Some nodes are available to administrators only.
3. Connect nodes by clicking ports or dragging wires. Select a node to open its settings.
4. Check **Graph validation**. It lists structural and configuration problems and selects the node to fix.
5. **Save flow**, then **Run now**.

A flow has exactly one trigger and 2 to 16 nodes with 1 to 32 connections, and it cannot contain cycles. You can undo and redo graph changes, fit the flow to the screen and move nodes freely.

**Branches:** a node takes several inputs only when they come from opposite sides of the same If / Else, or through a **Merge** node, which collects everything it receives into a list. The editor and the server both check this before you save.

## Kinds of nodes

- **Agent:** asks the model a question, with your prompt and the data wired into it. By default an agent node has no tools and answers from its input. It can be set to require that every figure in its answer matches a number in its input or in its tool results. This is a mechanical check against invented numbers, not a fact check.
- **Data and investor nodes:** watchlist quotes, technical scan, fundamentals, SEC filings, news headlines, leaderboard, backtest, insider activity, macro regime, corporate events, portfolio check and wealth plan. These produce rows from provider data computed on the server.
- **Logic and shaping:** If / Else, Merge, JSON field, Loop, Value, Transform, Screen (keep rows that match a rule), Only new since last run, Report table and Output. Any node can be set to skip when its input is empty.
- **Web:** HTTP JSON/XML, web crawler and web search.
- **Notify, Write file and Emit event:** change something outside the run and are reserved for administrators. See [Approvals](#approvals-and-the-inbox).

## Run a flow

- **Manual:** **Run now**. Runs are queued and executed in order. While a run waits for a worker you can cancel it.
- **Scheduled:** an enabled flow with an interval (5 minutes to 30 days) runs on that schedule.
- **On an event:** an enabled flow with an event key runs when that event arrives, either from another flow or from a signed webhook.

Scheduled and event triggers are set up by administrators. Everyone with the permission can run their own flows by hand.

A run that is interrupted resumes from its last checkpoint, so completed steps are not repeated. A run that cannot finish is marked as failed or dead, and you can retry it from the checkpoint.

## Versions: draft, publish, roll back

**Save** writes the editable draft. **Publish revision** freezes the draft as a numbered revision, which becomes the active one. Scheduled and event runs always use the active revision; **Run draft** tests the saved, unpublished draft, and **Test unsaved draft** runs what is currently in the editor without saving it (shown in Run history as a *draft test*). Activating an older revision rolls back immediately. A flow that was never published runs its draft.

## Read a run

Open **Run history**, choose a flow and **Inspect nodes**. Each step shows its status, duration, attempts, input and output and any error. For agent nodes with tools, every tool call is listed with its arguments, result size and, for successful calls, when it was retrieved. A step shows a note when its output was cut at the size limit, and a run says when a checkpoint could not be saved.

## Approvals and the Inbox

Only three kinds of node change anything outside a run: **Write file**, **Emit event** and **Notify**. A flow can require approval before such a node runs. The run then shows **Awaiting approval**, and the exact content waits in the **Inbox** and under **Approvals & audit**. An administrator approves or rejects it; a rejection needs a reason. Requests that nobody decides expire after seven days. Every applied side effect is recorded with its target, size, checksum and a redacted preview.

A flow with side effects that is set to run unattended must first have a published revision, validate cleanly and, if any side effect has no approval step, have completed at least one manual run. Otherwise it cannot be switched on.

## Who can do what

| Action | Permission |
| --- | --- |
| See Flow Studio and your flows | flow read |
| Create and edit flows | flow write |
| Run, cancel and retry | flow run |
| Schedules, event triggers, tool grants, Write file, Emit event, Notify, function library, approving side effects | Administrator (SYS_ADMIN) |

Administrators also decide which read-only research tools an agent node may use, up to eight, and with a budget of tool calls.

## Limits

- Flows run on the server in a queue with a bounded length. A full queue refuses new runs until there is room.
- A step's output has a size limit; a step over it fails with a message that names the limit, and a run has an overall output budget.
- Flows do not use model profiles yet. A flow agent node takes a model name from the person who edits the flow.
- Automatic layout and some diagnostics are not available yet.
