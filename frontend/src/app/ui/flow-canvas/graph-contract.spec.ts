// Runs with Node's built-in test runner:
//   node --test src/app/ui/flow-canvas/graph-contract.spec.ts
//
// The graph contract (#262). testdata/graphs holds saved graphs of every kind. The Go tests
// (internal/server/graph_contract_test.go) read the same files through the real validators; here
// the diagram adapters read them. Every node, edge, port and coordinate must come through, the
// adapters must not touch what they are given, and a wire must land on a port that exists.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
import { editorDiagram, flowNodeTag, functionDiagram, runSnapshotDiagram, triggerDiagram, typedFunctionDiagram } from './diagram-adapters.ts';
import { edgeBetween } from './flow-canvas-geometry.ts';
import { investorNode } from '#app/components/investor-nodes';
import { declaredPins, inputPorts, outputPorts, type V2Graph } from '#app/components/function-pins';

const dir = new URL('../../../../../testdata/graphs/', import.meta.url);
const fixture = <T>(name: string): T => JSON.parse(readFileSync(new URL(name, dir), 'utf8')) as T;
const ports = { inputPorts, outputPorts };

/** Freezes a value all the way down, so an adapter that writes to its input throws. */
function deepFreeze<T>(value: T): T {
  if (value && typeof value === 'object') {
    for (const v of Object.values(value)) deepFreeze(v);
    Object.freeze(value);
  }
  return value;
}

interface FlowNodeJson { id: string; type: string; label: string; x: number; y: number }
interface FlowJson { version: number; nodes: FlowNodeJson[]; edges: { from: string; to: string }[] }
interface V1Json { nodes: { id: string; type: 'input' | 'output' | 'constant' | 'operation'; operation?: string; x: number; y: number }[]; edges: { from: string; to: string; to_port?: string }[] }

test('every fixture is covered here, so a new one cannot be forgotten', () => {
  const covered = ['flow-research.json', 'function-untyped.json', 'function-typed.json', 'triggers.json', 'run-statuses.json'];
  const found = readdirSync(dir).filter(f => f.endsWith('.json')).sort();
  assert.deepEqual(found, [...covered].sort(), 'add a test for the new fixture, then list it here');
});

test('a run snapshot keeps every node and edge of the flow and tones each node by its recorded status', () => {
  const flow = deepFreeze(fixture<FlowJson>('flow-research.json'));
  const run = fixture<{ graph: string; statuses: Record<string, string> }>('run-statuses.json');
  assert.equal(run.graph, 'flow-research.json');
  const d = runSnapshotDiagram(flow.nodes, flow.edges, id => run.statuses[id] ?? '', type => flowNodeTag(type, t => investorNode(t)?.tag));
  assert.deepEqual(d.nodes.map(n => [n.id, n.x, n.y, n.label]), flow.nodes.map(n => [n.id, n.x, n.y, n.label]));
  assert.deepEqual(d.edges.map(e => [e.from, e.to]), flow.edges.map(e => [e.from, e.to]));
  const tone = Object.fromEntries(d.nodes.map(n => [n.id, n.tone]));
  assert.deepEqual(tone, { start: 'ok', analyst: 'fail', save: 'skip', report: 'none' });
  assert.equal(d.nodes.find(n => n.id === 'analyst')!.ariaLabel, 'Analyst FAILED', 'state is in text, not only color');
  for (const e of d.edges) assert.notEqual(edgeBetween(d.nodes, e), '', `edge ${e.from} -> ${e.to} can be drawn`);
});

test('the Flow Editor projection keeps every node and edge of a saved flow, and wires land on declared pins', () => {
  const flow = fixture<FlowJson>('flow-research.json');
  const fn = fixture<V2Graph>('function-typed.json');
  // The saved flow with a call to the typed function wired by pin, as the editor stores it.
  const nodes = deepFreeze([...flow.nodes, { id: 'risk', type: 'function', label: 'Risk', x: 300, y: 300 }]);
  const edges = deepFreeze([...flow.edges, { from: 'analyst', to: 'risk', to_port: 'in:score' }, { from: 'risk', to: 'report', from_port: 'out:passed' }]);
  const d = editorDiagram(nodes, edges, {
    tagOf: type => flowNodeTag(type),
    pinsOf: n => n.id === 'risk' ? declaredPins(fn) : { inputs: [], outputs: [] },
  });
  assert.deepEqual(d.nodes.map(n => [n.id, n.x, n.y, n.label]), nodes.map(n => [n.id, n.x, n.y, n.label]));
  assert.equal(d.edges.length, edges.length);
  assert.deepEqual(d.edges.map(e => [e.from, e.to]), edges.map(e => [e.from, e.to]));
  const byId = new Map(d.nodes.map(n => [n.id, n]));
  assert.deepEqual(byId.get('risk')!.inputs?.map(p => p.name), ['score', 'threshold']);
  assert.deepEqual(byId.get('risk')!.outputs?.map(p => p.name), ['result', 'passed']);
  for (const e of d.edges) {
    if (e.toPort) assert.ok(byId.get(e.to)!.inputs?.some(p => p.name === e.toPort), `${e.to} has the input ${e.toPort}`);
    if (e.fromPort) assert.ok(byId.get(e.from)!.outputs?.some(p => p.name === e.fromPort), `${e.from} has the output ${e.fromPort}`);
    assert.notEqual(edgeBetween(d.nodes, e), '');
  }
  // A trigger has no input to wire into and an output node no output to wire from.
  assert.equal(byId.get('start')!.inPort, undefined);
  assert.equal(byId.get('report')!.outPort, undefined);
  assert.equal(byId.get('analyst')!.inPort, 'Connect into Analyst');
});

