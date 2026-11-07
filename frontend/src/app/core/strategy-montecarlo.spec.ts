// Runs with Node's built-in test runner:
//   node --test src/app/core/strategy-montecarlo.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { TRADE_MC_DEFAULTS, closedTradeCount, tradeMonteCarloConcentration, tradeMonteCarloCsv, tradeMonteCarloError, tradeMonteCarloFileName, tradeMonteCarloJson, tradeMonteCarloStresses, tradeMonteCarloHeadline, tradeMonteCarloReadiness, tradeMonteCarloRequest, tradeMonteCarloRows } from './strategy-montecarlo.ts';
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

test('the new stresses are validated and sent only when switched on', () => {
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, delayBars: '9' }), /Fill delay/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, delayBars: '1.5' }), /Fill delay/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, regimeShare: '150' }), /Stressed share/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, regimeShare: '20', regimeScale: '9' }), /Stressed spread/);
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, positionScale: '5' }), /Position size/);
  assert.equal(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, delayBars: '2', regimeShare: '25', positionScale: '1.5' }), '');
  const run = { symbol: 'AAPL', range: '5Y' as const, cost_bps: 10 };
  const plain = tradeMonteCarloRequest(run, TRADE_MC_DEFAULTS);
  assert.equal(plain.delay_bars, undefined);
  assert.equal(plain.regime_share_pct, undefined);
  assert.equal(plain.position_scale, undefined);
  const on = tradeMonteCarloRequest(run, { ...TRADE_MC_DEFAULTS, delayBars: '2', regimeShare: '25', regimeScale: '3', positionScale: '2' });
  assert.deepEqual([on.delay_bars, on.regime_share_pct, on.regime_scale, on.position_scale], [2, 25, 3, 2]);
});

const stressed = {
  ...sim, symbol: 'AAPL', strategy: 'Dip buyer, v2', provider: 'YAHOO', range: '5Y', from: '2021-01-04', to: '2026-01-02', data_start: '2021-01-04', data_end: '2026-01-02',
  generated_at: '2026-11-06T10:00:00Z', cost_bps: 10,
  simulation: { ...sim.simulation, assumptions: ['Assumes, with "quotes"'], limitations: ['Not a forecast'],
    config: { ...sim.simulation.config, delay_bars: 2, regime_share_pct: 30, regime_scale: 3, position_scale: 1.5, extra_cost_bps_per_trade: 25 },
    concentration: { best_trades_removed: 3, return_without_best_pct: -4.2, largest_loss_pct: -3.1 } },
} as unknown as StrategyMonteCarloResult;

test('chosen stresses and observed concentration are said in words and marked as not predictions', () => {
  assert.deepEqual(tradeMonteCarloStresses(stressed.simulation.config), ['25 bps extra cost per trade', '2-bar fill delay', '30% of trades in a stretch with spread x3', 'positions x1.5 the size']);
  assert.match(tradeMonteCarloHeadline(stressed).at(-1)!, /Stresses you chose \(not observed\)/);
  assert.equal(tradeMonteCarloStresses(sim.simulation.config).length, 0);
  assert.match(tradeMonteCarloConcentration(stressed), /Observed, not simulated: without its best 3 trades the replay returned -4\.2% \(it returned \+12\.3%\).*-3\.1%/);
  assert.equal(tradeMonteCarloConcentration(sim), '');
});

test('the export carries provenance in UTC, the model, the seed and the assumptions, and escapes CSV', () => {
  const csv = tradeMonteCarloCsv(stressed);
  for (const want of ['strategy,"Dip buyer, v2"', 'provider,YAHOO', 'data_start_utc,2021-01-04', 'generated_at_utc,2026-11-06T10:00:00Z', 'model_version,mc-1', 'seed,42', 'delay_bars,2',
    'total_return_pct,12.34,-8.00,3.00,10.00,18.00,31.00', 'drawdown_limit_breach,7.25', 'return_without_best_pct,-4.20', 'assumption,"Assumes, with ""quotes"""', 'limitation,Not a forecast']) {
    assert.ok(csv.includes(want), `missing ${want}`);
  }
  assert.match(csv, /not predictions and not guaranteed/);
  const json = JSON.parse(tradeMonteCarloJson(stressed));
  assert.equal(json.simulation.config.seed, 42);
  assert.equal(json.simulation.model_version, 'mc-1');
  assert.equal(tradeMonteCarloFileName(stressed, 'csv'), 'monte-carlo-dip-buyer-v2-aapl-seed42.csv');
});

test('Monte Carlo seed cannot silently round unsafe large integers', () => {
  assert.match(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, seed: '9007199254740993' }), /Seed must be a safe whole number/);
  assert.equal(tradeMonteCarloError({ ...TRADE_MC_DEFAULTS, seed: '42' }), '');
});
