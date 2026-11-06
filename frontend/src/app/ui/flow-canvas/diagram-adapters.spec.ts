// Runs with Node's built-in test runner:
//   node --test src/app/ui/flow-canvas/diagram-adapters.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { flowNodeTag, functionDiagram, runSnapshotDiagram, triggerDiagram, typedFunctionDiagram } from './diagram-adapters.ts';
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
