# Flow diagram system

Every graph in the app (Flow Editor, Functions, Triggers, typed-function previews and run-history snapshots) draws with one set of shared primitives. The Flow Editor is the canonical look: improving a primitive improves every surface, and a new surface must not carry its own renderer. This page is the contract. Issue: #262.

## Ownership

| Piece | File | Owns |
| --- | --- | --- |
| Geometry | `frontend/src/app/ui/flow-canvas/flow-canvas-geometry.ts` | Node card size (125 x 70), port placement, the Bezier connector, fit/zoom math, status-to-tone mapping, arrow-key neighbor lookup. Pure, tested with `node --test`. |
| Styles | `frontend/src/app/ui/flow-canvas/flow-canvas-styles.ts` | `FLOW_CANVAS_STYLES`: node cards, accent bars per node kind, ports, connectors (data, execution, event variants), selection, focus, state tones, navigation toolbar, forced-colors fallback. Uses only the `--fs-*` tokens from `styles/_tokens.scss`, so light and dark follow the theme. |
| Component | `frontend/src/app/ui/flow-canvas/flow-diagram.ts` (`<fse-flow-diagram>`) | SVG canvas, pan (drag/touch), wheel and pinch zoom, Zoom in/out, Fit and Reset view, keyboard navigation, selection, empty state. With `editable` it also reports card drags, wires drawn between ports and port activation. It never changes the graph itself. |
| Adapters | `frontend/src/app/ui/flow-canvas/diagram-adapters.ts` | Read-only projections of each surface's own graph model into diagram nodes and edges (`editorDiagram`, `functionDiagram`, `typedFunctionDiagram`, `triggerDiagram`, `runSnapshotDiagram`, `flowNodeTag`). |
| Wiring rules | `frontend/src/app/components/wire-drag.ts` | What a wire drawn in the Flow Editor may connect (`wireFromDrag`): output to input, one source per typed pin, matching pin types, no self loops or duplicates. |

The Flow Editor (`flow-studio.ts`) draws through the same component. It decides what the canvas reports means (see Editing below); it has no canvas template of its own.

## Surfaces

| Surface | Renders with | Semantic variant shown |
| --- | --- | --- |
| Flow Editor | `<fse-flow-diagram variant="editor" [editable]="true">` | Node type accent and caption, typed pins on function nodes, live run state |
| Functions (untyped) | `<fse-flow-diagram variant="function">` | Input, value, operation and output nodes; operation inputs as named ports |
| Functions (typed pins) | `<fse-flow-diagram variant="typed-function">` | Data wires (solid) and execution wires (dashed) between named typed ports |
| Triggers | `<fse-flow-diagram variant="trigger">` | Event node feeding a typed payload node over an event connector (dotted); read-only |
| Run history snapshot | `<fse-flow-diagram variant="run-snapshot">` | Node tone and text for succeeded, failed, running, skipped; read-only |

State is never carried by color alone: tones always add a text line (for example "failed"), and nodes have an accessible name that includes the status.

## Editing

A diagram is read-only unless it sets `editable`. An editable canvas draws the graph it is given and reports what the user did with the pointer; the owner changes its own model and passes the next graph. Nothing in the component writes coordinates or edges.

| Input | Meaning |
| --- | --- |
| `editable` | Cards can be dragged (mouse; a touch screen pans instead), output ports start wires, plain ports are focusable buttons. |
| `autoFit`, `fitPadding`, `fitMinWidth`, `fitLabel` | An editor turns `autoFit` off so the view only changes when the user asks; the Flow Editor fits with a 65 unit margin, no further in than half the default width, and calls the button "Fit flow". |
| `showPinLabels` | Names typed pins next to them. |
| `connectingFrom` | The node whose output port was chosen to start a click-to-connect; its port is marked. |

| Output | Meaning |
| --- | --- |
| `nodeSelect`, `canvasClick`, `canvasContextMenu` | Selection, a click on empty canvas, a right click. |
| `nodeDragged` | `{id, x, y, phase}`: where the card would be, in whole canvas units, while it is `start`ed, `move`d and `end`ed. |
| `wireDrawn` | `{source, target}`: a wire dragged from an output port (or typed pin) and dropped on an input port. The owner applies its own rules. |
| `portActivate` | `{nodeId, side}`: a plain port was clicked or Enter was pressed on it; a click that ends a wire drag is not one. |

