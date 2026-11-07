import assert from 'node:assert/strict';
import test from 'node:test';

import { CHART_STYLES, barsFor, fullBar, heikinAshi, isCandleStyle, parseChartStyle } from './chart-styles.ts';

test('an unknown or missing stored style falls back to the default', () => {
  assert.equal(parseChartStyle('candles'), 'candles');
  assert.equal(parseChartStyle('pie'), 'area');
  assert.equal(parseChartStyle(null), 'area');
});

test('only the OHLC styles are drawn as bars', () => {
  assert.deepEqual(CHART_STYLES.filter(s => isCandleStyle(s.id)).map(s => s.id), ['candles', 'hollow', 'ohlc', 'heikin']);
});

test('a live tick without a range draws as a flat bar', () => {
  assert.deepEqual(fullBar({ c: 10 }), { o: 10, h: 10, l: 10, c: 10 });
});

test('a bar whose high or low is inside its open and close is widened to contain them', () => {
  assert.deepEqual(fullBar({ o: 12, h: 11, l: 13, c: 10 }), { o: 12, h: 12, l: 10, c: 10 });
});

test('Heikin Ashi averages each bar and opens at the middle of the previous body', () => {
  const ha = heikinAshi([{ o: 10, h: 14, l: 9, c: 12 }, { o: 12, h: 16, l: 11, c: 15 }]);
  assert.deepEqual(ha[0], { o: 11, h: 14, l: 9, c: 11.25 });
  assert.equal(ha[1].o, (11 + 11.25) / 2);
  assert.equal(ha[1].c, (12 + 16 + 11 + 15) / 4);
  assert.ok(ha[1].h >= Math.max(ha[1].o, ha[1].c) && ha[1].l <= Math.min(ha[1].o, ha[1].c));
});

test('plain candles keep the traded prices and Heikin Ashi does not', () => {
  const bars = [{ o: 1, h: 3, l: 0.5, c: 2 }, { o: 2, h: 4, l: 1.5, c: 3.5 }];
  assert.deepEqual(barsFor('candles', bars), bars);
  assert.notDeepEqual(barsFor('heikin', bars), bars);
});
