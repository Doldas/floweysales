import assert from 'node:assert/strict';
import test from 'node:test';

import { chartMeta, chartSummary, intervalLabel } from './chart-meta.ts';

const fmt = (ms: number): string => new Date(ms).toISOString().replace('T', ' ').slice(0, 16) + ' UTC';

test('states provider, bars, units and the newest bar in UTC', () => {
  const facts = chartMeta({ provider: 'YAHOO', interval: '1d', currency: 'SEK', range: '1M', times: [1_700_000_000, 1_700_086_400] }, fmt);
  assert.deepEqual(facts.map(f => f.label), ['Source', 'Bars', 'Units', 'Newest bar']);
  assert.equal(facts[0].value, 'YAHOO');
  assert.equal(facts[1].value, 'daily bars, 1M');
  assert.equal(facts[2].value, 'prices in SEK');
  assert.equal(facts[3].value, '2023-11-15 22:13 UTC');
});

test('missing facts are named, never shown as zero', () => {
  const facts = chartMeta({}, fmt);
  assert.equal(facts[0].value, 'unknown provider');
  assert.equal(facts[1].value, 'bars of unknown size, range unknown');
  assert.equal(facts[2].value, 'currency not reported');
  assert.equal(facts[3].value, 'no bars loaded');
  assert.equal(chartMeta({ times: [0] }, fmt)[3].value, 'no bars loaded');
});

test('studies are labeled as calculated, with the EMA periods when known', () => {
  const facts = chartMeta({ provider: 'X', studies: ['EMA', 'RSI'], emaFast: 12, emaSlow: 26 }, fmt);
  const studies = facts.find(f => f.label === 'Studies')!;
  assert.match(studies.value, /EMA, RSI \(EMA periods 12 and 26\)/);
  assert.match(studies.value, /Calculated here/);
  assert.equal(chartMeta({ studies: [] }, fmt).some(f => f.label === 'Studies'), false);
});

test('interval labels and the one-line summary', () => {
  assert.equal(intervalLabel('1h'), 'hourly');
  assert.equal(intervalLabel('3d'), '3d');
  assert.equal(intervalLabel(undefined), 'unknown bar size');
  assert.equal(chartSummary({ range: '1Y', provider: 'FMP', currency: 'USD' }), '1Y · FMP · USD');
  assert.equal(chartSummary({}), 'No data loaded');
});