A node opts in to ports through the diagram model: `inPort` and `outPort` (their accessible names) give a node one plain port in the middle of that side; `inputs` and `outputs` give typed pins and replace the plain port. `accessibleName` replaces the generated name (the Flow Editor names a node "Select <label>"). A toolbar addition goes in through `<ng-content select="[canvasToolbar]">`.

## Rules for new diagram types

1. Render through `<fse-flow-diagram>`. If a feature needs editing, use `editable` and its outputs, or add an output; add an overlay through content projection. Do not write a second canvas.
2. Write an adapter in `diagram-adapters.ts` from your model to `DiagramNode`/`DiagramEdge`, with a spec. Adapters must not change ids, ports, coordinates or settings of the persisted graph; layout derived for display (as for triggers) is never saved.
3. Pick a `kind` (accent color), a caption and, where relevant, typed ports. Add a new accent in `flow-canvas-styles.ts` only for a genuinely new semantic kind.
4. Use `--fs-*` tokens only. No hex colors or fixed fonts in a surface.
5. Keep the shared interaction contract: nodes are focusable buttons (Enter or Space selects, arrow keys move between nodes), `+`, `-` and `0` zoom and reset, Fit and Reset view are always present, and the canvas works at phone width (it scales to the container; pan and pinch work by touch).
6. Add the surface to `frontend/e2e/flow-diagrams.spec.ts` (same card geometry as the editor, controls, keyboard, dark mode and no sideways page scroll). An editable surface also needs a spec like `frontend/e2e/flow-canvas-editing.spec.ts`: drag, wire, connect by keyboard, pan, zoom, context menu, through what a user does and what is saved.

## Persistence and runtime

The diagram layer is presentation only. Saved flows, functions and triggers keep their stored schema (`version`, node ids, `x`/`y`, ports, settings); the graph validators, versioning and execution paths are unchanged. One deliberate change: nodes added to a typed function from the form are now placed on a grid instead of at 0,0, so they are visible in the preview. Existing nodes keep their stored coordinates. Moving the Flow Editor onto the component changed no stored data and, compared pixel by pixel with the old canvas (light and dark, a template with a selected node, typed pins), differs only in four corner pixels of the rounded frame.

## The backend: what every graph shares

Flows, untyped functions and typed functions are different graphs with different node types, ports and rules, and each keeps its own validator in `internal/server`. What they share is answered once, in `internal/graph`:

| Question | Where | Used by |
| --- | --- | --- |
| Does an edge join two different nodes that exist? | `graph.Joins` | all three validators |
| In which order do the nodes run, which nodes does nothing lead to, and is there a cycle? | `graph.Sort` (`Order`, `Roots`, `InDegree`, `Complete`) | the flow validator (and with it the run order), the untyped function evaluator, the typed function validator |

`graph.Sort` is Kahn's algorithm with a fixed tie-break: the roots in the order the nodes were saved, then each node once its last input has run, nodes released by one node in the order their edges were saved. The same graph always gets the same order, and a cycle leaves its nodes (and everything behind it) out of the order. Before the move each validator had its own copy (two of Kahn's algorithm, one depth-first search); the error messages did not change. `internal/server/graph_structure_test.go` pins the order and the exact errors of all three, and was written against the old code first.

The node types a flow can hold, and the graph version each is valid in, are one table (`flowNodeMinVersion` in `internal/server/flow_node_catalog.go`, plus the investor data nodes); `validateFlow` asks `flowNodeAllowed`. `TestFlowNodeTypeGateByVersion` pins what each version accepts through the real validator and `TestFlowNodeTypesMatchEditor` checks that the editor's `FlowNode` type union lists exactly the same types.

What stays with the graph kind: node types' settings, ports and pins, limits, type checking of a typed function, and the semantics of a flow (`checkFlowSemantics`). A new kind of graph calls `graph.Joins` and `graph.Sort` and adds its own rules next to them.