test('an untyped function keeps its nodes, wires and ports, and an operation shows its input ports', () => {
  const fn = deepFreeze(fixture<V1Json>('function-untyped.json'));
  const d = functionDiagram(fn.nodes, fn.edges, ports);
  assert.deepEqual(d.nodes.map(n => [n.id, n.x, n.y]), fn.nodes.map(n => [n.id, n.x, n.y]));
  // Wires keep their stored ports. A wire without one goes to port a only where the node has named inputs; the stored graph is not changed.
  assert.deepEqual(d.edges.map(e => [e.from, e.to, e.toPort]), fn.edges.map(e => [e.from, e.to, e.to_port]));
  assert.deepEqual(d.nodes.find(n => n.id === 'double')!.inputs?.map(p => p.name), ['a', 'b']);
  // A wire to a named port lands on a port the node has.
  for (const e of d.edges.filter(e => e.toPort)) {
    assert.ok(d.nodes.find(n => n.id === e.to)!.inputs?.some(p => p.name === e.toPort), `${e.to} has the port ${e.toPort}`);
  }
  for (const e of d.edges) assert.notEqual(edgeBetween(d.nodes, e), '');
});

test('a typed function keeps its ids, coordinates and wire kinds, and every wire lands on a real port', () => {
  const fn = deepFreeze(fixture<V2Graph>('function-typed.json'));
  const d = typedFunctionDiagram(fn, ports);
  assert.deepEqual(d.nodes.map(n => [n.id, n.x, n.y]), fn.nodes.map(n => [n.id, n.x, n.y]));
  assert.equal(d.edges.length, fn.edges.length);
  assert.deepEqual(d.edges.map(e => [e.from, e.fromPort, e.to, e.toPort, e.kind]), fn.edges.map(e => [e.from, e.from_port, e.to, e.to_port, e.kind]));
  const byId = new Map(d.nodes.map(n => [n.id, n]));
  for (const e of d.edges) {
    assert.ok(byId.get(e.from)!.outputs?.some(p => p.name === e.fromPort), `${e.from} has the output ${e.fromPort}`);
    assert.ok(byId.get(e.to)!.inputs?.some(p => p.name === e.toPort), `${e.to} has the input ${e.toPort}`);
    assert.notEqual(edgeBetween(d.nodes, e), '');
  }
  // Execution wires are dashed, data wires solid: the kinds are told apart, not merged.
  assert.deepEqual([...new Set(d.edges.map(e => e.kind))].sort(), ['data', 'exec']);
  // The declared pins become the typed ports of the input and output nodes.
  assert.deepEqual(byId.get('start')!.outputs?.filter(p => p.name !== 'next').map(p => [p.name, p.type]), [['score', 'number'], ['threshold', 'number']]);
  assert.deepEqual(byId.get('finish')!.inputs?.filter(p => p.name !== 'in').map(p => [p.name, p.type]), [['result', 'text'], ['passed', 'boolean']]);
});

test('triggers become distinct event and payload nodes, one pair per trigger, with their payload type', () => {
  const triggers = deepFreeze(fixture<{ id: number; event_key: string; payload_type: string }[]>('triggers.json'));
  const d = triggerDiagram(triggers);
  assert.equal(d.nodes.length, triggers.length * 2);
  assert.equal(d.edges.length, triggers.length);
  assert.equal(new Set(d.nodes.map(n => n.id)).size, d.nodes.length, 'node ids are unique');
  triggers.forEach((t, i) => {
    const event = d.nodes.find(n => n.id === `event:${t.id}`)!;
    assert.equal(event.label, t.event_key);
    assert.deepEqual(event.outputs, [{ name: 'payload', type: t.payload_type }]);
    assert.deepEqual(d.edges[i], { from: `event:${t.id}`, to: `payload:${t.id}`, kind: 'event' });
  });
});

test('fixtures are plain JSON that survives a round trip unchanged', () => {
  for (const name of ['flow-research.json', 'function-untyped.json', 'function-typed.json', 'triggers.json', 'run-statuses.json']) {
    const parsed = fixture<unknown>(name);
    assert.deepEqual(JSON.parse(JSON.stringify(parsed)), parsed, name);
  }
});
