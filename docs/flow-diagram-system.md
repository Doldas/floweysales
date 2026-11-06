# Flow diagram system

Every graph in the app (Flow Editor, Functions, Triggers, typed-function previews and run-history snapshots) draws with one set of shared primitives. The Flow Editor is the canonical look: improving a primitive improves every surface, and a new surface must not carry its own renderer. This page is the contract. Issue: #262.

## Ownership

| Piece | File | Owns |
| --- | --- | --- |
| Geometry | `frontend/src/app/ui/flow-canvas/flow-canvas-geometry.ts` | Node card size (125 x 70), port placement, the Bezier connector, fit/zoom math, status-to-tone mapping, arrow-key neighbor lookup. Pure, tested with `node --test`. |
| Styles | `frontend/src/app/ui/flow-canvas/flow-canvas-styles.ts` | `FLOW_CANVAS_STYLES`: node cards, accent bars per node kind, ports, connectors (data, execution, event variants), selection, focus, state tones, navigation toolbar, forced-colors fallback. Uses only the `--fs-*` tokens from `styles/_tokens.scss`, so light and dark follow the theme. |
| Component | `frontend/src/app/ui/flow-canvas/flow-diagram.ts` (`<fse-flow-diagram>`) | SVG canvas, pan (drag/touch), wheel and pinch zoom, Zoom in/out, Fit and Reset view, keyboard navigation, selection, empty state. It never edits coordinates. |
| Adapters | `frontend/src/app/ui/flow-canvas/diagram-adapters.ts` | Read-only projections of each surface's own graph model into diagram nodes and edges (`functionDiagram`, `typedFunctionDiagram`, `triggerDiagram`, `runSnapshotDiagram`, `flowNodeTag`). |

The Flow Editor's editable canvas (`flow-studio.ts`) keeps its own template because it owns wiring, dragging and port editing, but it includes `FLOW_CANVAS_STYLES` and the same geometry constants, so it cannot drift visually.

## Surfaces

| Surface | Renders with | Semantic variant shown |
| --- | --- | --- |
| Flow Editor | Own editable template + shared styles | Node type accent and caption, typed pins on function nodes |
| Functions (untyped) | `<fse-flow-diagram variant="function">` | Input, value, operation and output nodes; operation inputs as named ports |
| Functions (typed pins) | `<fse-flow-diagram variant="typed-function">` | Data wires (solid) and execution wires (dashed) between named typed ports |
| Triggers | `<fse-flow-diagram variant="trigger">` | Event node feeding a typed payload node over an event connector (dotted); read-only |
| Run history snapshot | `<fse-flow-diagram variant="run-snapshot">` | Node tone and text for succeeded, failed, running, skipped; read-only |

State is never carried by color alone: tones always add a text line (for example "failed"), and nodes have an accessible name that includes the status.

## Rules for new diagram types

1. Render through `<fse-flow-diagram>`. If a feature needs editing, add an output to the component (or an overlay through content projection), not a second canvas.
2. Write an adapter in `diagram-adapters.ts` from your model to `DiagramNode`/`DiagramEdge`, with a spec. Adapters must not change ids, ports, coordinates or settings of the persisted graph; layout derived for display (as for triggers) is never saved.
3. Pick a `kind` (accent color), a caption and, where relevant, typed ports. Add a new accent in `flow-canvas-styles.ts` only for a genuinely new semantic kind.
4. Use `--fs-*` tokens only. No hex colors or fixed fonts in a surface.
5. Keep the shared interaction contract: nodes are focusable buttons (Enter or Space selects, arrow keys move between nodes), `+`, `-` and `0` zoom and reset, Fit and Reset view are always present, and the canvas works at phone width (it scales to the container; pan and pinch work by touch).
6. Add the surface to `frontend/e2e/flow-diagrams.spec.ts` (same card geometry as the editor, controls, keyboard, dark mode and no sideways page scroll).

## Persistence and runtime

The diagram layer is presentation only. Saved flows, functions and triggers keep their stored schema (`version`, node ids, `x`/`y`, ports, settings); the graph validators, versioning and execution paths are unchanged. One deliberate change: nodes added to a typed function from the form are now placed on a grid instead of at 0,0, so they are visible in the preview. Existing nodes keep their stored coordinates.

## Not covered yet

The issue also asks for a shared backend graph contract (common Go packages for validation, serialization and versioning, with generated TypeScript types and cross-language round-trip tests). That is a separate, larger change: today flows, untyped functions and typed functions each have their own validators in Go. This PR does not change them. It is tracked on #262.
