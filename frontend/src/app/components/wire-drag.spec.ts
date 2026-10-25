// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/wire-drag.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { edgeKey, nearestPort, portPoints, wireFromDrag, wirePath, type PortPoint, type WireNode } from './wire-drag.ts';

const nodes: WireNode[] = [
  { id: 'start', type: 'trigger', x: 0, y: 0 },
  { id: 'fn', type: 'function', x: 200, y: 0 },
  { id: 'out', type: 'output', x: 400, y: 0 },
];
const pinsOf = (n: WireNode) => n.id === 'fn'
  ? { inputs: [{ name: 'a', type: 'number' as const }, { name: 'b', type: 'text' as const }], outputs: [{ name: 'r', type: 'number' as const }] }
  : { inputs: [], outputs: [] };
const points = portPoints(nodes, pinsOf);
const find = (nodeId: string, side: 'in' | 'out', pin?: string) => points.find(p => p.nodeId === nodeId && p.side === side && p.pin === pin) as PortPoint;

test('triggers have no input port, outputs have no output port, pins replace the default port', () => {
  assert.equal(points.some(p => p.nodeId === 'start' && p.side === 'in'), false);
  assert.equal(points.some(p => p.nodeId === 'out' && p.side === 'out'), false);
  assert.equal(points.filter(p => p.nodeId === 'fn' && p.side === 'in').length, 2);
  assert.equal(points.some(p => p.nodeId === 'fn' && p.side === 'in' && !p.pin), false);
});

test('nearestPort picks the closest port of the requested side within the radius', () => {
  const a = find('fn', 'in', 'a');
  assert.equal(nearestPort(points, a.x + 2, a.y + 3, 'in')?.pin, 'a');
  assert.equal(nearestPort(points, a.x, a.y, 'out'), null);
  assert.equal(nearestPort(points, 300, 300, 'in'), null);
});

test('plain wires connect once and refuse self loops', () => {
  const r = wireFromDrag([], find('start', 'out'), find('out', 'in'));
  assert.ok('edges' in r && r.edges.length === 1 && !r.edge.from_port && !r.edge.to_port);
  assert.ok('problem' in wireFromDrag((r as any).edges, find('start', 'out'), find('out', 'in')));
  assert.match((wireFromDrag([], find('fn', 'out', 'r'), find('fn', 'in', 'a')) as any).problem, /itself/);
});

test('pin wires are typed and each input pin takes one source', () => {
  const ok = wireFromDrag([], find('start', 'out'), find('fn', 'in', 'a'));
  assert.ok('edge' in ok && ok.edge.to_port === 'in:a');
  const duplicate = wireFromDrag((ok as any).edges, find('start', 'out'), find('fn', 'in', 'a'));
  assert.ok('problem' in duplicate);
  const bad = wireFromDrag([], find('fn', 'out', 'r'), find('fn', 'in', 'b'));
  assert.ok('problem' in bad);
  const other: PortPoint = { nodeId: 'src', side: 'out', x: 0, y: 0 };
  const replaced = wireFromDrag((ok as any).edges, other, find('fn', 'in', 'a')) as any;
  assert.equal(replaced.edges.filter((e: any) => e.to === 'fn' && e.to_port === 'in:a').length, 1);
  assert.equal(replaced.edges[0].from, 'src');
});

test('an untyped input into a pinned node is replaced by the pin wire and vice versa', () => {
  const plain = [{ from: 'start', to: 'fn' }];
  const withPin = wireFromDrag(plain, { nodeId: 'src', side: 'out', x: 0, y: 0 }, find('fn', 'in', 'a')) as any;
  assert.deepEqual(withPin.edges.map((e: any) => e.to_port), ['in:a']);
  const back = wireFromDrag(withPin.edges, { nodeId: 'src2', side: 'out', x: 0, y: 0 }, { nodeId: 'fn', side: 'in', x: 0, y: 0 }) as any;
  assert.deepEqual(back.edges.map((e: any) => e.to_port ?? ''), ['']);
});

test('edge keys distinguish several pin wires between the same nodes', () => {
  const a = { from: 'x', to: 'fn', from_port: 'out:p', to_port: 'in:a' };
  const b = { from: 'x', to: 'fn', from_port: 'out:q', to_port: 'in:b' };
  assert.notEqual(edgeKey(a), edgeKey(b));
  assert.match(wirePath(0, 0, 100, 50), /^M 0 0 C /);
});
