import assert from 'node:assert/strict';
import test from 'node:test';

import { MAX_RECENT, addRecent, parseRecent } from './recent-instruments.ts';

test('the newest instrument comes first and a repeat moves up instead of doubling', () => {
  let list = addRecent([], { symbol: 'nvda', provider: 'yahoo' });
  list = addRecent(list, { symbol: 'AZN.ST', provider: 'YAHOO' });
  list = addRecent(list, { symbol: 'NVDA', provider: 'YAHOO' });
  assert.deepEqual(list.map(r => r.symbol), ['NVDA', 'AZN.ST']);
});

test('the same ticker on two providers stays two entries', () => {
  let list = addRecent([], { symbol: 'AZN', provider: 'YAHOO' });
  list = addRecent(list, { symbol: 'AZN', provider: 'AVANZA' });
  assert.equal(list.length, 2);
});

test('a known name and exchange survive a repeat that has none', () => {
  let list = addRecent([], { symbol: 'AZN.ST', provider: 'YAHOO', name: 'AstraZeneca', exchange: 'Stockholm' });
  list = addRecent(list, { symbol: 'AZN.ST', provider: 'YAHOO' });
  assert.deepEqual(list[0], { symbol: 'AZN.ST', provider: 'YAHOO', name: 'AstraZeneca', exchange: 'Stockholm' });
});

test('the list is capped', () => {
  let list: ReturnType<typeof addRecent> = [];
  for (let i = 0; i < MAX_RECENT + 5; i++) list = addRecent(list, { symbol: `S${i}`, provider: 'YAHOO' });
  assert.equal(list.length, MAX_RECENT);
  assert.equal(list[0].symbol, `S${MAX_RECENT + 4}`);
});

test('an empty symbol or provider adds nothing', () => {
  assert.deepEqual(addRecent([], { symbol: ' ', provider: 'YAHOO' }), []);
  assert.deepEqual(addRecent([], { symbol: 'NVDA', provider: '' }), []);
});

test('stored data that is damaged or hostile is dropped, never thrown on', () => {
  assert.deepEqual(parseRecent(null), []);
  assert.deepEqual(parseRecent('not json'), []);
  assert.deepEqual(parseRecent('{"a":1}'), []);
  const parsed = parseRecent(JSON.stringify([
    { symbol: 'nvda', provider: 'yahoo' },
    { symbol: '<script>', provider: 'YAHOO' },
    { symbol: 'NVDA', provider: 'YAHOO' },
    { symbol: 5, provider: 'YAHOO' },
    null,
  ]));
  assert.deepEqual(parsed, [{ symbol: 'NVDA', provider: 'YAHOO' }]);
});
