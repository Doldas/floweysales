import assert from 'node:assert/strict';
import test from 'node:test';

import { feedStatus, type FeedInput } from './feed-status.ts';

const NOW = Date.UTC(2026, 10, 4, 14, 40);
const recent = NOW - 60_000;

const input = (over: Partial<FeedInput> = {}): FeedInput => ({
  research: false, hasTracker: true, connection: 'connected', lastUpdateAt: recent,
  quoteKind: 'realtime', providerLabel: 'Yahoo', officialApi: false, ...over,
});

test('a connection alone is never live', () => {
  for (const quoteKind of ['', 'ohlc_bar_close', 'delayed_15m', 'something_else']) {
    for (const lastUpdateAt of [null, recent, NOW - 3_600_000]) {
      const s = feedStatus(input({ quoteKind, lastUpdateAt }), NOW);
      assert.notEqual(s.headline, 'Live prices', `${quoteKind} / ${lastUpdateAt}`);
      assert.equal(s.pulse, false);
    }
  }
});

test('live needs a provider that says real time and a recent update', () => {
  const s = feedStatus(input(), NOW);
  assert.equal(s.headline, 'Live prices');
  assert.equal(s.pulse, true);
});

test('connected but stale reports the last update, not live', () => {
  const s = feedStatus(input({ lastUpdateAt: NOW - 20 * 60_000 }), NOW);
  assert.equal(s.headline, 'No new prices since 14:20 UTC');
  assert.equal(s.freshness, 'stale');
  assert.equal(s.canReconnect, true);
  assert.equal(s.pulse, false);
});

test('connected before the first price waits honestly', () => {
  const s = feedStatus(input({ lastUpdateAt: null }), NOW);
  assert.equal(s.headline, 'Waiting for the first price');
  assert.equal(s.freshness, 'no-data');
});

test('bar-close data shows the update time and says it is not a stream', () => {
  const s = feedStatus(input({ quoteKind: 'ohlc_bar_close' }), NOW);
  assert.equal(s.headline, 'Last update 14:39 UTC');
  assert.equal(s.dataType, 'bar-close');
  assert.match(s.details.find(d => d.label === 'Data type')!.text, /not a streaming quote/);
});

test('delayed quotes are labelled delayed', () => {
  assert.equal(feedStatus(input({ quoteKind: 'delayed_15m' }), NOW).headline, 'Delayed prices');
});

test('the market session is never claimed', () => {
  const s = feedStatus(input(), NOW);
  assert.match(s.details.find(d => d.label === 'Market session')!.text, /^Unknown/);
});

test('reconnecting, offline and recovery', () => {
  assert.equal(feedStatus(input({ connection: 'reconnecting' }), NOW).headline, 'Reconnecting…');
  const offline = feedStatus(input({ connection: 'offline' }), NOW);
  assert.equal(offline.headline, 'Prices not updating');
  assert.equal(offline.tone, 'is-danger');
  assert.equal(offline.canReconnect, true);
  assert.equal(feedStatus(input({ connection: 'connected' }), NOW).headline, 'Live prices');
});

test('research mode and no tracker have their own states', () => {
  assert.equal(feedStatus(input({ research: true }), NOW).headline, 'Prices from history');
  assert.equal(feedStatus(input({ hasTracker: false }), NOW).headline, 'No active tracker');
});

test('unknown provider facts are disclosed', () => {
  const s = feedStatus(input({ quoteKind: '', officialApi: null }), NOW);
  assert.match(s.details.find(d => d.label === 'Data type')!.text, /does not say/);
});
