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
  assert.equal(feedStatus(input({ quoteKind: 'delayed' }), NOW).headline, 'Delayed prices');
});

test('the market session is never claimed', () => {
  const s = feedStatus(input(), NOW);
  assert.match(s.details.find(d => d.label === 'Market session')!.text, /^Unknown/);
});

test('a stated quote time shows in the headline and the details', () => {
  const quoteTimeAt = Date.UTC(2026, 10, 4, 14, 32);
  const s = feedStatus(input({ quoteKind: 'ohlc_bar_close', quoteTimeAt }), NOW);
  assert.equal(s.headline, 'Last quote 14:32 UTC');
  assert.match(s.details.find(d => d.label === 'Freshness')!.text, /quote itself is from 14:32 UTC/);
  assert.match(feedStatus(input({ quoteKind: 'ohlc_bar_close' }), NOW).details[1].text, /did not state the quote's own time/);
});

test('delayed data shows the delay, from the tick or from the declared kind', () => {
  assert.equal(feedStatus(input({ quoteKind: 'delayed_15m' }), NOW).headline, 'Delayed 15 min');
  const tick = feedStatus(input({ quoteKind: 'ohlc_bar_close', delaySeconds: 1200 }), NOW);
  assert.equal(tick.headline, 'Delayed 20 min');
  assert.equal(tick.pulse, false);
});

test('a stated delay overrides a real-time claim, and a stated zero does not create one', () => {
  const delayed = feedStatus(input({ quoteKind: 'realtime', delaySeconds: 900 }), NOW);
  assert.equal(delayed.headline, 'Delayed 15 min');
  assert.equal(delayed.pulse, false);
  const zero = feedStatus(input({ quoteKind: 'ohlc_bar_close', delaySeconds: 0 }), NOW);
  assert.notEqual(zero.headline, 'Live prices');
  assert.equal(zero.dataType, 'bar-close');
});

test('market closed beats stale and does not offer a reconnect', () => {
  const quoteTimeAt = Date.UTC(2026, 10, 3, 16, 30);
  const s = feedStatus(input({ lastUpdateAt: NOW - 3 * 3_600_000, quoteTimeAt, marketState: 'closed', marketBasis: 'provider' }), NOW);
  assert.equal(s.headline, 'Market closed · last 16:30 UTC');
  assert.equal(s.session, 'closed');
  assert.equal(s.canReconnect, false);
  assert.equal(s.pulse, false);
  assert.match(s.details.find(d => d.label === 'Market session')!.text, /as stated by the provider/);
});

test('a computed session says its basis and that holidays are unknown', () => {
  const s = feedStatus(input({ marketState: 'open', marketBasis: 'regular_hours', quoteKind: 'ohlc_bar_close' }), NOW);
  assert.equal(s.session, 'open');
  assert.match(s.details.find(d => d.label === 'Market session')!.text, /regular weekday hours.*Holidays/);
  assert.equal(s.headline, 'Last update 14:39 UTC');
});

test('a closed market never hides a connection problem', () => {
  assert.equal(feedStatus(input({ marketState: 'closed', connection: 'offline' }), NOW).headline, 'Prices not updating');
  assert.equal(feedStatus(input({ marketState: 'closed', lastUpdateAt: null }), NOW).headline, 'Waiting for the first price');
});

test('recovery: stale with a closed market is calm, then a fresh live tick is live', () => {
  const old = NOW - 5 * 3_600_000;
  assert.match(feedStatus(input({ connection: 'reconnecting', lastUpdateAt: old, marketState: 'closed' }), NOW).headline, /Reconnecting/);
  assert.equal(feedStatus(input({ marketState: 'open', marketBasis: 'provider' }), NOW).headline, 'Live prices');
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

test('a selected instrument without a tracker is not shown as live or as having no selection', () => {
  const s = feedStatus(input({ hasTracker: false, hasSelection: true }), NOW);
  assert.equal(s.headline, 'Not tracked live');
  assert.equal(s.pulse, false);
  assert.equal(s.canReconnect, false);
  assert.match(s.details[0].text, /Track live/);
});

test('unknown provider facts are disclosed', () => {
  const s = feedStatus(input({ quoteKind: '', officialApi: null }), NOW);
  assert.match(s.details.find(d => d.label === 'Data type')!.text, /does not say/);
});
