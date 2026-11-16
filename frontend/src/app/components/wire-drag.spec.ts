// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/wire-drag.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { wireFromDrag, type PortPoint } from './wire-drag.ts';

// The ports of a graph Start -> function (inputs a: number, b: text; output r: number) -> Out.
const port = (nodeId: string, side: 'in' | 'out', pin?: string, type?: string): PortPoint => ({ nodeId, side, pin, type, x: 0, y: 0 });
const start = port('start', 'out');
const out = port('out', 'in');
const fnIn = (pin: 'a' | 'b') => port('fn', 'in', pin, pin === 'a' ? 'number' : 'text');
const fnOut = port('fn', 'out', 'r', 'number');

test('plain wires connect once and refuse self loops', () => {
  const r = wireFromDrag([], start, out);
  assert.ok('edges' in r && r.edges.length === 1 && !r.edge.from_port && !r.edge.to_port);
  assert.ok('problem' in wireFromDrag((r as any).edges, start, out));
  assert.match((wireFromDrag([], fnOut, fnIn('a')) as any).problem, /itself/);
});

test('a wire runs from an output to an input and nothing else', () => {
  assert.match((wireFromDrag([], out, start) as any).problem, /output port to an input port/);
  assert.match((wireFromDrag([], start, port('x', 'out')) as any).problem, /output port to an input port/);
});

test('pin wires are typed and each input pin takes one source', () => {
  const ok = wireFromDrag([], start, fnIn('a'));
  assert.ok('edge' in ok && ok.edge.to_port === 'in:a');
  const duplicate = wireFromDrag((ok as any).edges, start, fnIn('a'));
  assert.ok('problem' in duplicate);
  const bad = wireFromDrag([], port('src', 'out', 'r', 'number'), fnIn('b'));
  assert.match((bad as any).problem, /Cannot connect number output "r" to text input "b"/);
  const replaced = wireFromDrag((ok as any).edges, port('src', 'out'), fnIn('a')) as any;
  assert.equal(replaced.edges.filter((e: any) => e.to === 'fn' && e.to_port === 'in:a').length, 1);
  assert.equal(replaced.edges[0].from, 'src');
});

test('an output pin names its pin in the stored port', () => {
  const r = wireFromDrag([], fnOut, out) as any;
  assert.deepEqual(r.edge, { from: 'fn', to: 'out', from_port: 'out:r' });
});

test('an untyped input into a pinned node is replaced by the pin wire and vice versa', () => {
  const plain = [{ from: 'start', to: 'fn' }];
  const withPin = wireFromDrag(plain, port('src', 'out'), fnIn('a')) as any;
  assert.deepEqual(withPin.edges.map((e: any) => e.to_port), ['in:a']);
  const back = wireFromDrag(withPin.edges, port('src2', 'out'), port('fn', 'in')) as any;
  assert.deepEqual(back.edges.map((e: any) => e.to_port ?? ''), ['']);
});

test('several pin wires may join the same two nodes', () => {
  const first = wireFromDrag([], port('x', 'out', 'p'), fnIn('a')) as any;
  const second = wireFromDrag(first.edges, port('x', 'out', 'q'), fnIn('b')) as any;
  assert.deepEqual(second.edges.map((e: any) => [e.from_port, e.to_port]), [['out:p', 'in:a'], ['out:q', 'in:b']]);
});
