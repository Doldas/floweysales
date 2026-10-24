# Flow Studio, Run History and My Workspace — delivery progress

**Status as of 2026-10-24.** This page describes code **merged into `main`**, not just proposed work. Tracking issue: [#95](https://github.com/Doldas/floweysales/issues/95) (still open). The nine UX increments below are merged, but their completion does **not** mean the overall UX roadmap is finished.

## Merged delivery log

| PR | Delivered increment | Result |
| --- | --- | --- |
| [#96](https://github.com/Doldas/floweysales/pull/96) | Flow Studio / History usability and data-integrity foundations | History search and status filters; safer node deletion and unsaved-edit handling |
| [#97](https://github.com/Doldas/floweysales/pull/97) | Independent, live-refreshing History | Separate History flow selection and bounded refresh for active runs |
| [#98](https://github.com/Doldas/floweysales/pull/98) | My Flows dashboard | Saved flow search, status/schedule filters, sorting, create/edit/history and Run now actions |
| [#99](https://github.com/Doldas/floweysales/pull/99) | Searchable node picker | Replaced 17 toolbar add-node buttons with a searchable, role-aware picker |
| [#100](https://github.com/Doldas/floweysales/pull/100) | Canvas navigation and connections | Fit flow, free node positions, curved connections, accessible click-to-connect input/output ports |
| [#101](https://github.com/Doldas/floweysales/pull/101) | Historical execution graph | Run-time graph snapshots, node status visualization, selectable output details, trace-only fallback for older runs |
| [#102](https://github.com/Doldas/floweysales/pull/102) | Run diagnostics | Start/finish timestamps, total duration, per-step recording times, failure context and suspected-node navigation |
| [#103](https://github.com/Doldas/floweysales/pull/103) | Editor undo/redo and Workspace folders | Bounded graph Undo/Redo (40 snapshots); confirmed, owner-scoped recursive Workspace folder deletion |
| [#104](https://github.com/Doldas/floweysales/pull/104) | Preflight validation and full node tooltip | Inline actionable graph checks; every node-inspector field moved into a scrollable canvas tooltip; permanent Details pane removed |

## What users can do now

### Find and configure flows
- Open **My Flows** to search, filter, sort, edit, inspect History or start saved flows.
- Open **Flow Editor** and choose **+ Add node**. The picker searches available node types and hides SYS_ADMIN-only nodes from regular users.
- Click a graph node for its **complete settings** in the enlarged floating tooltip: prompt/model, retries/timeouts, JSON output contract, conditions, loops, URL/crawler settings, typed values, file operations, function/subflow references, and connection/field mapping where applicable. The former persistent Details sidebar was removed.
- Zoom/pan, **Fit flow**, move nodes without the old fixed position clamps, and click output then input ports to connect nodes. Branch-specific and typed field mapping remain available within node settings.
- Use **Undo/Redo** for graph edits, up to 40 history states. History is reset when replacing the graph/flow; metadata undo and coalesced drag transactions are not yet implemented.
- Read **Graph validation** issues, select a node-linked issue to open its tooltip, correct the graph, save, then run. Preflight covers core structural checks, not every backend rule; backend validation remains authoritative.

### Run and troubleshoot
- History selection is separate from the flow being edited; switching History should not overwrite unsaved editor state.
- Search and filter recent runs by text/status, inspect node traces, and compare recorded steps. Active runs refresh with bounded polling while History is open.
- Expand **Inspect nodes** to see the **immutable graph snapshot captured when that run started**. Nodes are colored by recorded SUCCESS/FAILED/RUNNING/SKIPPED status (or not recorded). Select a node to see its persisted status/output preview; old runs lacking snapshots fall back to the trace.
- View run start/finish times, total elapsed time and node-step recording timestamps. Errors have a visible panel with **Inspect failed node** for the recorded failing node; runs recorded before structured attribution show a message-based suspected-node hint instead.
- Side-effect-free replay previews and basic run comparison exist. They are **not** equivalent to a complete deterministic debugger, versioned editor drafts, or replay with real external effects.

### Work with private documents
- **My Workspace** stores small text documents and infers folder structure from paths and `.folder` marker files. Users can create and browse folders, edit/download/delete files, and delete an entire folder including nested files after explicit confirmation.
- Folder deletion is permanent, owner-scoped, transactional, and prevents deleting the root. It does not implement Trash/Restore or an OS filesystem.
- Limits: **2 MiB per file and 256 files per user**. Existing flow workspace read/write nodes remain governed by flow ownership and role restrictions.

## Implementation boundaries (important)

- **Authorization:** Ordinary users may create/run their own manual flows; SYS_ADMIN controls privileged shared functions/subflows, Workspace nodes and scheduled/event-triggered flows. The backend enforces ownership and permissions; hiding a UI control is not the security boundary.
- **Graph semantics:** The server validates flow graphs, cycles, typed edges and allowed node types. UI preflight is advisory and only blocks directly initiated Run now from the editor when issues or unsaved changes exist; it does not replace server-side validation or establish a publish workflow.
- **Run tracing:** Saved node steps contain a bounded output preview, recording timestamp and per-node duration. Since migration 022 a failed run stores the top-level node that raised the error (`failed_node`), and that node gets a FAILED step with its error, duration and, for retryable agent nodes, the attempt count. Errors inside a subflow are attributed to the calling node; cancellations and flow-level errors (validation, snapshot) stay unattributed. Older runs fall back to the message-based "suspected node" hint. Complete raw node input is still not recorded. Output previews may contain sensitive research text; avoid putting secrets into flows.
- **History:** Recent run listings are bounded; full pagination and a permanent event journal are not yet available. Historical graphs cannot be reconstructed for runs from before snapshots were introduced.
- **Execution:** Flow agents remain bounded and research-oriented; no automatic live trading. Function/subflow execution, loops, event triggers, web research and Workspace operations each retain their existing backend policy and limits.

## Next priority backlog

1. **Validator parity:** typed-edge and port checks, JSON contract semantics, branch/output paths, and accessible inline messages that match backend results.
2. **Editor correctness and accessibility:** keyboard shortcuts, transactional drag history, focus and Escape handling for full tooltips, mobile/touch experience, and automated Angular visual/accessibility regression coverage.
3. **History observability:** safe redacted input diagnostics, attempt metrics for successful retried nodes, pagination and richer filters (structured failed-node attribution and per-step duration are delivered).
4. **Graph usability:** auto-layout, true drag-to-connect gestures, branch-specific ports, and standardized node categories.
5. **Publishing/data lifecycle:** separate draft/publish/enable states, versioned revisions, safe rollback, optional Workspace trash/restore and scoped permissions.

See [UX implementation plan](flow-studio-ux-implementation-plan.md) for the detailed checklist, [Agent Flow Studio](agent-flow-studio.md) for operating concepts, [Flow Engine v2](flow-engine-v2.md) for execution semantics, and [Userspace architecture](userspace-architecture.md) for storage/security details.

## Validation and development policy

All nine PRs above were **merged into `main`**; this is a merge-status statement, not a fresh production acceptance test. Future changes must still meet the repository's protected-branch naming, linked-issue, review and required-CI policies. For local verification, use the repository's Go tests/format checks and Angular build, then exercise node editing, run tracing and Workspace folder deletion interactively.
