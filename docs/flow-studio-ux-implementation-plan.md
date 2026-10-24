# Floweysales Flow Studio & History UX implementation plan

## Goal
Make flows easy to discover, build, run, inspect and troubleshoot without moving between unrelated screens. Preserve existing backend security and flow execution semantics. Deliver in reviewable increments with automated tests and accessibility checks.

## P0 — Usability and data integrity
- [x] Select a flow in Run History without opening/changing the editor. History requests, compare, cancel, replay and trace must use that selection.
- [x] Put Delete node in the quick tooltip; keep only the corner X for closing; never allow trigger removal.
- [x] Add a visible unsaved-changes indicator and confirm before replacing a flow or applying a template.
- [x] Add a History search and status filter.
- [ ] Add keyboard shortcuts and undo/redo using an editor command history, including drag transactions.
- [ ] Separate draft from active flow: save draft, validate and explicitly publish/enable.
- [ ] Ensure stale async History responses never overwrite a newer selection; cancel/ignore pending trace, preview and compare.
- [ ] Add automated Angular tests for navigation, deletion, unsaved work and history selectors.

## P1 — Flow management
- [x] Create initial My Flows dashboard with search, sorting, enabled/schedule indicators, create/edit/run/history actions.
- [ ] Extend the dashboard with latest run status/time and duplicate action once backend metadata is available.
- [x] Replace dense add-node toolbar with a searchable, role-aware node picker using existing node creation actions.
- [ ] Extend the picker with categories, richer descriptions and keyboard-first focus handling.
- [ ] Introduce a consistent editor command bar and clear Save, Saved, Saving, Failed indicators.
- [ ] Add graph validation (disconnected nodes, required inputs, invalid schema, trigger and output paths) before Run.
- [x] Allow freely placed nodes, curved edges, clickable connection ports and Fit flow viewport control without changing graph serialization.
- [ ] Add graph auto-layout, drag-to-connect gestures, port-specific branch/data mapping and robust connection validation.

## P1 — Run History and diagnostics
- [x] Backend: return run start/end timestamps, total duration, and recorded node step timestamps from existing database columns.
- [ ] Backend: add pagination, flow snapshot version, structured failure node, attempt counts and per-node timing.
- [x] Frontend: display run timing metadata, status labels and step timestamps; show error context in the execution graph.
- [ ] Frontend: add paginated History table, date/trigger filtering, and dedicated failure-node navigation informed by structured backend metadata.
- [ ] Live run updates using bounded polling/SSE with cleanup, rather than a single 3-second refresh.
- [ ] Complete execution detail: node input, error field, attempt count, elapsed time and safely redacted secrets (output inspection implemented in the graph).
- [x] Execution graph overlay: render the immutable graph snapshot for the selected run, color nodes by recorded status, inspect node outputs and refresh live traces for active runs.
- [ ] Side-by-side run comparison matching snapshot nodes with clear added/removed/modified annotations.
- [ ] Replay preview with explicit non-side-effect mode and clear relationship to its saved graph snapshot.
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
The first P0 improvements are implemented in the companion PR. Other tasks above remain planned and should not be considered shipped.

## Follow-up increment — editor safety and Workspace folders
- [x] Add bounded graph Undo/Redo in Flow Editor, with history cleared on flow/template changes.
- [x] Add explicit recursive folder deletion to My Workspace with confirmation, owner scoping and a single database transaction.
- [ ] Extend Undo/Redo to flow metadata changes and add richer graph validation before running.
