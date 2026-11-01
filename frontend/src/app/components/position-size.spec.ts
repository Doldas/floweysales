// Runs with Node's built-in test runner:
//   node --test src/app/components/position-size.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sizeSummary } from './position-size.ts';
import type { SizeResponse } from '../models';

const base: SizeResponse = { symbol: 'AAPL', side: 'buy', entry: 190, equity: 100000, risk_pct: 1, atr_mult: 2, atr: 3.8, stop_source: 'atr',
  result: { quantity: 131, stop: 182.4, risk_amount: 996.2, position_pct: 24.9 } };
const money = (v: number) => `$${v.toFixed(0)}`;

test('summary names risk, stop, size and quantity', () => {
  assert.equal(sizeSummary(base, money), 'Risk 1.0% = $996 · stop 182.40 (2× ATR) · 25% of equity · quantity 131');
});

test('a given stop and caps are named', () => {
  const capped = { ...base, stop_source: 'given' as const, result: { ...base.result, capped_by: 'buying_power' as const } };
  assert.match(sizeSummary(capped, money), /^Risk 1.0% = \$996 · stop 182.40 · 25% of equity · quantity 131 · capped by buying power$/);
});

test('fractional quantities drop trailing zeros and an empty size says so', () => {
  assert.match(sizeSummary({ ...base, result: { ...base.result, quantity: 0.05 } }, money), /quantity 0\.05$/);
  assert.match(sizeSummary({ ...base, result: { ...base.result, quantity: 0 } }, money), /too small to size at this stop$/);
});
