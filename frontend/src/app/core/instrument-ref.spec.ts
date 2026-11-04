import assert from 'node:assert/strict';
import test from 'node:test';

import { instrumentFromQuery, instrumentKey, instrumentLabel, isStaleResponse, pickInitialInstrument } from './instrument-ref.ts';

test('a link beats the stored selection, which beats nothing', () => {
  assert.equal(pickInitialInstrument('url', 'stored'), 'url');
  assert.equal(pickInitialInstrument(null, 'stored'), 'stored');
  assert.equal(pickInitialInstrument(null, null), null);
});

test('deep link parsing', () => {
  assert.deepEqual(instrumentFromQuery('?symbol=volv-b.st&provider=yahoo'), { symbol: 'VOLV-B.ST', provider: 'YAHOO' });
  assert.deepEqual(instrumentFromQuery('?symbol=NVDA'), { symbol: 'NVDA', provider: null });
  assert.equal(instrumentFromQuery('?provider=YAHOO'), null);
  assert.equal(instrumentFromQuery('?symbol=%3Cscript%3E'), null);
  assert.equal(instrumentFromQuery(''), null);
});

test('the same ticker on two providers is two instruments', () => {
  assert.notEqual(instrumentKey({ symbol: 'ABB', provider: 'YAHOO' }), instrumentKey({ symbol: 'ABB', provider: 'AVANZA' }));
  assert.equal(instrumentKey({ symbol: 'abb', provider: 'yahoo' }), 'YAHOO:ABB');
});

test('the label names exchange and provider', () => {
  assert.equal(instrumentLabel({ symbol: 'VOLV-B.ST', provider: 'YAHOO', name: 'Volvo AB', exchange: 'Stockholm' }), 'VOLV-B.ST · Volvo AB · Stockholm (YAHOO)');
  assert.equal(instrumentLabel({ symbol: 'NVDA', provider: 'YAHOO' }), 'NVDA (YAHOO)');
});

test('an older response is stale', () => {
  assert.equal(isStaleResponse('YAHOO:AAPL|1M', 'YAHOO:MSFT|1M'), true);
  assert.equal(isStaleResponse('YAHOO:AAPL|1M', 'YAHOO:AAPL|1Y'), true);
  assert.equal(isStaleResponse('YAHOO:AAPL|1M', 'YAHOO:AAPL|1M'), false);
});
