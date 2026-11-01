// Runs with Node's built-in test runner:
//   node --test src/app/ui/format.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { duration, everyLabel, formatMoney, runLabel, runTone, timeAgo } from './format.ts';

const NOW = Date.UTC(2026, 9, 30, 12, 0, 0);

test('timeAgo reads naturally and tolerates missing values', () => {
  assert.equal(timeAgo(NOW - 10_000, NOW), 'just now');
  assert.equal(timeAgo(NOW - 5 * 60_000, NOW), '5 min ago');
  assert.equal(timeAgo(NOW - 3 * 3600_000, NOW), '3 h ago');
  assert.equal(timeAgo(NOW - 30 * 3600_000, NOW), 'yesterday');
  assert.equal(timeAgo(new Date(NOW - 3 * 86400_000).toISOString(), NOW), '3 days ago');
  assert.equal(timeAgo(null, NOW), '—');
  assert.equal(timeAgo('not a date', NOW), '—');
});

test('duration picks a sensible unit', () => {
  assert.equal(duration(820), '820 ms');
  assert.equal(duration(4200), '4.2 s');
  assert.equal(duration(190_000), '3 min 10 s');
  assert.equal(duration(null), '—');
});

test('schedules read as words where possible', () => {
  assert.equal(everyLabel(15), 'Every 15 min');
  assert.equal(everyLabel(60), 'Hourly');
  assert.equal(everyLabel(360), 'Every 6 h');
  assert.equal(everyLabel(1440), 'Daily');
  assert.equal(everyLabel(2880), 'Every 2 days');
  assert.equal(everyLabel(10080), 'Weekly');
});

test('run statuses map to labels and tones', () => {
  assert.equal(runLabel('AWAITING_APPROVAL'), 'Awaiting approval');
  assert.equal(runTone('SUCCESS'), 'is-success');
  assert.equal(runTone('DEAD'), 'is-danger');
  assert.equal(runTone('QUEUED'), 'is-info');
  assert.equal(runTone('AWAITING_APPROVAL'), 'is-warning');
  assert.equal(runTone('CANCELLED'), '');
});

test('money is whole units with a currency when the code is usable', () => {
  assert.equal(formatMoney(1234567.89, 'USD', 'en-US'), '$1,234,568');
  assert.equal(formatMoney(-50, 'usd', 'en-US'), '-$50');
  assert.equal(formatMoney(1500, '', 'en-US'), '1,500');
  assert.equal(formatMoney(1500, 'EURO', 'en-US'), '1,500');
  assert.equal(formatMoney(1500, 'ZZZ', 'en-US').includes('1,500'), true);
  assert.equal(formatMoney(NaN, 'USD'), '—');
  assert.equal(formatMoney(undefined), '—');
});