## Contract tests

The saved graphs in `testdata/graphs/` (a flow, an untyped function, a typed function, triggers and the statuses of a run snapshot) are read by both languages, so neither side can lose or invent anything unnoticed:

| Check | Where | What fails |
| --- | --- | --- |
| The real validators accept every fixture | `internal/server/graph_contract_test.go` | A validator change that rejects a graph the editor saves |
| A fixture survives decode and encode in Go | same | A field the Go model drops or renames |
| Go and TypeScript models list the same JSON fields (functions, typed functions, triggers; flows are covered by `TestFlowGraphContractMatchesEditor`) | same | A field added on one side only. Intended differences are listed with their reason: `name` on an untyped function node (server only) and the editor coordinates of a typed function node (the server stores the graph as raw JSON and never re-encodes it) |
| The same fields hold the same kind of value (string, number, boolean, array) in Go and in the TypeScript interface, flows included | same (`TestGraphContractGoAndTypeScriptTypesAgree`) | A field that is a number on one side and text on the other. Types that are neither (a named type, an object, raw JSON) are not judged |
| The adapters (the Flow Editor's included) keep ids, coordinates, edges and ports, never write to their input, and every wire lands on a port the node has | `frontend/src/app/ui/flow-canvas/graph-contract.spec.ts` | An adapter that renames, moves or invents something |
| Every fixture is covered by the TypeScript spec | same | A new fixture nobody tests |

A new field of a stored graph goes into a fixture in the same change. The TypeScript interfaces are written by hand and checked against the Go structs by the tests above, rather than generated from them: the editor's interfaces carry unions of node types and named types that Go does not have, so a generated file would have to be edited or ignored.

## Visual regression

`frontend/e2e/flow-canvas-visual.spec.ts` describes every graph surface in the light and the dark theme: the Flow Editor (a new flow, a template with a node selected, a function call with typed pins), the untyped and the typed function canvas, the event triggers and the execution graph of a run. The description is, for each card, port and wire, where it is (canvas units), its classes, the colors the theme gives it, its text and its accessible name, and the toolbar buttons. It is compared to a snapshot in `frontend/e2e/__snapshots__/flow-canvas-visual.spec.ts/`.

It does not compare screenshots. A screenshot of a canvas differs between machines (fonts, anti-aliasing), so a pixel baseline made on one machine fails on another for no reason. The description is the same everywhere and a change fails with a readable diff (`- "stroke-width": "2.2px"`, `+ "3px"`). Screenshots are still attached to the report for people to look at (`flow-diagrams.spec.ts`, `visual-evidence.spec.ts`).

A deliberate change to the canvas is accepted by reading the diff and then running `npx playwright test e2e/flow-canvas-visual.spec.ts --update-snapshots` in `frontend/`. Changing a shared primitive changes every surface's snapshot at once, which is the point: the diff shows what each surface gained or lost.

## Which tests run in CI

CI names the Node specs and the Playwright specs it runs one by one in `.github/workflows/ci.yml`, so a spec that is not added there never runs. Two checks in the `policy` job keep that from happening quietly: `scripts/node-specs-in-ci.test.mjs` (every Node spec) and `scripts/e2e-specs-in-ci.test.mjs` (every Playwright spec). For the Playwright specs a short list, `scripts/e2e-not-in-ci.json`, holds those that no CI step runs yet (it is empty now: every spec runs). It is a debt, not a permission: a spec leaves the list when a CI step runs it, and a new spec is added to a step, not to the list.

The graph surfaces are covered by `flow-diagrams.spec.ts` (shared contract), `flow-canvas-editing.spec.ts` (how the Flow Editor is edited), `flow-canvas-visual.spec.ts` (how every surface looks) and the Node specs of the adapters and geometry, so a change to a shared primitive is judged against all of them.

## Not covered yet

Generating the TypeScript interfaces from the Go structs (see Contract tests for why they are checked instead), and a shared description of node *settings*: the types are shared now (see above), but each type's settings rules still live in `validateFlow`, and the editor has its own per-node settings forms. Both are tracked on #262.
