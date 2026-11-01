/**
 * Display helpers for the portfolio Risk tab. Pure functions so they run
 * under node --test; the figures themselves come from the server
 * (internal/portfolio).
 */
import type { PortfolioAnalytics, PortfolioSuggestion } from '../models';

export const CLASS_LABELS: Record<string, string> = { equity: 'Core equity', bond: 'Bonds', cash: 'Cash', satellite: 'Satellite' };

/** Plain-language text for a server alert id; unknown ids stay readable. */
export function alertText(alert: string): string {
  if (alert.startsWith('highly_correlated:')) {
    const [a, b] = alert.slice('highly_correlated:'.length).split('/');
    return `${a} and ${b} move almost together (correlation above 0.85), so owning both adds little diversification.`;
  }
  switch (alert) {
    case 'concentration': return 'One holding is more than 25% of the portfolio.';
    case 'low_diversification': return 'Fewer than five holdings\' worth of diversification: a few positions dominate.';
    case 'deep_drawdown': return 'These holdings would have fallen more than 20% from a peak over the past year.';
    case 'satellite_over_cap': return 'The high-risk sleeve is above the cap your plan allows.';
    case 'short_history': return 'Not enough shared price history for volatility, beta and correlation.';
    default: return alert.replace(/_/g, ' ');
  }
}

export interface Tile { id: string; label: string; value: string; hint: string }

function num(v: number | undefined, digits: number, unit = ''): string {
  return v === undefined || v === null || !Number.isFinite(v) ? '–' : `${v.toFixed(digits)}${unit}`;
}

export function riskTiles(a: PortfolioAnalytics, money: (v: number) => string): Tile[] {
  return [
    { id: 'total', label: 'Total value', value: money(a.total_value), hint: 'Holdings plus free cash' },
    { id: 'vol', label: 'Volatility', value: num(a.volatility_pct, 1, '%'), hint: 'Annualized, from a year of daily returns at current weights' },
    { id: 'beta', label: 'Beta', value: num(a.beta, 2), hint: 'Sensitivity to the benchmark; 1 moves with it' },
    { id: 'dd', label: 'Worst fall (1Y)', value: num(a.max_drawdown_1y_pct, 1, '%'), hint: 'Largest peak-to-trough drop if today\'s weights were held for the past year' },
    { id: 'eff', label: 'Effective holdings', value: num(a.effective_holdings, 1), hint: '1 divided by the sum of squared weights: how many equal positions it behaves like' },
  ];
}

export interface DriftRow { id: string; label: string; actual: number; drift?: number; target?: number }

/** Class weights next to their drift from the plan; the target is actual minus drift. */
export function driftRows(a: PortfolioAnalytics): DriftRow[] {
  return Object.keys(CLASS_LABELS).map(id => {
    const actual = a.class_weights_pct[id] ?? 0;
    const drift = a.drift_pct?.[id];
    return { id, label: CLASS_LABELS[id], actual, drift, target: drift === undefined ? undefined : Math.round((actual - drift) * 100) / 100 };
  }).filter(r => r.actual > 0 || (r.target ?? 0) > 0);
}

export function driftText(drift: number | undefined): string {
  if (drift === undefined) return '–';
  const v = Math.round(drift * 10) / 10;
  return `${v > 0 ? '▲ +' : v < 0 ? '▼ ' : ''}${v.toFixed(1)} pts`;
}

/** Holdings by share of the portfolio's risk, largest first. */
export function riskBars(a: PortfolioAnalytics): { symbol: string; risk: number; weight: number }[] {
  return a.symbols.map(symbol => ({ symbol, risk: a.risk_contribution_pct[symbol] ?? 0, weight: a.weights_pct[symbol] ?? 0 }))
    .sort((x, y) => y.risk - x.risk);
}

/** Background tint for a correlation cell; the value is always printed too. */
export function heatStyle(corr: number): string {
  const strength = Math.round(Math.min(1, Math.max(0, Math.abs(corr))) * 55);
  return strength === 0 ? '' : `color-mix(in srgb, var(${corr > 0 ? '--fs-warn' : '--fs-info'}) ${strength}%, transparent)`;
}

export interface TicketPrefill { symbol: string; side: 'BUY' | 'SELL'; quantity: number }

/**
 * What to put in the order ticket for a suggestion: whole shares that fit the
 * amount at the last price. Placeholders such as "<bond fund>" and amounts
 * too small for one share give null; nothing is submitted either way.
 */
export function ticketFor(s: PortfolioSuggestion, price: number | undefined): TicketPrefill | null {
  if (s.symbol.startsWith('<') || !price || !(price > 0)) return null;
  const quantity = Math.floor(s.amount / price);
  return quantity >= 1 ? { symbol: s.symbol, side: s.side === 'sell' ? 'SELL' : 'BUY', quantity } : null;
}
