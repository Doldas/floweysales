// Runs with Node's built-in test runner:
//   node --test src/app/components/portfolio-risk.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { alertText, driftRows, driftText, heatStyle, riskBars, riskTiles, ticketFor } from './portfolio-risk.ts';
import type { PortfolioAnalytics } from '../models';

const analytics: PortfolioAnalytics = {
  total_value: 10000, weights_pct: { AAPL: 70, BND: 30 }, top_weight_pct: 70, hhi: 0.58, effective_holdings: 1.72,
  volatility_pct: 18.456, beta: 1.1, max_drawdown_1y_pct: -12.3, correlation: [[1, 0.2], [0.2, 1]], symbols: ['AAPL', 'BND'],
  risk_contribution_pct: { AAPL: 92, BND: 8 }, class_weights_pct: { equity: 70, bond: 30 }, drift_pct: { equity: 10, bond: -10, cash: 0, satellite: 0 },
  suggestions: [], alerts: ['concentration'],
};

test('alerts read as sentences and unknown ids stay readable', () => {
  assert.match(alertText('concentration'), /more than 25%/);
  assert.match(alertText('highly_correlated:AAPL/MSFT'), /^AAPL and MSFT move almost together/);
  assert.equal(alertText('odd_one'), 'odd one');
});

test('tiles show absent figures as a dash', () => {
  const money = (v: number) => `$${v}`;
  const tiles = Object.fromEntries(riskTiles(analytics, money).map(t => [t.id, t.value]));
  assert.deepEqual(tiles, { total: '$10000', vol: '18.5%', beta: '1.10', dd: '-12.3%', eff: '1.7' });
  const short = Object.fromEntries(riskTiles({ ...analytics, volatility_pct: undefined, beta: undefined, max_drawdown_1y_pct: undefined }, money).map(t => [t.id, t.value]));
  assert.equal(short.vol, '–');
  assert.equal(short.beta, '–');
});

test('drift rows pair actual weights with targets and use signs, not colour alone', () => {
  const rows = driftRows(analytics);
  assert.deepEqual(rows.map(r => [r.id, r.actual, r.target]), [['equity', 70, 60], ['bond', 30, 40]]);
  assert.equal(driftText(10), '▲ +10.0 pts');
  assert.equal(driftText(-4.26), '▼ -4.3 pts');
  assert.equal(driftText(0), '0.0 pts');
  assert.equal(driftText(undefined), '–');
  assert.deepEqual(driftRows({ ...analytics, drift_pct: undefined }).map(r => r.target), [undefined, undefined]);
});

test('risk bars put the biggest risk first', () => {
  assert.deepEqual(riskBars({ ...analytics, symbols: ['BND', 'AAPL'] }).map(b => b.symbol), ['AAPL', 'BND']);
});

test('heat tint grows with correlation and is empty at zero', () => {
  assert.equal(heatStyle(0), '');
  assert.match(heatStyle(1), /55%/);
  assert.match(heatStyle(-0.5), /--fs-info/);
});

test('ticket prefill rounds down to whole shares and skips placeholders', () => {
  assert.deepEqual(ticketFor({ symbol: 'AAPL', side: 'sell', amount: 4000, reason: '' }, 190), { symbol: 'AAPL', side: 'SELL', quantity: 21 });
  assert.deepEqual(ticketFor({ symbol: 'BND', side: 'buy', amount: 950, reason: '' }, 72), { symbol: 'BND', side: 'BUY', quantity: 13 });
  assert.equal(ticketFor({ symbol: '<bond fund>', side: 'buy', amount: 4000, reason: '' }, 10), null);
  assert.equal(ticketFor({ symbol: 'AAPL', side: 'buy', amount: 50, reason: '' }, 190), null);
  assert.equal(ticketFor({ symbol: 'AAPL', side: 'buy', amount: 500, reason: '' }, undefined), null);
});
