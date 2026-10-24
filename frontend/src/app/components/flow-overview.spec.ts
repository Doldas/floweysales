// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/flow-overview.spec.ts
// No extra test dependencies are required.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { listOverviewFlows } from './flow-overview.ts';

const flows = [
  { id: 1, name: 'Daily digest', enabled: true, interval_minutes: 60, event_key: '' },
  { id: 2, name: 'alpha screener', enabled: false, interval_minutes: 0, event_key: 'price.drop' },
  { id: 3, name: 'Earnings watch', enabled: true, interval_minutes: 0, event_key: 'earnings.released' },
  { id: 4, name: 'Beta scan', enabled: false, interval_minutes: 15, event_key: '' },
];

const ids = (items: { id: number }[]) => items.map(flow => flow.id);

test('default filter, search and sort list every flow sorted A–Z', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'all', 'name')), [2, 4, 1, 3]);
});

test('name sort is locale-aware and case-insensitive', () => {
  // 'alpha' sorts before 'Beta' and 'Daily', regardless of case.
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'all', 'name')).slice(0, 2), [2, 4]);
});

test('name-desc reverses the alphabetical order', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'all', 'name-desc')), [3, 1, 4, 2]);
});

test('newest sorts by highest id first and oldest by lowest id first', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'all', 'newest')), [4, 3, 2, 1]);
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'all', 'oldest')), [1, 2, 3, 4]);
});

test('an unknown sort value falls back to name A–Z', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'all', 'bogus')), [2, 4, 1, 3]);
});

test('enabled and disabled filters use the enabled flag', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'enabled', 'oldest')), [1, 3]);
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'disabled', 'oldest')), [2, 4]);
});

test('scheduled and manual filters use the interval', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'scheduled', 'oldest')), [1, 4]);
  assert.deepEqual(ids(listOverviewFlows(flows, '', 'manual', 'oldest')), [2, 3]);
});

test('an unrecognised filter value matches nothing', () => {
  assert.deepEqual(listOverviewFlows(flows, '', 'weekly', 'name'), []);
});

test('search matches the flow name case-insensitively', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, 'SCAN', 'all', 'oldest')), [4]);
});

test('search also matches the event key', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, 'earnings', 'all', 'oldest')), [3]);
});

test('search is trimmed and a whitespace-only search matches everything', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, '  digest  ', 'all', 'oldest')), [1]);
  assert.equal(listOverviewFlows(flows, '   ', 'all', 'oldest').length, flows.length);
});

test('search, filter and sort combine', () => {
  assert.deepEqual(ids(listOverviewFlows(flows, 'a', 'manual', 'newest')), [3, 2]);
});

test('listing does not mutate the input array', () => {
  const copy = flows.map(flow => ({ ...flow }));
  listOverviewFlows(copy, '', 'all', 'name-desc');
  assert.deepEqual(copy.map(flow => flow.id), [1, 2, 3, 4]);
});
