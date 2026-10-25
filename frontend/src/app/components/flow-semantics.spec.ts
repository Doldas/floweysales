// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/flow-semantics.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { semanticIssues, sourceType, type SemEdge, type SemNode, type SemPinsOf } from './flow-semantics.ts';

const noPins: SemPinsOf = () => ({ inputs: [], outputs: [] });
const nodes: SemNode[] = [
  { id: 't', type: 'trigger' }, { id: 'c', type: 'condition', label: 'Check' },
  { id: 'a', type: 'constant', value_type: 'text', label: 'A' }, { id: 'b', type: 'constant', value_type: 'text', label: 'B' },
  { id: 'out', type: 'output', label: 'Out' },
];
const branches = (aPort: string, bPort: string): SemEdge[] => [
  { from: 't', to: 'c' }, { from: 'c', to: 'a', from_port: aPort || undefined }, { from: 'c', to: 'b', from_port: bPort || undefined },
  { from: 'a', to: 'out' }, { from: 'b', to: 'out' },
];

test('exclusive IF/ELSE branches may join without a merge', () => {
  assert.deepEqual(semanticIssues(2, nodes, branches('true', 'false'), noPins), []);
});

test('parallel or same-branch inputs need a merge node', () => {
  assert.match(semanticIssues(2, nodes, branches('', ''), noPins)[0].message, /Merge node/);
  assert.match(semanticIssues(2, nodes, branches('true', 'true'), noPins)[0].message, /Out can receive input from A and B/);
  const merged = [...nodes.slice(0, 4), { id: 'm', type: 'merge' }, nodes[4]];
  const edges = [...branches('', '').slice(0, 3), { from: 'a', to: 'm' }, { from: 'b', to: 'm' }, { from: 'm', to: 'out' }];
  assert.deepEqual(semanticIssues(2, merged, edges, noPins), []);
});

test('duplicate connections are reported in every graph version', () => {
  const edges = [{ from: 't', to: 'a' }, { from: 't', to: 'a' }];
  assert.match(semanticIssues(1, nodes, edges, noPins)[0].message, /duplicated/);
});

test('version 1 graphs keep their sequential semantics', () => {
  assert.deepEqual(semanticIssues(1, nodes, branches('', ''), noPins), []);
});

const fnPins: SemPinsOf = n => n.type === 'function'
  ? { inputs: [{ name: 'score', type: 'number' }, { name: 'note', type: 'text' }], outputs: [{ name: 'ok', type: 'boolean' }] }
  : { inputs: [], outputs: [] };

test('typed pins check source types and branch consistency', () => {
  const pinNodes: SemNode[] = [
    { id: 't', type: 'trigger' }, { id: 'c', type: 'condition' },
    { id: 'n', type: 'constant', value_type: 'number' }, { id: 's', type: 'constant', value_type: 'text', label: 'Words' },
    { id: 'fn', type: 'function', label: 'Fn' },
  ];
  const base: SemEdge[] = [{ from: 't', to: 'c' }, { from: 'c', to: 'n', from_port: 'true' }, { from: 'c', to: 's', from_port: 'true' },
    { from: 'n', to: 'fn', to_port: 'in:score' }, { from: 's', to: 'fn', to_port: 'in:note' }];
  assert.deepEqual(semanticIssues(2, pinNodes, base, fnPins), []);
  const wrongType = base.map(e => e.to_port === 'in:score' ? { ...e, from: 's' } : e.to_port === 'in:note' ? { ...e, from: 'n' } : e);
  assert.ok(semanticIssues(2, pinNodes, wrongType, fnPins).some(i => /expects number but Words provides text/.test(i.message)));
  const split = base.map(e => e.to === 's' ? { ...e, from_port: 'false' } : e);
  assert.ok(semanticIssues(2, pinNodes, split, fnPins).some(i => /different IF\/ELSE branches/.test(i.message)));
  const twice = base.map(e => e.to_port === 'in:note' ? { ...e, to_port: 'in:score' } : e);
  assert.ok(semanticIssues(2, pinNodes, twice, fnPins).some(i => /more than one source/.test(i.message)));
});

test('source types are known for constants, conditions and output pins', () => {
  const fn = { id: 'fn', type: 'function' };
  assert.equal(sourceType({ from: 'fn', to: 'x', from_port: 'out:ok' }, fn, fnPins), 'boolean');
  assert.equal(sourceType({ from: 'c', to: 'x' }, { id: 'c', type: 'condition' }, fnPins), 'boolean');
  assert.equal(sourceType({ from: 'c', to: 'x', from_port: 'true' }, { id: 'c', type: 'condition' }, fnPins), '');
  assert.equal(sourceType({ from: 'g', to: 'x', from_port: 'field:score' }, { id: 'g', type: 'agent' }, fnPins), '');
});
