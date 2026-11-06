/**
 * Display helpers for the walk-forward fit of a rule strategy. Pure functions so they run under
 * node --test; the fit itself runs on the server (internal/strategy/fit.go).
 */
import type { FitFoldResult, FitMode, FitObjective, FitParameter, FitRequest, FitResult, StrategyRunRequest } from '../models';

export const FIT_MAX_PARAMS = 2;
export const FIT_FOLD_CHOICES = [2, 3, 4, 5, 6, 8] as const;

export const FIT_MODES: { id: FitMode; label: string; hint: string }[] = [
  { id: 'rolling', label: 'Rolling', hint: 'Choose the numbers on the window just before each test window.' },
  { id: 'anchored', label: 'Anchored', hint: 'Choose the numbers on everything before each test window.' },
];

export const FIT_OBJECTIVES: { id: FitObjective; label: string }[] = [
  { id: 'return', label: 'Total return' },
  { id: 'sharpe', label: 'Sharpe ratio' },
  { id: 'expectancy', label: 'Expectancy per trade' },
];

/** Adds a number to the selection, or removes it when already chosen; at most FIT_MAX_PARAMS stay chosen. */
export function toggleParam(selected: readonly string[], path: string): string[] {
  if (selected.includes(path)) return selected.filter(p => p !== path);
  return selected.length >= FIT_MAX_PARAMS ? [...selected] : [...selected, path];
}

/** Why a fit cannot be run, or '' when it can. */
export function fitProblem(selected: readonly string[], available: readonly FitParameter[] | undefined): string {
  if (!available?.length) return 'Run a test first. The numbers a fit can change come from the strategy that was tested.';
  if (!selected.length) return 'Choose one or two numbers to fit.';
  if (selected.length > FIT_MAX_PARAMS) return `Choose at most ${FIT_MAX_PARAMS} numbers.`;
  if (selected.some(p => !available.some(a => a.path === p))) return 'A chosen number is no longer part of this strategy. Run the test again.';
  return '';
}

export function fitRequest(base: StrategyRunRequest, p: { params: string[]; folds: number; mode: FitMode; objective: FitObjective }): FitRequest {
  return { ...base, params: p.params, folds: p.folds, mode: p.mode, objective: p.objective };
}

const pct = (v: number): string => `${v > 0 ? '+' : ''}${v.toFixed(1)}%`;
const num = (v: number): string => String(Math.round(v * 1000) / 1000);

/** "EMA period 13, Time stop (bars) 40" for the values a fold chose. */
export function chosenText(f: Pick<FitFoldResult, 'chosen'>): string {
  return (f.chosen ?? []).map(c => `${c.label} = ${num(c.value)}`).join('; ');
}

export interface FitRow {
  fold: number; train: string; test: string; chosen: string;
  trainReturn: string; testReturn: string; unfitted: string; buyHold: string; testTrades: string; skipped: string;
}

/** Table rows. A skipped fold shows n/a for every figure, never 0. */
export function fitRows(folds: readonly FitFoldResult[] | undefined): FitRow[] {
  return (folds ?? []).map(f => {
    const skipped = f.skipped ?? '';
    const n = (v: string): string => (skipped ? 'n/a' : v);
    return {
      fold: f.index,
      train: `${f.train_from} to ${f.train_to}`,
      test: `${f.test_from} to ${f.test_to}`,
      chosen: skipped ? 'none' : chosenText(f),
      trainReturn: skipped && !f.chosen?.length ? 'n/a' : pct(f.train_return_pct),
      testReturn: n(pct(f.test_return_pct)),
      unfitted: n(pct(f.unfitted_return_pct)),
      buyHold: n(pct(f.buy_hold_return_pct)),
      testTrades: n(String(f.test_trades)),
      skipped,
    };
  });
}

/** Plain sentences that state what the fit showed, from the recorded figures only. */
export function fitHeadline(r: Pick<FitResult, 'out_of_sample' | 'folds' | 'combos_per_fold'>): string[] {
  const s = r.out_of_sample;
  if (!s) return ['No fold could be used, so there is no out-of-sample result. See each fold for why.'];
  const lines = [
    `${s.folds_used} of ${r.folds.length} folds gave a result. Each tried ${r.combos_per_fold} combinations on its training window and kept the best.`,
    `On the windows the numbers were not chosen on, the fitted rules returned ${pct(s.return_pct)}, your original numbers ${pct(s.unfitted_return_pct)} and buy and hold ${pct(s.buy_hold_return_pct)} (compounded, ${s.trades} trades).`,
    `On average the training windows returned ${pct(s.mean_train_return_pct)} and the test windows ${pct(s.mean_test_return_pct)}.`,
  ];
  if (s.efficiency !== undefined) lines.push(`The test windows kept ${(s.efficiency * 100).toFixed(0)}% of the training return.`);
  return lines;
}
