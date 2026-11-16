// Runs with Node's built-in test runner:
//   node --test src/app/ui/flow-canvas/diagram-adapters.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { editorDiagram, flowNodeTag, functionDiagram, runSnapshotDiagram, triggerDiagram, typedFunctionDiagram } from './diagram-adapters.ts';
import { inputPorts, newTypedFunctionGraph, outputPorts } from '#app/components/function-pins';
import { investorNode } from '#app/components/investor-nodes';
import { edgeBetween } from './flow-canvas-geometry.ts';

const ports = { inputPorts, outputPorts };

test('typed function projection keeps ids, coordinates and typed ports; graph is untouched', () => {
  const graph = newTypedFunctionGraph();
  const before = JSON.stringify(graph);
  const d = typedFunctionDiagram(graph, ports);
  assert.equal(JSON.stringify(graph), before);
  assert.deepEqual(d.nodes.map(n => [n.id, n.x, n.y]), graph.nodes.map(n => [n.id, n.x, n.y]));
  const start = d.nodes.find(n => n.id === 'start')!;
  assert.deepEqual(start.outputs, [{ name: 'value', type: 'number' }, { name: 'next', type: 'exec' }]);
  assert.deepEqual(d.edges.map(e => e.kind), ['exec', 'data']);
  for (const e of d.edges) assert.notEqual(edgeBetween(d.nodes, e), '');
});

test('untyped function projection defaults operation wires to port a', () => {
  const d = functionDiagram(
    [{ id: 'input', type: 'input', x: 0, y: 0 }, { id: 'op', type: 'operation', operation: 'add', x: 200, y: 0 }, { id: 'output', type: 'output', x: 400, y: 0 }],
    [{ from: 'input', to: 'op' }, { from: 'op', to: 'output' }], ports);
  assert.deepEqual(d.nodes[1].inputs, [{ name: 'a' }, { name: 'b' }]);
  assert.equal(d.edges[0].toPort, 'a');
  assert.equal(d.nodes[1].tag, 'ƒ OPERATION');
});

test('a branch-like operation exposes condition/true/false ports', () => {
  const d = functionDiagram([{ id: 'x', type: 'operation', operation: 'if', x: 0, y: 0 }], [], ports);
  assert.deepEqual(d.nodes[0].inputs?.map(p => p.name), ['condition', 'true', 'false']);
});

test('run snapshot tones nodes by step status and states it in text', () => {
  const d = runSnapshotDiagram(
    [{ id: 'a', type: 'trigger', label: 'Start', x: 0, y: 0 }, { id: 'b', type: 'agent', label: 'Analyst', x: 200, y: 0 }],
    [{ from: 'a', to: 'b' }], id => id === 'a' ? 'SUCCESS' : 'FAILED', type => flowNodeTag(type, t => investorNode(t)?.tag));
  assert.deepEqual(d.nodes.map(n => n.tone), ['ok', 'fail']);
  assert.equal(d.nodes[1].ariaLabel, 'Analyst FAILED');
  assert.equal(d.nodes[0].tag, '⏱ TRIGGER');
});

test('trigger projection draws one event node feeding one typed payload node per trigger', () => {
  const d = triggerDiagram([{ id: 1, event_key: 'filing.published', payload_type: 'text' }, { id: 2, event_key: 'x', payload_type: 'number' }]);
  assert.equal(d.nodes.length, 4);
  assert.equal(d.edges.length, 2);
  assert.equal(d.edges[0].kind, 'event');
  assert.deepEqual(d.nodes[0].outputs, [{ name: 'payload', type: 'text' }]);
  assert.deepEqual(triggerDiagram([]), { nodes: [], edges: [] });
});

test('unknown node types fall back to the agent caption', () => {
  assert.equal(flowNodeTag('something_new'), '✦ AGENT');
  assert.equal(flowNodeTag('output'), '→ OUTPUT');
});

test('trigger diagrams use stable unique ids even when event labels repeat', () => {
  const d = triggerDiagram([{ id: 10, event_key: 'same.key', payload_type: 'text' }, { id: 11, event_key: 'same.key', payload_type: 'number' }]);
  assert.equal(new Set(d.nodes.map(n => n.id)).size, 4);
  assert.deepEqual(d.edges.map(e => [e.from, e.to]), [['event:10', 'payload:10'], ['event:11', 'payload:11']]);
});

