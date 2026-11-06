// Runs with Node's built-in test runner:
//   node --test src/app/core/strategy-montecarlo.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { TRADE_MC_DEFAULTS, closedTradeCount, tradeMonteCarloError, tradeMonteCarloHeadline, tradeMonteCarloReadiness, tradeMonteCarloRequest, tradeMonteCarloRows } from './strategy-montecarlo.ts';
import type { StrategyMonteCarloResult } from '../models';

const sim = {
  simulation: {
    model_version: 'mc-1',
    config: { method: 'trade_bootstrap', runs: 2000, trades: 40, seed: 42, drawdown_limit_pct: -20, dispersion_scale: 1 },
    observed_trades: 40, observed: { days: 40, total_return_pct: 12.34, max_drawdown_pct: -6.5 },
    terminal_return_pct: { p5: -8, p25: 3, p50: 10, p75: 18, p95: 31 }, max_drawdown_pct: { p5: -22, p25: -14, p50: -9, p75: -6, p95: -3 },
    prob_loss_pct: 12.5, prob_breach_pct: 7.25, prob_ruin_pct: 0, assumptions: [], limitations: [],
  },
} as unknown as StrategyMonteCarloResult;

test('defaults are valid and each bad setting is named', () => {
  assert.equal(tradeMonteCarloError(TRADE_MC_DEFAULTS), '');
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, method: 'block' }), /method/i);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, runs: '50' }), /Runs/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, seed: '-1' }), /Seed/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, drawdownLimit: '0' }), /Drawdown/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, extraCost: '900' }), /Extra cost/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, dispersion: '4' }), /Dispersion/);
});

test('the request carries the seed and sends the drawdown limit as a negative percentage', () => {
  const run = { symbol: 'AAPL', range: '5Y' as const, cost_bps: 10, strategy_id: 'fib_golden_pocket' };
  const req = tradeMonteCarloRequest(run, { ...TRADE_MC_DEFAULTS, seed: '7', drawdownLimit: '15', extraCost: '25' });
  assert.equal(req.seed, 7);
  assert.equal(req.drawdown_limit, -15);
  assert.equal(req.extra_cost_bps, 25);
  assert.equal(req.symbol, 'AAPL');
  const plain = tradeMonteCarloRequest(run, TRADE_MC_DEFAULTS);
  assert.equal(plain.drawdown_limit, undefined);
  assert.equal(plain.extra_cost_bps, undefined);
});

test('rows put the observed figure beside signed simulated percentiles', () => {
  const rows = tradeMonteCarloRows(sim);
  assert.equal(rows[0].label, 'Total return after 40 trades');
  assert.deepEqual([rows[0].observed, rows[0].p5, rows[0].p50, rows[0].p95], ['+12.3%', '-8.0%', '+10.0%', '+31.0%']);
  assert.equal(rows[1].observed, '-6.5%');
});

test('the headline names sample size and seed, says simulated, and skips absent probabilities', () => {
  const lines = tradeMonteCarloHeadline(sim);
  assert.match(lines[0], /2000 simulated paths of 40 trades \(trade bootstrap, seed 42, model mc-1\), drawn from 40 closed trades/);
  assert.match(lines[1], /7\.3% of them a fall of 20%/);
  assert.equal(lines.length, 2);
  const ruin = tradeMonteCarloHeadline({ simulation: { ...sim.simulation, prob_breach_pct: undefined, prob_ruin_pct: 3.5 } });
  assert.equal(ruin.length, 2);
  assert.match(ruin[1], /everything was lost/);
});

test('too few closed trades are explained before the run, and open trades are not counted', () => {
  assert.equal(tradeMonteCarloReadiness(20), '');
  assert.match(tradeMonteCarloReadiness(0), /Only 0 closed trades/);
  assert.match(tradeMonteCarloReadiness(1), /Only 1 closed trade in/);
  assert.equal(closedTradeCount([{ reason: 'stop' }, { reason: 'open' }, { reason: 'target' }]), 2);
});
