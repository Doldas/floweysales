# Issue #262: focused UI consistency

Status: changes committed in this PR, pending CI/review; parent issue still open.

Delivered: Keep shared trigger diagram node identities stable across duplicate event names.

Validation: `node --test frontend/src/app/ui/flow-canvas/diagram-adapters.spec.ts` and normal frontend CI.

Remaining: Wider multi-surface UX, accessibility and visual acceptance criteria are not claimed complete by this targeted regression.

## Frontend/backend graph contract

`internal/server/flow_graph_contract_test.go` fails when the editor's `FlowNode`, `FlowEdge` or `FlowGraph` interface and the server's persisted structs list different fields (a field known to one side is dropped on the next save), and when a fully populated graph does not survive a decode and encode unchanged. Visual parity on every surface and the shared Go graph package remain open.
