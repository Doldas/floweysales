// Runs with Node's built-in test runner:
//   node --test src/app/components/events-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { daysUntil, summarizeEvents } from './events-view.ts';
import type { CorporateEvent } from '../models';

const ev = (kind: CorporateEvent['kind'], date: string, amount?: number, confirmed = true): CorporateEvent => ({ symbol: 'AAA', kind, date, amount, confirmed });
const today = '2026-11-01';

test('next earnings and ex-dividend dates come from upcoming events only', () => {
  const s = summarizeEvents([], [ev('earnings', '2026-12-05', undefined, false), ev('earnings', '2026-11-20'), ev('dividend', '2026-11-14', 0.25), ev('dividend', '2026-10-01', 0.2)], 100, today);
  assert.equal(s.nextEarnings?.date, '2026-11-20');
  assert.equal(s.nextDividend?.date, '2026-11-14');
});

test('trailing twelve month dividends and yield', () => {
  const past = [ev('dividend', '2026-08-14', 0.25), ev('dividend', '2026-05-14', 0.25), ev('dividend', '2026-02-14', 0.25), ev('dividend', '2025-11-14', 0.25),
    ev('dividend', '2025-10-30', 0.25), ev('dividend', '2026-03-01'), ev('split', '2026-06-01')];
  const s = summarizeEvents(past, [], 50, today);
  assert.equal(s.ttmDividend, 1);
  assert.equal(s.yieldPct, 2);
});

test('absent data stays absent instead of zero', () => {
  const s = summarizeEvents([], [], 100, today);
  assert.deepEqual(s, { nextEarnings: undefined, nextDividend: undefined });
  assert.equal(summarizeEvents([ev('dividend', '2026-08-14', 0.25)], [], 0, today).yieldPct, undefined);
});

test('relative days', () => {
  assert.equal(daysUntil('2026-11-01', today), 'today');
  assert.equal(daysUntil('2026-11-02', today), 'tomorrow');
  assert.equal(daysUntil('2026-11-20', today), 'in 19 days');
});
