# Graph contract fixtures

Saved graphs of every kind, shared by the Go and the TypeScript tests (issue #262). Each file is a
graph as the app stores it. Both languages must read the same file into the same nodes, edges,
ports and coordinates, and neither may lose or invent anything.

| File | What it is | Go reads it with | TypeScript reads it with |
| --- | --- | --- | --- |
| `flow-research.json` | A flow, graph version 2 | `validateFlow`, `flowGraph` | `runSnapshotDiagram` |
| `function-untyped.json` | A function, no version (untyped) | `validateBlueprintFunction`, `functionGraph` | `functionDiagram` |
| `function-typed.json` | A function with typed pins, version 2 | `validateBlueprintFunction`, `blueprintV2Graph` | `typedFunctionDiagram` |
| `triggers.json` | Custom event triggers | `storage.BlueprintTrigger` | `triggerDiagram` |
| `run-statuses.json` | Step statuses for a run snapshot of `flow-research.json` | node ids only | `runSnapshotDiagram` |

Rules for changing them:

- A new field of a stored graph goes into a fixture in the same change that adds it, so a field one
  side does not know about fails here instead of being dropped in production.
- Fixtures are valid by the real validators. Do not weaken a validator to make a fixture pass.
- Tests: `go test ./internal/server -run GraphContract` and
  `node --test frontend/src/app/ui/flow-canvas/graph-contract.spec.ts`.