const pinned = { inputs: [{ name: 'score', type: 'number' }, { name: 'threshold', type: 'number' }], outputs: [{ name: 'result', type: 'text' }, { name: 'passed', type: 'boolean' }] };
const editorNodes = [
  { id: 'start', type: 'trigger', label: 'Start', x: 65, y: 150 },
  { id: 'fn', type: 'function', label: 'Risk', x: 300, y: 40 },
  { id: 'agent', type: 'agent', label: 'Research', x: 540, y: 150 },
  { id: 'out', type: 'output', label: 'Report', x: 780, y: 150 },
];
const editorOptions = { tagOf: (t: string) => flowNodeTag(t), pinsOf: (n: { id: string }) => n.id === 'fn' ? pinned : { inputs: [], outputs: [] } };

test('the editor projection names nodes "Select <label>" and gives ports only where a node can be wired', () => {
  const frozen = Object.freeze(editorNodes.map(n => Object.freeze({ ...n })));
  const d = editorDiagram(frozen, [], editorOptions);
  assert.deepEqual(d.nodes.map(n => [n.id, n.x, n.y, n.accessibleName]), editorNodes.map(n => [n.id, n.x, n.y, `Select ${n.label}`]));
  const by = Object.fromEntries(d.nodes.map(n => [n.id, n]));
  assert.equal(by.start.inPort, undefined, 'a trigger has no input port');
  assert.equal(by.start.outPort, 'Start connection from Start');
  assert.equal(by.out.outPort, undefined, 'an output has no output port');
  assert.equal(by.out.inPort, 'Connect into Report');
  assert.deepEqual([by.agent.inPort, by.agent.outPort], ['Connect into Research', 'Start connection from Research']);
  // A function call shows its pins instead of the plain ports.
  assert.deepEqual(by.fn.inputs, pinned.inputs);
  assert.deepEqual(by.fn.outputs, pinned.outputs);
  assert.equal(by.fn.inPort, undefined);
  assert.equal(by.fn.outPort, undefined);
  assert.equal(by.agent.tone, undefined, 'no run is followed: no state');
});

test('stored pin ports become pin names, and other ports end in the middle of the card', () => {
  const d = editorDiagram(editorNodes, [
    { from: 'start', to: 'fn', to_port: 'in:score' },
    { from: 'fn', to: 'agent', from_port: 'out:passed' },
    { from: 'agent', to: 'out' },
    { from: 'start', to: 'agent', to_port: 'input', from_port: 'field:price' },
  ], editorOptions);
  assert.deepEqual(d.edges.map(e => [e.from, e.fromPort, e.to, e.toPort]), [
    ['start', undefined, 'fn', 'score'], ['fn', 'passed', 'agent', undefined], ['agent', undefined, 'out', undefined], ['start', undefined, 'agent', undefined],
  ]);
  // The wire to a pin ends at that pin; the others at the middle of the card.
  const toScore = edgeBetween(d.nodes, d.edges[0]);
  const toMiddle = edgeBetween(d.nodes, { ...d.edges[0], toPort: undefined });
  assert.notEqual(toScore, toMiddle);
});

test('a followed run states each node by tone and text in the badge, and the badge is not repeated', () => {
  const live = { tone: (id: string) => id === 'start' ? 'ok' as const : id === 'agent' ? 'fail' as const : 'none' as const, badge: (id: string) => id === 'start' ? '✓ 2 ms' : id === 'agent' ? '✕ failed' : '' };
  const d = editorDiagram(editorNodes, [], { ...editorOptions, live });
  const by = Object.fromEntries(d.nodes.map(n => [n.id, n]));
  assert.deepEqual([by.start.tone, by.start.sub, by.start.subClass, by.start.plainSub], ['ok', '✓ 2 ms', 'live-badge', true]);
  assert.deepEqual([by.agent.tone, by.agent.sub], ['fail', '✕ failed']);
  assert.equal(by.out.tone, 'none');
});
