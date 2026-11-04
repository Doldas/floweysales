// Runs with Node's built-in test runner, under any TZ:
//   TZ=Asia/Tokyo node --test src/app/ui/datetime.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatNumber, formatUtcDate, formatUtcDateTime, formatUtcDateTimeSeconds, formatUtcLongDay, formatUtcMonthDay, formatUtcTime, todayUtc, toMillis, utcYear } from './datetime.ts';

const T = Date.UTC(2026, 10, 3, 23, 59, 5);

test('every format is UTC, whatever the machine time zone', () => {
  assert.equal(formatUtcDateTime(T), 'Nov 3, 2026, 23:59 UTC');
  assert.equal(formatUtcDateTimeSeconds(T), 'Nov 3, 2026, 23:59:05 UTC');
  assert.equal(formatUtcDate(T), 'Nov 3, 2026');
  assert.equal(formatUtcMonthDay(T), 'Nov 3');
  assert.equal(formatUtcTime(T), '23:59');
  assert.equal(formatUtcLongDay(T), 'Tuesday, November 3');
});

test('midnight UTC belongs to the new day, and a year ends at UTC midnight', () => {
  assert.equal(formatUtcDate(Date.UTC(2026, 10, 4, 0, 0, 0)), 'Nov 4, 2026');
  assert.equal(formatUtcTime(Date.UTC(2026, 10, 4, 0, 0, 0)), '00:00');
  assert.equal(utcYear(Date.UTC(2026, 11, 31, 23, 59)), 2026);
  assert.equal(utcYear(Date.UTC(2027, 0, 1, 0, 0)), 2027);
});

test('ISO strings and Dates are accepted; unusable values show a dash', () => {
  assert.equal(formatUtcDateTime('2026-11-03T14:32:00Z'), 'Nov 3, 2026, 14:32 UTC');
  assert.equal(formatUtcDate(new Date(T)), 'Nov 3, 2026');
  for (const bad of [null, undefined, '', 'nope', 0, -5]) assert.equal(formatUtcDateTime(bad), '—');
  assert.ok(Number.isNaN(toMillis('nope')));
  assert.ok(Number.isNaN(utcYear(null)));
});

test('today is the UTC date', () => {
  assert.equal(todayUtc(Date.UTC(2026, 10, 3, 23, 59)), '2026-11-03');
  assert.equal(todayUtc(Date.UTC(2026, 10, 4, 0, 1)), '2026-11-04');
});

test('numbers use en-US separators', () => {
  assert.equal(formatNumber(1234567.891, { maximumFractionDigits: 2 }), '1,234,567.89');
  assert.equal(formatNumber(NaN), '—');
  assert.equal(formatNumber(null), '—');
});
