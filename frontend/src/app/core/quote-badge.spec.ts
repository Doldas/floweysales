import assert from 'node:assert/strict';
import test from 'node:test';

import { ageText, quoteBadge } from './quote-badge.ts';

const q = (over: object = {}) => ({ age_seconds: 20, is_realtime: false, stale: false, market_session: 'open', ...over });

test('ages are written in the largest whole unit', () => {
  assert.equal(ageText(0), '0 s');
  assert.equal(ageText(59), '59 s');
  assert.equal(ageText(60), '1 min');
  assert.equal(ageText(900), '15 min');
  assert.equal(ageText(7200), '2 h');
  assert.equal(ageText(172800), '2 d');
  assert.equal(ageText(-5), '0 s');
});

test('a quote is live only when the provider says it is real time', () => {
  assert.deepEqual(quoteBadge(q({ is_realtime: true })), { text: 'live', tone: 'live' });
  assert.deepEqual(quoteBadge(q()), { text: '20 s ago', tone: 'delayed' });
});

test('stale beats a closed market, and a closed market beats real time', () => {
  assert.deepEqual(quoteBadge(q({ stale: true, market_session: 'closed', age_seconds: 10800 })), { text: 'stale · 3 h', tone: 'stale' });
  assert.deepEqual(quoteBadge(q({ market_session: 'closed', is_realtime: true, age_seconds: 7200 })), { text: 'closed · 2 h', tone: 'closed' });
  assert.equal(quoteBadge(q({ market_session: 'unknown' })).tone, 'delayed');
});
