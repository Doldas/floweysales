import assert from 'node:assert/strict';
import test from 'node:test';

import { WINDOW_THRESHOLD, clampHeight, heightForKey, parsePrefs, rowWindow, serializePrefs, withDensity, withPanel } from './workspace-prefs.ts';

test('missing or broken storage gives comfortable defaults', () => {
  for (const raw of [null, '', 'not json', '42', '[]', 'null']) {
    assert.deepEqual(parsePrefs(raw), { density: 'comfortable', panels: {} });
  }
});

test('stored values round trip and bad ones are dropped', () => {
  const raw = JSON.stringify({
    density: 'compact',
    panels: { 'markets.chart': { collapsed: true, height: 5000 }, 'Bad Id!': { collapsed: true }, 'a.b': { height: 'tall' }, x: 'y', 'ok-1': { height: 300.4 } },
  });
  const p = parsePrefs(raw);
  assert.equal(p.density, 'compact');
  assert.deepEqual(p.panels, { 'markets.chart': { collapsed: true, height: 1400 }, 'ok-1': { height: 300 } });
  assert.deepEqual(parsePrefs(serializePrefs(p)), p);
});

test('an unknown density is ignored', () => {
  assert.equal(parsePrefs('{"density":"huge"}').density, 'comfortable');
  assert.equal(withDensity(parsePrefs(null), 'huge' as never).density, 'comfortable');
  assert.equal(withDensity(parsePrefs(null), 'compact').density, 'compact');
});

test('panel state merges and a default state is removed', () => {
  let p = withPanel(parsePrefs(null), 'markets.chart', { height: 10 });
  assert.deepEqual(p.panels, { 'markets.chart': { height: 120 } });
  p = withPanel(p, 'markets.chart', { collapsed: true });
  assert.deepEqual(p.panels['markets.chart'], { collapsed: true, height: 120 });
  p = withPanel(p, 'markets.chart', { collapsed: false });
  assert.deepEqual(p.panels['markets.chart'], { collapsed: false, height: 120 });
  p = withPanel(p, 'markets.chart', { height: undefined, collapsed: undefined });
  assert.deepEqual(p.panels, {});
  assert.equal(withPanel(p, '../evil', { collapsed: true }), p);
});

test('the number of stored panels is bounded', () => {
  const panels: Record<string, unknown> = {};
  for (let i = 0; i < 200; i++) panels[`p${i}`] = { collapsed: true };
  assert.equal(Object.keys(parsePrefs(JSON.stringify({ panels })).panels).length, 60);
});

test('clampHeight keeps a pane usable', () => {
  assert.equal(clampHeight(NaN), 120);
  assert.equal(clampHeight(-5), 120);
  assert.equal(clampHeight(9999), 1400);
  assert.equal(clampHeight(333.6), 334);
});

test('keyboard resize steps, pages and jumps within limits', () => {
  assert.equal(heightForKey('ArrowDown', 300), 324);
  assert.equal(heightForKey('ArrowUp', 300), 276);
  assert.equal(heightForKey('ArrowUp', 130), 120);
  assert.equal(heightForKey('PageDown', 300), 420);
  assert.equal(heightForKey('Home', 300), 120);
  assert.equal(heightForKey('End', 300, 100, 800), 800);
  assert.equal(heightForKey('Enter', 300), null);
});

test('rowWindow draws only the visible rows plus overscan and keeps the spacer total', () => {
  const w = rowWindow(1000, 4000, 400, 40, 5);
  assert.equal(w.start, 95);
  assert.equal(w.end, 110 + 5);
  assert.equal(w.before, 95 * 40);
  assert.equal(w.before + (w.end - w.start) * 40 + w.after, 1000 * 40);
});

test('rowWindow handles the edges and bad input', () => {
  assert.deepEqual(rowWindow(0, 0, 400, 40), { start: 0, end: 0, before: 0, after: 0 });
  const top = rowWindow(500, 0, 400, 40, 3);
  assert.equal(top.start, 0);
  assert.equal(top.before, 0);
  const bottom = rowWindow(500, 1e9, 400, 40, 3);
  assert.equal(bottom.end, 500);
  assert.equal(bottom.after, 0);
  assert.ok(bottom.end > bottom.start);
  const bad = rowWindow(50, NaN, NaN, 0);
  assert.ok(bad.end > bad.start && bad.end <= 50);
});

test('windowing a 20000 row table draws a small fixed number of rows', () => {
  const w = rowWindow(20000, 300000, 600, 38);
  assert.ok(w.end - w.start < 40);
  assert.ok(WINDOW_THRESHOLD < 20000);
});

test('row window clamps nonfinite counts and invalid overscan without losing rows', () => {
  assert.deepEqual(rowWindow(NaN, 0, 500, 40), { start: 0, end: 0, before: 0, after: 0 });
  const normal = rowWindow(100, 80, 200, 40, 0);
  const invalid = rowWindow(100, 80, 200, 40, -Infinity);
  assert.deepEqual(invalid, normal);
  assert.ok(normal.end > normal.start);
});
