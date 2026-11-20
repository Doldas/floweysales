import { strict as assert } from 'node:assert';
import { test } from 'node:test';

import { chartMarks, markPlacement } from './chart-marks.ts';

test('an open position and its alerts become price lines, entry first and alerts high to low', () => {
  const marks = chartMarks(
    [{ level: 100, direction: 'below' }, { level: 130, direction: 'above' }, { level: 125, direction: 'above', triggered_at: 9 }, { level: 0, direction: 'above' }],
    { quantity: 10, avg_entry: 110 },
  );
  assert.deepEqual(marks.map(m => `${m.label} ${m.price} ${m.tone}`), ['Avg entry 110 entry', 'Alert above 130 alert', 'Alert hit 125 hit', 'Alert below 100 alert']);
});

test('a short is named as one and a flat or missing position draws no entry', () => {
  assert.equal(chartMarks([], { quantity: -2, avg_entry: 50 })[0].label, 'Avg short entry');
  assert.deepEqual(chartMarks([], { quantity: 0, avg_entry: 50 }), []);
  assert.deepEqual(chartMarks([], null), []);
});

test('a mark outside the visible range is placed at the edge it is beyond', () => {
  assert.equal(markPlacement(120, 100, 110), 'above');
  assert.equal(markPlacement(90, 100, 110), 'below');
  assert.equal(markPlacement(105, 100, 110), 'in');
});
