import { test } from 'node:test';
import assert from 'node:assert/strict';

import { exchangeById, formatCountdown, sessionAt, utcClock, utcDaySegments, zonedInstant } from './market-hours.ts';

const at = (iso: string) => Date.parse(iso);
const ex = (id: string) => {
  const e = exchangeById(id);
  assert.ok(e, id);
  return e;
};

test('a wall-clock time in a zone becomes the right instant on both sides of a DST change', () => {
  assert.equal(zonedInstant(2026, 7, 15, 9 * 60 + 30, 'America/New_York'), at('2026-07-15T13:30:00Z'));
  assert.equal(zonedInstant(2026, 1, 14, 9 * 60 + 30, 'America/New_York'), at('2026-01-14T14:30:00Z'));
  // Between the US and the European changes, New York is already on summer time and London is not.
  assert.equal(zonedInstant(2026, 3, 16, 9 * 60 + 30, 'America/New_York'), at('2026-03-16T13:30:00Z'));
  assert.equal(zonedInstant(2026, 3, 16, 8 * 60, 'Europe/London'), at('2026-03-16T08:00:00Z'));
  assert.equal(zonedInstant(2026, 11, 20, 10 * 60, 'Australia/Sydney'), at('2026-11-19T23:00:00Z'));
});

test('New York is open inside regular hours, with the close and the share of the session passed', () => {
  const s = sessionAt(ex('US'), at('2026-07-15T14:00:00Z'));
  assert.equal(s.state, 'open');
  assert.equal(s.closesAt, at('2026-07-15T20:00:00Z'));
  assert.ok(Math.abs((s.progress ?? 0) - 30 / 390) < 1e-9);
});

test('before the bell the next open is today, and at the weekend it is Monday', () => {
  assert.deepEqual(sessionAt(ex('US'), at('2026-01-14T14:00:00Z')), { state: 'closed', opensAt: at('2026-01-14T14:30:00Z') });
  assert.deepEqual(sessionAt(ex('US'), at('2026-11-21T15:00:00Z')), { state: 'closed', opensAt: at('2026-11-23T14:30:00Z') });
  // The close itself is no longer open.
  assert.equal(sessionAt(ex('US'), at('2026-11-20T21:00:00Z')).state, 'closed');
});

test('Tokyo has a lunch break, and after Friday close it opens on Monday', () => {
  assert.deepEqual(sessionAt(ex('XTKS'), at('2026-11-20T03:00:00Z')), { state: 'break', opensAt: at('2026-11-20T03:30:00Z') });
  assert.deepEqual(sessionAt(ex('XTKS'), at('2026-11-20T07:00:00Z')), { state: 'closed', opensAt: at('2026-11-23T00:00:00Z') });
});

test('crypto trades around the clock', () => {
  assert.deepEqual(sessionAt(ex('CRYPTO'), at('2026-11-21T15:00:00Z')), { state: 'open' });
  assert.deepEqual(utcDaySegments(ex('CRYPTO'), at('2026-11-21T15:00:00Z')).map(s => [s.from, s.to]), [[0, 1]]);
});

test('the UTC day shows each session as a share of 24 hours, including those across midnight', () => {
  const day = (id: string, iso: string) => utcDaySegments(ex(id), at(iso)).map(s => [s.from * 24, s.to * 24]);
  assert.deepEqual(day('XSTO', '2026-11-18T12:00:00Z'), [[8, 16.5]]);
  // Sydney's Friday starts on Thursday in UTC; its Saturday has no session.
  assert.deepEqual(day('XASX', '2026-11-19T12:00:00Z'), [[0, 5], [23, 24]]);
  assert.deepEqual(day('XASX', '2026-11-20T12:00:00Z'), [[0, 5]]);
  assert.deepEqual(day('XTKS', '2026-11-20T12:00:00Z'), [[0, 2.5], [3.5, 6.5]]);
  assert.deepEqual(day('US', '2026-11-22T12:00:00Z'), []);
});

test('countdowns and clock times read briefly', () => {
  assert.equal(formatCountdown(20_000), 'less than a minute');
  assert.equal(formatCountdown(45 * 60_000), '45 min');
  assert.equal(formatCountdown((2 * 60 + 5) * 60_000), '2 h 05 min');
  assert.equal(formatCountdown((28 * 60 + 40) * 60_000), '1 d 4 h');
  assert.equal(utcClock(at('2026-11-20T09:05:59Z')), '09:05');
});
