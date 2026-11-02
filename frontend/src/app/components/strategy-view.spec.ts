// Runs with Node's built-in test runner:
//   node --test src/app/components/strategy-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { groupPresets, ownerLine, blankStrategy, cloneForEditing, defaultOperand, describeCondition, describeStrategy, geometry, nearestIndex, problems, tradeMarkers, usesFibonacci } from './strategy-view.ts';
import type { Persona, StrategyDef, StrategyRunResult } from '../models';

const pocket: StrategyDef = {
  name: 'Pocket', direction: 'long', family: 'fibonacci', id: 'fib_golden_pocket', sizing: { mode: 'risk_pct', value: 1 }, max_bars: 40,
  entry: [{ left: { type: 'price', field: 'low' }, op: '<=', right: { type: 'fib_retr', ratio: 0.618 } }],
  stop: { of: { type: 'fib_retr', ratio: 0.786 }, atr: 0.5 }, target: { of: { type: 'fib_ext', ratio: 1.272 } },
};

test('conditions and strategies read back as sentences', () => {
  assert.equal(describeCondition(pocket.entry[0]), 'the low is at or below the 61.8% Fibonacci retracement');
  const lines = describeStrategy(pocket);
  assert.match(lines[0], /^Buy at the next open when the low is at or below/);
  assert.ok(lines.includes('Stop: the 78.6% Fibonacci retracement beyond by 0.5 ATR.'));
  assert.ok(lines.includes('Target: the 127.2% Fibonacci extension.'));
  assert.ok(lines.includes('Close after 40 days at the latest.'));
  assert.ok(lines.includes('Size each trade so a stop-out loses 1% of equity.'));
});

test('a blank strategy needs only a name', () => {
  assert.deepEqual(problems(blankStrategy()), ['Give the strategy a name.']);
  const s = { ...blankStrategy(), name: 'x', stop: null };
  assert.deepEqual(problems(s), ['Add a way out: a stop, a target, an exit condition or a day limit.', 'Sizing by risk needs a stop.']);
});

test('copies for editing are independent and drop the preset identity', () => {
  const copy = cloneForEditing(pocket, true);
  copy.entry[0].right = defaultOperand('rsi');
  assert.equal(pocket.entry[0].right.type, 'fib_retr');
  assert.equal(copy.id, undefined);
  assert.equal(copy.family, undefined);
  assert.equal(copy.name, 'Pocket (copy)');
  assert.equal(cloneForEditing({ ...pocket, id: 'custom:3' }, false).id, 'custom:3');
});

test('fibonacci use is detected in rules, stop and target', () => {
  assert.equal(usesFibonacci(pocket), true);
  assert.equal(usesFibonacci(blankStrategy()), false);
});

test('nearest index finds the closest date on a thinned curve', () => {
  const dates = ['2024-01-01', '2024-01-10', '2024-01-20'];
  assert.equal(nearestIndex(dates, '2024-01-01'), 0);
  assert.equal(nearestIndex(dates, '2024-01-12'), 1);
  assert.equal(nearestIndex(dates, '2024-01-19'), 2);
  assert.equal(nearestIndex(dates, '2030-01-01'), 2);
});

test('trade markers land on the instrument line scaled by the base price', () => {
  const g = geometry([100, 120], 2, 200, 100);
  const r = {
    base_price: 50, curve: [{ date: '2024-01-01', close: 100, equity: 100 }, { date: '2024-01-10', close: 120, equity: 110 }],
    trades: [{ entry_date: '2024-01-01', entry_price: 50, exit_date: '2024-01-10', exit_price: 60, bars: 9, return_pct: 20, reason: 'target' }],
  } as unknown as StrategyRunResult;
  const [entry, exit] = tradeMarkers(r, g);
  assert.equal(entry.kind, 'entry');
  assert.equal(entry.x, g.x(0));
  assert.equal(entry.y, g.y(100));
  assert.equal(exit.y, g.y(120));
  assert.equal(exit.win, true);
});

const person = (id: string, name: string): Persona => ({ id, name, tagline: '', archetype: `The ${id} archetype.`, horizon: '', principles: [], checks: [], avoids: [], voice: '', tools: [] });
const preset = (id: string, family: string, persona?: string): StrategyDef => ({ ...pocket, id, family, persona });

test('presets are grouped by Fibonacci and by character, in the catalogue order', () => {
  const groups = groupPresets(
    [preset('p2', 'persona', 'quant'), preset('f1', 'fibonacci'), preset('p1', 'persona', 'owner'), preset('p3', 'persona', 'quant'), preset('x', 'persona', 'ghost')],
    [person('owner', 'Walter'), person('quant', 'Isak')],
  );
  assert.deepEqual(groups.map(g => [g.key, g.items.map(i => i.id)]), [['fibonacci', ['f1']], ['owner', ['p1']], ['quant', ['p2', 'p3']], ['other', ['x']]]);
  assert.match(groups[1].note ?? '', /fictional character/);
});

test('the owner line names the fictional character', () => {
  assert.equal(ownerLine(preset('p', 'persona', 'quant'), [person('quant', 'Isak')]), "Isak's strategy · a fictional character following quant archetype");
  assert.equal(ownerLine(preset('f', 'fibonacci'), [person('quant', 'Isak')]), '');
});
