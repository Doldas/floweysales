// Runs with Node's built-in test runner:
//   node --test src/app/components/provider-routing-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formFrom, groupDomains, moveFallback, policyBody, policyLine, policyProblems, sourceState, sourcesFor, stateLabel, toggleFallback } from './provider-routing-view.ts';

const src = (id: string, extra = {}) => ({ id, label: id.toUpperCase(), kind: 'plugin', domains: ['market_quote'], scope: 'system', needs_key: false, configured: true, enabled: true, ...extra }) as never;
const domain = (extra = {}) => ({ id: 'market_quote', label: 'Quotes', group: 'Market', no_fallback: false, sources: 2, routed: true, ...extra }) as never;

test('a source is ready, needs credentials or is disabled', () => {
  assert.equal(sourceState(src('a')), 'ready');
  assert.equal(sourceState(src('a', { needs_key: true, configured: false })), 'needs_key');
  assert.equal(stateLabel(src('a', { needs_key: true, configured: false })), 'Needs credentials');
  assert.equal(sourceState(src('a', { enabled: false, needs_key: true, configured: false })), 'disabled');
});

test('sources managed elsewhere are listed but not routable', () => {
  const all = [src('yahoo'), src('web_search:brave', { domains: ['web_search'], managed_by: 'Settings → Web search' }), src('mine', { scope: 'user' })];
  const q = sourcesFor('market_quote', all);
  assert.deepEqual(q.routable.map((s: { id: string }) => s.id), ['yahoo']);
  const w = sourcesFor('web_search', all);
  assert.deepEqual(w.routable, []);
  assert.deepEqual(w.managed.map((s: { id: string }) => s.id), ['web_search:brave']);
});

test('domains are grouped in server order', () => {
  const g = groupDomains([domain(), domain({ id: 'market_history' }), domain({ id: 'news', group: 'News' })]);
  assert.deepEqual(g.map(x => [x.group, x.domains.length]), [['Market', 2], ['News', 1]]);
});

test('a form needs a primary and sensible fallbacks', () => {
  const routable = [src('a'), src('b'), src('c')];
  assert.deepEqual(policyProblems({ primary: '', fallback: [], noFallback: false }, routable), ['Choose a primary source.']);
  assert.match(policyProblems({ primary: 'a', fallback: ['a'], noFallback: false }, routable).join(), /cannot also be a fallback/);
  assert.match(policyProblems({ primary: 'a', fallback: ['zzz'], noFallback: false }, routable).join(), /not available/);
  assert.deepEqual(policyProblems({ primary: 'a', fallback: ['b', 'c'], noFallback: false }, routable), []);
  // With fallback off the list is not checked, and it is not sent.
  assert.deepEqual(policyProblems({ primary: 'a', fallback: ['zzz'], noFallback: true }, routable), []);
  assert.deepEqual(policyBody({ primary: 'a', fallback: ['b'], noFallback: true }), { primary: 'a', fallback: [], no_fallback: true });
  assert.deepEqual(policyBody({ primary: 'a', fallback: ['a', 'b'], noFallback: false }), { primary: 'a', fallback: ['b'], no_fallback: false });
});

test('domains that forbid substitution always start with fallback off', () => {
  assert.equal(formFrom(undefined, domain({ no_fallback: true })).noFallback, true);
  assert.equal(formFrom({ domain: 'x', primary: 'a', fallback: ['b'] }, domain()).noFallback, false);
});

test('fallback order is kept and can be changed', () => {
  let f = { primary: 'a', fallback: [] as string[], noFallback: false };
  f = toggleFallback(toggleFallback(toggleFallback(f, 'b'), 'c'), 'd');
  assert.deepEqual(f.fallback, ['b', 'c', 'd']);
  assert.deepEqual(moveFallback(f, 'd', -1).fallback, ['b', 'd', 'c']);
  assert.deepEqual(moveFallback(f, 'b', -1).fallback, ['b', 'c', 'd']);
  assert.deepEqual(toggleFallback(f, 'c').fallback, ['b', 'd']);
});

test('the policy line says what is used and whether a feature reads it', () => {
  const sources = [src('yahoo', { label: 'Yahoo' }), src('finnhub', { label: 'Finnhub' })];
  assert.match(policyLine(undefined, domain(), sources), /features choose their own source/);
  assert.match(policyLine(undefined, domain({ routed: false }), sources), /none reads a policy/);
  assert.equal(policyLine({ domain: 'market_quote', primary: 'yahoo', fallback: ['finnhub'] }, domain(), sources), 'Yahoo, then Finnhub.');
  assert.equal(policyLine({ domain: 'market_quote', primary: 'yahoo', no_fallback: true }, domain(), sources), 'Yahoo, no fallback.');
  assert.match(policyLine({ domain: 'news', primary: 'yahoo' }, domain({ routed: false }), sources), /no feature reads it for this domain yet/);
});
