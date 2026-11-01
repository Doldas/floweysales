// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/investor-nodes.spec.ts
// Regenerate the server fixture after changing a playbook:
//   UPDATE_PLAYBOOKS=1 node --test src/app/components/investor-nodes.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { INVESTOR_NODES, INVESTOR_PLAYBOOKS, LEADERBOARD_STRATEGIES, LEADERBOARD_UNIVERSES, buildPlaybook, investorNode, isInvestorNode, watchlistProblem } from './investor-nodes.ts';

const fixture = fileURLToPath(new URL('../../../../internal/server/testdata/investor-playbooks.json', import.meta.url));
const optionsFixture = fileURLToPath(new URL('../../../../internal/server/testdata/leaderboard-options.json', import.meta.url));

function allPlaybooks() {
  const out: Record<string, unknown> = {};
  for (const p of INVESTOR_PLAYBOOKS) for (const admin of [true, false]) out[`${p.id}/${admin ? 'admin' : 'user'}`] = buildPlaybook(p.id, { admin });
  return out;
}

test('every playbook is a connected, bounded v2 graph with one trigger', () => {
  for (const [key, built] of Object.entries(allPlaybooks())) {
    const p = built as NonNullable<ReturnType<typeof buildPlaybook>>;
    assert.ok(p, key);
    const ids = new Set(p.graph.nodes.map(n => n.id));
    assert.equal(ids.size, p.graph.nodes.length, `${key}: duplicate node ids`);
    assert.equal(p.graph.nodes.filter(n => n.type === 'trigger').length, 1, key);
    assert.ok(p.graph.nodes.length <= 16 && p.graph.edges.length <= 32, key);
    assert.ok(JSON.stringify(p.graph).length < 16000, `${key}: graph too large`);
    for (const n of p.graph.nodes) if (n.type !== 'trigger') assert.ok(p.graph.edges.some(e => e.to === n.id), `${key}: ${n.id} has no input`);
    for (const e of p.graph.edges) assert.ok(ids.has(e.from) && ids.has(e.to), `${key}: dangling edge`);
  }
});

test('only administrators get notify nodes and research tools', () => {
  for (const p of INVESTOR_PLAYBOOKS) {
    const user = buildPlaybook(p.id, { admin: false })!;
    assert.ok(!user.graph.nodes.some(n => n.type === 'notify' || (n as { tools?: string[] }).tools), p.id);
    assert.ok(user.graph.nodes.some(n => n.type === 'output'), p.id);
    assert.ok(buildPlaybook(p.id, { admin: true })!.graph.nodes.some(n => n.type === 'notify'), p.id);
  }
  assert.equal(buildPlaybook('nope', { admin: true }), null);
});

test('a custom watchlist reaches the data nodes', () => {
  const p = buildPlaybook('price-alerts', { admin: false, symbols: 'VOLV-B.ST, SAAB-B.ST' })!;
  assert.equal(p.graph.nodes.find(n => n.type === 'watchlist_quotes')!['symbols'], 'VOLV-B.ST, SAAB-B.ST');
});

test('a custom watchlist reaches watchlist leaderboards', () => {
  for (const id of ['watchlist-health', 'earnings-scored']) {
    const p = buildPlaybook(id, { admin: true, symbols: 'VOLV-B.ST, SAAB-B.ST' })!;
    assert.equal(p.graph.nodes.find(n => n.type === 'leaderboard')!['symbols'], 'VOLV-B.ST, SAAB-B.ST', id);
  }
});

test('the server knows every leaderboard universe and strategy', () => {
  const current = JSON.stringify({ universes: LEADERBOARD_UNIVERSES.map(u => u.id), strategies: LEADERBOARD_STRATEGIES.map(s => s.id) }, null, 1) + '\n';
  if (process.env['UPDATE_PLAYBOOKS']) writeFileSync(optionsFixture, current);
  assert.equal(readFileSync(optionsFixture, 'utf8'), current, 'run with UPDATE_PLAYBOOKS=1 to regenerate internal/server/testdata/leaderboard-options.json');
});

test('node catalogue and watchlist checks', () => {
  assert.ok(isInvestorNode('screen') && !isInvestorNode('agent'));
  assert.equal(investorNode('notify')!.admin, true);
  assert.ok(INVESTOR_NODES.filter(n => n.admin).every(n => n.type === 'notify'));
  assert.equal(watchlistProblem('AAPL, msft BRK.B'), '');
  assert.match(watchlistProblem('AAPL, $$$'), /not a valid ticker/);
  assert.match(watchlistProblem(Array.from({ length: 26 }, (_, i) => 'S' + i).join(',')), /At most 25/);
});

test('the server fixture matches the playbooks', () => {
  const current = JSON.stringify(allPlaybooks(), null, 1) + '\n';
  if (process.env['UPDATE_PLAYBOOKS']) writeFileSync(fixture, current);
  assert.equal(readFileSync(fixture, 'utf8'), current, 'run with UPDATE_PLAYBOOKS=1 to regenerate internal/server/testdata/investor-playbooks.json');
});
