import assert from 'node:assert/strict';
import test from 'node:test';
import {
  classifyScenarios, defaultMaxWorkflows, fixtureProblems, maxWorkflowCeiling, maxWorkflows, optedIn, pickFixtures, redact, stepProblems,
  type FlowDef,
} from './workflow-scenarios.ts';

const chain: FlowDef = {
  id: 1, name: '[E2E] chain', enabled: false, interval_minutes: 0, event_key: '',
  graph: {
    nodes: [{ id: 'a', type: 'trigger' }, { id: 'b', type: 'transform' }, { id: 'c', type: 'output' }],
    edges: [{ from: 'a', to: 'b' }, { from: 'b', to: 'c' }],
  },
};

test('opt-in gates only open for explicit true/1', () => {
  for (const v of [undefined, '', 'false', 'yes', '0', 'TRUEISH']) assert.equal(optedIn(v), false);
  for (const v of ['true', 'TRUE', ' 1 ']) assert.equal(optedIn(v), true);
});

test('fixture count is bounded', () => {
  assert.equal(maxWorkflows(undefined), defaultMaxWorkflows);
  assert.equal(maxWorkflows('abc'), defaultMaxWorkflows);
  assert.equal(maxWorkflows('0'), defaultMaxWorkflows);
  assert.equal(maxWorkflows('3'), 3);
  assert.equal(maxWorkflows('999'), maxWorkflowCeiling);
});

test('a safe fixture has no problems', () => {
  assert.deepEqual(fixtureProblems(chain), []);
});

test('unsafe fixtures are rejected with reasons', () => {
  const bad: FlowDef = {
    ...chain, name: 'Prod flow', enabled: true, interval_minutes: 60, event_key: 'x',
    graph: { nodes: [{ id: 'a', type: 'trigger' }, { id: 'b', type: 'agent', tools: ['get_quote'] }, { id: 'c', type: 'email' }] },
  };
  const joined = fixtureProblems(bad).join('|');
  for (const part of ['name must start', 'disabled', 'no schedule', 'no event', 'email', 'tool grants']) assert.match(joined, new RegExp(part));
  assert.ok(fixtureProblems({ ...chain, graph: { nodes: [{ type: 'trigger' }] } }).some(p => /two nodes/.test(p)));
});

test('only [E2E] flows are picked, sorted', () => {
  const picked = pickFixtures([{ ...chain, id: 3, name: '[E2E] b' }, { ...chain, id: 2, name: 'user flow' }, { ...chain, id: 1, name: '[E2E] a' }]);
  assert.deepEqual(picked.map(f => f.id), [1, 3]);
});

test('scenario classification covers the angles', () => {
  assert.deepEqual(classifyScenarios(chain.graph), ['deterministic', 'chain']);
  const branch = { nodes: [{ id: 'a', type: 'trigger' }, { id: 'b', type: 'condition' }, { id: 'c', type: 'output' }, { id: 'd', type: 'merge' }], edges: [] };
  assert.deepEqual(classifyScenarios(branch), ['branch', 'merge', 'deterministic']);
  const ai = { nodes: [{ id: 'a', type: 'trigger' }, { id: 'b', type: 'agent' }, { id: 'c', type: 'output' }], edges: [] };
  assert.deepEqual(classifyScenarios(ai), ['agent-output', 'chain']);
  assert.deepEqual(classifyScenarios(undefined), ['deterministic', 'chain']);
});

test('step invariants accept a complete trace', () => {
  const steps = [{ node: 'a', status: 'SUCCESS' }, { node: 'b', status: 'SUCCESS' }, { node: 'c', status: 'SUCCESS' }];
  assert.deepEqual(stepProblems(chain.graph, steps), []);
});

test('step invariants do not expect a step for the trigger node', () => {
  assert.deepEqual(stepProblems(chain.graph, [{ node: 'b', status: 'SUCCESS' }, { node: 'c', status: 'SUCCESS' }]), []);
  assert.match(stepProblems(chain.graph, [{ node: 'b', status: 'SUCCESS' }]).join('|'), /1 of 2 nodes/);
});

test('step invariants flag gaps and failures', () => {
  assert.deepEqual(stepProblems(chain.graph, []), ['run recorded no node steps']);
  const partial = stepProblems(chain.graph, [{ node: 'a', status: 'SUCCESS' }, { node: 'b', status: 'FAILED' }]).join('|');
  assert.match(partial, /failed/);
  assert.match(partial, /no output node executed/);
  assert.match(partial, /1 of 2/);
  assert.match(stepProblems(chain.graph, [{ node: 'zzz', status: 'SUCCESS' }]).join('|'), /not in the graph/);
});

test('branching graphs may leave a side unexecuted', () => {
  const g = { nodes: [{ id: 'a', type: 'trigger' }, { id: 'b', type: 'condition' }, { id: 'x', type: 'output' }, { id: 'y', type: 'output' }], edges: [] };
  assert.deepEqual(stepProblems(g, [{ node: 'a', status: 'SUCCESS' }, { node: 'b', status: 'SUCCESS' }, { node: 'x', status: 'SUCCESS' }]), []);
});

test('redaction removes secrets and identities from log text', () => {
  const out = redact('user a.b@example.com Bearer abc.def password=hunter2 api_key: "k123" sk-abcdefghijk ' + 'a'.repeat(40));
  assert.doesNotMatch(out, /example\.com|abc\.def|hunter2|k123|sk-abc|aaaaaaaa/);
  assert.match(out, /\[email\]/);
  assert.equal(redact('plain text'), 'plain text');
});
