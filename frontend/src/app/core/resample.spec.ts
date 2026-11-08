import assert from 'node:assert/strict';
import test from 'node:test';

import type { HistorySeries } from '../models';
import { availableIntervals, bucketOf, emaSeed, intervalSeconds, mergeBars, parseIntervalChoice, resampleSeries } from './resample.ts';

const bar = (t: number, o: number, h: number, l: number, c: number, v = 10) => ({ t, o, h, l, c, v });
const series = (interval: string, points: ReturnType<typeof bar>[], stops?: number[]): HistorySeries =>
  ({ symbol: 'X', provider: 'P', range: '5D', interval, currency: 'USD', points, emaConfig: { fastPeriod: 12, slowPeriod: 26 }, stops });

test('interval labels are read, and an unknown label is not guessed', () => {
  assert.equal(intervalSeconds('5m'), 300);
  assert.equal(intervalSeconds('1h'), 3600);
  assert.equal(intervalSeconds('1d'), 86400);
  assert.equal(intervalSeconds('1wk'), 604800);
  assert.equal(intervalSeconds('1mo'), 2592000);
  assert.equal(intervalSeconds('tick'), null);
  assert.equal(intervalSeconds(undefined), null);
  assert.equal(intervalSeconds('0m'), null);
});

test('only lengths longer than the provider bars, and fitting them, are offered', () => {
  assert.deepEqual(availableIntervals(300), ['15m', '30m', '1h', '4h', '1d', '1wk', '1mo']);
  assert.deepEqual(availableIntervals(1800), ['1h', '4h', '1d', '1wk', '1mo']);
  assert.deepEqual(availableIntervals(86400), ['1wk', '1mo']);
  assert.deepEqual(availableIntervals(604800), ['1mo']);
  assert.deepEqual(availableIntervals(2592000), []);
  assert.deepEqual(availableIntervals(null), []);
  assert.deepEqual(availableIntervals(7200), ['4h', '1d', '1wk', '1mo']); // 2h bars merge into 4h but not 1h or 30m
});

test('a stored choice that is not an interval becomes Auto', () => {
  assert.equal(parseIntervalChoice('4h'), '4h');
  assert.equal(parseIntervalChoice('2w'), 'auto');
  assert.equal(parseIntervalChoice(null), 'auto');
});

test('merged bars keep the first open, the extremes, the last close and the summed volume', () => {
  const base = 1_700_000_000 - (1_700_000_000 % 3600); // on the hour
  const { points, last } = mergeBars([
    bar(base, 10, 12, 9, 11), bar(base + 1800, 11, 15, 10, 14),
    bar(base + 3600, 14, 16, 13, 13.5), bar(base + 5400, 13.5, 14, 12, 12.5),
  ], '1h');
  assert.deepEqual(points, [
    { t: base, o: 10, h: 15, l: 9, c: 14, v: 20 },
    { t: base + 3600, o: 14, h: 16, l: 12, c: 12.5, v: 20 },
  ]);
  assert.deepEqual(last, [1, 3]);
});

test('weeks start on Monday and months follow the UTC calendar', () => {
  const day = (d: string) => Date.parse(`${d}T12:00:00Z`) / 1000;
  assert.equal(bucketOf('1wk', day('2026-11-02')), bucketOf('1wk', day('2026-11-08'))); // Mon..Sun
  assert.notEqual(bucketOf('1wk', day('2026-11-08')), bucketOf('1wk', day('2026-11-09')));
  assert.equal(bucketOf('1mo', day('2026-10-01')), bucketOf('1mo', day('2026-10-31')));
  assert.notEqual(bucketOf('1mo', day('2026-10-31')), bucketOf('1mo', day('2026-11-01')));
  assert.equal(bucketOf('1mo', Date.parse('2026-10-31T23:59:59Z') / 1000), bucketOf('1mo', day('2026-10-01')));
});

test('a series is merged to the chosen length, with the stop at the end of each merged bar', () => {
  const d = (n: number) => 1_700_000_000 - (1_700_000_000 % 86400) + n * 86400;
  const s = series('1d', [bar(d(0), 1, 2, 1, 2), bar(d(1), 2, 3, 2, 3), bar(d(7), 3, 4, 3, 4)], [0, 5, 6]);
  const w = resampleSeries(s, '1wk');
  assert.equal(w.interval, '1wk');
  assert.equal(w.points.length, 2);
  assert.deepEqual(w.stops, [5, 6]);
  assert.equal(s.points.length, 3, 'the loaded series is not changed');
});

test('Auto, and a length the provider bars cannot make, leave the series as it is', () => {
  const s = series('1d', [bar(1, 1, 2, 1, 2)]);
  assert.equal(resampleSeries(s, 'auto'), s);
  assert.equal(resampleSeries(s, '1h'), s);
  assert.equal(resampleSeries(series('tick', [bar(1, 1, 2, 1, 2)]), '1d').interval, 'tick');
});

test('a bar without open, high or low is merged from its close', () => {
  const { points } = mergeBars([{ t: 0, o: 0, h: 0, l: 0, c: 5, v: 0 }, { t: 1, o: 0, h: 0, l: 0, c: 7, v: 0 }], '1h');
  assert.deepEqual(points, [{ t: 0, o: 5, h: 7, l: 5, c: 7, v: 0 }]);
});

test('the EMA seed follows the tracker formula and is empty without bars', () => {
  assert.equal(emaSeed(series('1d', [])), null);
  const e = emaSeed(series('1d', [bar(1, 10, 10, 10, 10), bar(2, 20, 20, 20, 20)]))!;
  assert.equal(e.price, 20);
  assert.ok(Math.abs(e.fastEMA - (10 + 10 * (2 / 13))) < 1e-9);
  assert.ok(Math.abs(e.slowEMA - (10 + 10 * (2 / 27))) < 1e-9);
});
