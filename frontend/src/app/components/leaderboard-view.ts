/**
 * Display helpers for the Markets leaderboard. Pure functions so they run
 * under node --test; the scoring itself lives in the server (leaderboard.go).
 */
import type { LeaderboardRow } from '../models';

export const FACTOR_LABELS: Record<string, string> = {
  value: 'Value', quality: 'Quality', growth: 'Growth', momentum: 'Momentum', timing: 'Entry timing', risk: 'Low risk',
};

export const FLAG_LABELS: Record<string, string> = {
  overbought: 'Overbought', below_sma200: 'Below 200-day', death_cross: 'Death cross', unprofitable: 'Unprofitable',
  shrinking_revenue: 'Shrinking revenue', no_fundamentals: 'No fundamentals', stale_price: 'Stale price',
};

/** Which flow playbook automates a strategy. */
export const STRATEGY_PLAYBOOK: Record<string, string> = {
  best: 'top10-buy-now', value: 'value-bargains', growth: 'garp-growth', momentum: 'momentum-leaders', quality: 'top10-buy-now', dip: 'quality-dip',
};

export function scoreTone(score: number | undefined): 'pos' | 'neg' | 'mid' {
  if (score === undefined) return 'mid';
  return score >= 62 ? 'pos' : score < 40 ? 'neg' : 'mid';
}

export function flagList(row: Pick<LeaderboardRow, 'flags'>): { id: string; label: string }[] {
  return (row.flags || '').split(',').map(f => f.trim()).filter(Boolean).map(id => ({ id, label: FLAG_LABELS[id] ?? id.replace(/_/g, ' ') }));
}

export function reasonList(row: Pick<LeaderboardRow, 'reasons'>): string[] {
  return (row.reasons || '').split(';').map(r => r.trim()).filter(Boolean);
}

/** Signed percent with no decimals, or an en dash when absent. */
export function pct(v: number | undefined, signed = true): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '–';
  return `${signed && v > 0 ? '+' : ''}${v.toFixed(0)}%`;
}

export function num(v: number | undefined, digits = 1): string {
  return v === undefined || v === null || !Number.isFinite(v) ? '–' : v.toFixed(digits);
}

/** A prompt asking the agent to dig into the top of the board, figures included. */
export function leaderboardPrompt(rows: LeaderboardRow[], universe: string, strategy: string, asOf: string, top = 5): string {
  const lines = rows.slice(0, top).map(r =>
    `${r.rank}. ${r.symbol} score ${r.score} (${r.grade})${r.reasons ? ': ' + r.reasons : ''}${r.flags ? ' [flags: ' + r.flags + ']' : ''}`);
  return `My ${strategy} leaderboard for ${universe} (data as of ${asOf || 'today'}) ranks these first:\n${lines.join('\n')}\n\n` +
    'For each, check the latest quarterly report and filings: is the score backed by durable fundamentals, what is the main risk, and what would make it a better or worse buy now? Scores are percentiles within the universe, not predictions.';
}
