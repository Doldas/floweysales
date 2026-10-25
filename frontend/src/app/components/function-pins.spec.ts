// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/function-pins.spec.ts
// No extra test dependencies are required.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  bindInputPin, connectWire, declaredPins, inputPorts, isTypedFunctionGraph, newTypedFunctionGraph, outputPorts,
  mixesPinAndPlainInputs, pinBindings, pinEdgeProblem, pinOffset, pinProblems, removePin, renamePin, routeOutputPin, unboundInputPins,
} from './function-pins.ts';

test('a new typed function is a complete version 2 graph', () => {
  const g = newTypedFunctionGraph();
  assert.equal(isTypedFunctionGraph(g), true);
  assert.equal(isTypedFunctionGraph({ nodes: [], edges: [] }), false);
  assert.deepEqual(g.edges.map(e => e.kind).sort(), ['data', 'exec']);
});

test('pin lists are bounded, named and unique', () => {
  assert.deepEqual(pinProblems([{ name: 'score', type: 'number' }], 'input'), []);
  assert.equal(pinProblems([], 'input').length, 1);
  assert.match(pinProblems([{ name: '1bad', type: 'text' }], 'output')[0], /must start with a letter/);
  assert.match(pinProblems([{ name: 'a', type: 'text' }, { name: 'a', type: 'number' }], 'input')[0], /declared twice/);
});

test('ports follow node type and declared pins', () => {
  const g = newTypedFunctionGraph();
  g.inputs.push({ name: 'limit', type: 'number' });
  const input = g.nodes[0];
  assert.deepEqual(outputPorts(input, g).map(p => p.port + ':' + p.kind), ['value:data', 'limit:data', 'next:exec']);
  assert.deepEqual(inputPorts({ id: 'op', type: 'operation', operation: 'if', x: 0, y: 0 }, g).map(p => p.port), ['condition', 'true', 'false', 'in']);
  assert.deepEqual(inputPorts({ id: 'op', type: 'operation', operation: 'not', x: 0, y: 0 }, g).map(p => p.port), ['a', 'in']);
  assert.deepEqual(outputPorts({ id: 'b', type: 'branch', x: 0, y: 0 }, g).map(p => p.port), ['true', 'false']);
});

test('wires replace an existing source and refuse mismatched kinds', () => {
  const g = newTypedFunctionGraph();
  g.nodes.push({ id: 'two', type: 'constant', value_type: 'number', value: '2', x: 0, y: 0 });
  let edges = connectWire(g.edges, { kind: 'data', from: 'two', from_port: 'value', to: 'finish', to_port: 'result' }, g);
  assert.equal(edges.filter(e => e.to === 'finish' && e.to_port === 'result').length, 1);
  assert.equal(edges.find(e => e.to_port === 'result')?.from, 'two');
  const unchanged = connectWire(edges, { kind: 'exec', from: 'two', from_port: 'value', to: 'finish', to_port: 'in' }, g);
  assert.deepEqual(unchanged, edges);
  edges = connectWire(edges, { kind: 'data', from: 'finish', from_port: 'x', to: 'finish', to_port: 'result' }, g);
  assert.equal(edges.length, 2);
});

test('renaming and removing pins keeps wires consistent', () => {
  const g = newTypedFunctionGraph();
  const renamed = renamePin(g, 'input', 'value', 'price');
  assert.equal(renamed.inputs[0].name, 'price');
  assert.ok(renamed.edges.some(e => e.from === 'start' && e.from_port === 'price'));
  const out = renamePin(g, 'output', 'result', 'total');
  assert.ok(out.edges.some(e => e.to === 'finish' && e.to_port === 'total'));
  const removed = removePin(g, 'output', 'result');
  assert.deepEqual(removed.outputs, []);
  assert.ok(!removed.edges.some(e => e.kind === 'data'));
});

test('flow pin bindings replace earlier and untyped inputs', () => {
  let edges = [{ from: 'x', to: 'fn' }, { from: 'y', to: 'other' }];
  edges = bindInputPin(edges, 'fn', 'score', 'a');
  edges = bindInputPin(edges, 'fn', 'limit', 'b');
  edges = bindInputPin(edges, 'fn', 'score', 'c');
  assert.deepEqual(pinBindings(edges, 'fn'), { limit: 'b', score: 'c' });
  assert.ok(!edges.some(e => e.from === 'x'), 'untyped input into the node is replaced');
  edges = bindInputPin(edges, 'fn', 'limit', '');
  assert.deepEqual(pinBindings(edges, 'fn'), { score: 'c' });
  edges = routeOutputPin(edges, 'fn', 'passed', 'flag');
  assert.ok(edges.some(e => e.from === 'fn' && e.from_port === 'out:passed' && e.to === 'flag'));
  assert.deepEqual(declaredPins({ nodes: [] }), { inputs: [], outputs: [] });
});

test('pins are spread evenly along the node edge', () => {
  assert.deepEqual([0, 1].map(i => pinOffset(i, 2, 90)), [30, 60]);
  assert.equal(pinOffset(0, 1, 70), 35);
});

test('preflight accepts declared pins and explains the rest', () => {
  const pins = [{ name: 'score', type: 'number' as const }, { name: 'limit', type: 'number' as const }];
  assert.equal(pinEdgeProblem({ from: 'a', to: 'fn', to_port: 'in:score' }, 'constant', 'function', [], pins), null);
  assert.equal(pinEdgeProblem({ from: 'fn', from_port: 'out:score', to: 'o' }, 'function', 'output', pins, []), null);
  assert.match(pinEdgeProblem({ from: 'a', to: 'fn', to_port: 'in:nope' }, 'constant', 'function', [], pins)!, /no input pin "nope"/);
  assert.match(pinEdgeProblem({ from: 'a', to: 'c', to_port: 'in:x' }, 'constant', 'constant', [], [])!, /only on function nodes/);
  assert.match(pinEdgeProblem({ from: 'a', to: 'fn', to_port: 'in:x' }, 'constant', 'function', [], [])!, /no declared input pins/);
  assert.match(pinEdgeProblem({ from: 'c', from_port: 'out:x', to: 'o' }, 'constant', 'output', [], [])!, /only on function nodes/);
  assert.equal(pinEdgeProblem({ from: 'a', to: 'b' }, 'agent', 'agent', [], []), null);
});

test('unbound pins and mixed inputs are detected', () => {
  const pins = [{ name: 'score', type: 'number' as const }, { name: 'limit', type: 'number' as const }];
  const edges = [{ from: 'a', to: 'fn', to_port: 'in:score' }];
  assert.deepEqual(unboundInputPins(edges, 'fn', pins).map(p => p.name), ['limit']);
  assert.deepEqual(unboundInputPins(bindInputPin(edges, 'fn', 'limit', 'b'), 'fn', pins), []);
  assert.equal(mixesPinAndPlainInputs(edges, 'fn'), false);
  assert.equal(mixesPinAndPlainInputs([...edges, { from: 'x', to: 'fn' }], 'fn'), true);
});
