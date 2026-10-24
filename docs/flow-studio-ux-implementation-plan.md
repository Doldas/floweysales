# Floweysales Flow Studio & History UX implementation plan

**Updated 2026-10-24.** See [the merged delivery report](flow-studio-progress.md) for PRs #96–#104, feature scope, known limits and next priorities. Checked items reflect code merged to `main`, not blanket production acceptance.

## Goal
Make flows easy to discover, build, run, inspect and troubleshoot without moving between unrelated screens. Preserve existing backend security and flow execution semantics. Deliver in reviewable increments with automated tests and accessibility checks.

## P0 — Usability and data integrity
- [x] Select a flow in Run History without opening/changing the editor. History requests, compare, cancel, replay and trace must use that selection.
- [x] Put Delete node in the quick tooltip; keep only the corner X for closing; never allow trigger removal.
- [x] Add a visible unsaved-changes indicator and confirm before replacing a flow or applying a template.
- [x] Add a History search and status filter.
- [x] Add bounded graph Undo/Redo controls (40 snapshot states; #103).
- [ ] Add keyboard shortcuts, coalesced drag transactions and undo for flow metadata.
- [ ] Separate draft from active flow: save draft, validate and explicitly publish/enable.
- [x] Protect run-list and selected node-trace History responses from stale selections (#97, #101).
- [ ] Add consistent stale-response guards for replay previews and comparisons.
- [ ] Add automated Angular tests for navigation, deletion, unsaved work and history selectors.

## P1 — Flow management
- [x] Create initial My Flows dashboard with search, sorting, enabled/schedule indicators, create/edit/run/history actions.
- [ ] Extend the dashboard with latest run status/time and duplicate action once backend metadata is available.
- [x] Replace dense add-node toolbar with a searchable, role-aware node picker using existing node creation actions.
- [ ] Extend the picker with categories, richer descriptions and keyboard-first focus handling.
- [ ] Introduce a consistent editor command bar and clear Save, Saved, Saving, Failed indicators.
- [x] Add inline preflight validation for graph size, node connectivity, unreachable/cyclic nodes, prompts, identifiers and selected required configuration, with click-through node fixes.
- [ ] Extend validation to backend-parity typed edges, JSON schema semantics and branching/output path requirements.
- [x] Allow freely placed nodes, curved edges, clickable connection ports and Fit flow viewport control without changing graph serialization.
- [ ] Add graph auto-layout, drag-to-connect gestures, port-specific branch/data mapping and robust connection validation.

## P1 — Run History and diagnostics
- [x] Backend: return run start/end timestamps, total duration, and recorded node step timestamps from existing database columns.
- [ ] Backend: add pagination, flow snapshot version, structured failure node, attempt counts and per-node timing.
- [x] Frontend: display run timing metadata, status labels and step timestamps; show error context in the execution graph.
- [ ] Frontend: add paginated History table, date/trigger filtering, and dedicated failure-node navigation informed by structured backend metadata.
- [x] Bounded active-run polling in History with cleanup, including node trace updates (#97, #101).
- [ ] Add complete run lifecycle events/SSE and richer live diagnostics.
- [ ] Complete execution detail: node input, error field, attempt count, elapsed time and safely redacted secrets (output inspection implemented in the graph).
- [x] Execution graph overlay: render the immutable graph snapshot for the selected run, color nodes by recorded status, inspect node outputs and refresh live traces for active runs.
- [ ] Side-by-side run comparison matching snapshot nodes with clear added/removed/modified annotations.
- [x] Side-effect-free replay preview based on a saved graph snapshot.
- [ ] Extend preview diagnostics and snapshot-aware comparison UX.
- [ ] Optionally aggregate success rate, duration and AI costs when backend data supports it.

## P2 — Consistency and accessibility
- [ ] Extract FlowStudio into FlowList, Canvas, NodePalette, NodeInspector, RunHistory and ExecutionDetail components, and centralize state in a service/store.
- [ ] Accessible keyboard-only connections; semantic names, roles, focus handling, dialogs and Escape behavior.
- [ ] Responsive tablet/mobile layouts focused on viewing and monitoring, plus touch-friendly node editing.
- [ ] Normalize status colors, icons, typography, spacing, confirmation language and notifications.
- [ ] Add visual regression and WCAG 2.2 AA checks for core scenarios.

## Acceptance scenarios
1. In History, switch Flow A → B → A while an unrelated edited flow remains untouched; only selected flow's runs show.
2. Open a node tooltip, remove a non-trigger node and its edges; only the corner X closes the tooltip.
3. Switching an edited flow or applying a template requires a discard decision; cancel preserves edits.
4. Search and status filters work together with clear empty state.
5. Open a failed run and identify the failing node, input, output, and error without searching unrelated logs.
6. Run a long task and receive accurate status updates until completion/cancellation.
7. Navigate major flows using keyboard and a narrow viewport without losing actions or data.

## Release strategy
Implement backend metadata and APIs in backward-compatible increments. Add component tests and run `npm test`, `npm run build`, accessibility and smoke tests before merge. Migrate UI areas incrementally and retain existing flow graph format to protect saved automations.

## Current implementation scope
PRs #96–#104 have been merged to `main`. Several P0/P1 deliverables are implemented; unchecked features still require work. Review the [progress report](flow-studio-progress.md) for exact scope.

## Follow-up increment — editor safety and Workspace folders
- [x] Add bounded graph Undo/Redo in Flow Editor, with history cleared on flow/template changes.
- [x] Add explicit recursive folder deletion to My Workspace with confirmation, owner scoping and a single database transaction.
- [x] Add actionable graph preflight validation, including jump-to-node diagnostics (#104).
- [ ] Extend Undo/Redo to metadata and match all backend validation rules.

## Consolidated node editing
- [x] Put every per-node inspector field, node-to-node links, typed mapping and Delete node into an expanded canvas tooltip.
- [x] Remove the standalone selected-node Details panel to give the graph the full editor width.
- [ ] Consider extracting the full tooltip into a standalone inspector component without reintroducing a permanent sidebar.

## Current milestone status

- Merged through PR #104 as of 2026-10-24; issue #95 remains open.
- Current UX includes My Flows, role-aware node picker, Fit flow, click-to-connect, complete node tooltip replacing Details panel, graph Undo/Redo, preflight, historical execution graph, run timestamps and Workspace folder deletion.
- Automated UI, accessibility, schema/typed-edge validator parity, auto-layout, publishing/revisions and richer diagnostics are still open.
- See [the PR-by-PR progress report](flow-studio-progress.md).
