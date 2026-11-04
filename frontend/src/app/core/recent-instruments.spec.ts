import assert from 'node:assert/strict';
import test from 'node:test';

import { MAX_FAVORITES, MAX_RECENT, addRecent, isFavorite, parseFavorites, parseRecent, toggleFavorite } from './recent-instruments.ts';

test('favorites toggle on and off, per provider', () => {
  let list = toggleFavorite([], { symbol: 'volv-b.st', provider: 'yahoo', name: 'Volvo' });
  assert.deepEqual(list, [{ symbol: 'VOLV-B.ST', provider: 'YAHOO', name: 'Volvo' }]);
  assert.equal(isFavorite(list, { symbol: 'VOLV-B.ST', provider: 'YAHOO' }), true);
  assert.equal(isFavorite(list, { symbol: 'VOLV-B.ST', provider: 'FINNHUB' }), false);
  list = toggleFavorite(list, { symbol: 'VOLV-B.ST', provider: 'FINNHUB' });
  assert.equal(list.length, 2);
  list = toggleFavorite(list, { symbol: 'VOLV-B.ST', provider: 'YAHOO' });
  assert.deepEqual(list.map(f => f.provider), ['FINNHUB']);
});

test('a full favorites list refuses a new entry but still lets one go', () => {
  const full = Array.from({ length: MAX_FAVORITES }, (_, i) => ({ symbol: `S${i}`, provider: 'YAHOO' }));
  assert.equal(toggleFavorite(full, { symbol: 'NEW', provider: 'YAHOO' }).length, MAX_FAVORITES);
  assert.equal(toggleFavorite(full, { symbol: 'S3', provider: 'YAHOO' }).length, MAX_FAVORITES - 1);
});

test('stored favorites that are malformed are dropped, never thrown on', () => {
  assert.deepEqual(parseFavorites('not json'), []);
  assert.deepEqual(parseFavorites('[{"symbol":"<b>","provider":"YAHOO"},{"symbol":"AAPL","provider":"YAHOO"}]').map(f => f.symbol), ['AAPL']);
});

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
