/** Wording for the Command Center "Today" card. Pure, tested with node --test. */
import type { TodayBriefing } from '../models';

export const REGIME_LABELS: Record<string, string> = { risk_on: 'Risk on', caution: 'Caution', risk_off: 'Risk off' };

export function regimeIcon(regime: string): string {
  return regime === 'risk_off' ? 'bi-exclamation-octagon' : regime === 'caution' ? 'bi-exclamation-triangle' : 'bi-check-circle';
}

/** Signed money change with an arrow, so direction never depends on colour. */
export function signedChange(value: number | undefined, pct: number | undefined): string {
  if (value === undefined || !Number.isFinite(value)) return '–';
  const arrow = value > 0 ? '▲ +' : value < 0 ? '▼ ' : '';
  const amount = Math.round(value).toLocaleString('en-US');
  return `${arrow}${amount}${pct !== undefined && Number.isFinite(pct) ? ` (${pct > 0 ? '+' : ''}${pct.toFixed(1)}%)` : ''}`;
}

export function hasAnything(t: TodayBriefing | null): boolean {
  return !!t && !!(t.phase || t.macro || t.portfolio || (t.worth_a_look && Object.keys(t.worth_a_look).length));
}

/** Plain-language names for the server's portfolio alert ids. */
export function alertName(alert: string): string {
  return alert.replace(/^highly_correlated:/, 'correlated holdings ').replace(/_/g, ' ');
}

/**
 * A prompt that hands the agent exactly these figures and asks it to stay
 * inside them, so "Brief me" cannot add numbers of its own.
 */
export function todayPrompt(t: TodayBriefing): string {
  const lines: string[] = [];
  if (t.phase) lines.push(`Wealth plan: ${t.phase.name} phase, ${t.phase.probability_goal_pct.toFixed(0)}% ${t.phase.probability_label ?? 'probability of the goal'}.${t.phase.next_checklist_item ? ` Next checklist item: ${t.phase.next_checklist_item}` : ''}`);
  if (t.macro) lines.push(`Macro regime: ${REGIME_LABELS[t.macro.regime] ?? t.macro.regime}${t.macro.reasons ? ` (${t.macro.reasons})` : ''}${t.macro.as_of ? `, as of ${t.macro.as_of}` : ''}.`);
  const p = t.portfolio;
  if (p) {
    const parts = [`equity ${Math.round(p.equity)}`];
    if (p.day_change !== undefined) parts.push(`day change ${Math.round(p.day_change)}${p.day_change_pct !== undefined ? ` (${p.day_change_pct.toFixed(1)}%)` : ''}`);
    if (p.drawdown_from_peak_pct !== undefined) parts.push(`${p.drawdown_from_peak_pct.toFixed(1)}% from its peak`);
    if (p.top_alert) parts.push(`top alert: ${alertName(p.top_alert)}`);
    lines.push(`Paper portfolio: ${parts.join(', ')}.`);
  }
  const w = t.worth_a_look;
  if (w?.leaderboard) lines.push(`Leaderboard (${w.leaderboard.universe}, ${w.leaderboard.strategy}): ${w.leaderboard.symbol} ranks first with score ${w.leaderboard.score}${w.leaderboard.reasons ? ` — ${w.leaderboard.reasons}` : ''}.`);
  if (w?.insider) lines.push(`Largest insider buy on the watchlist: ${w.insider.symbol}, ${Math.round(w.insider.largest_buy_value)}${w.insider.largest_buyer ? ` by ${w.insider.largest_buyer}` : ''}${w.insider.latest ? `, latest ${w.insider.latest}` : ''}${w.insider.cluster_buy ? ', a cluster of buyers' : ''}.`);
  if (w?.earnings) lines.push(`Next earnings on the watchlist: ${w.earnings.symbol} on ${w.earnings.date}${w.earnings.confirmed ? '' : ' (estimated)'}.`);
  return 'Brief me for today in under 150 words. Use only the figures below, name the date they are as of, and do not add numbers of your own. Say what deserves attention first and why. This is research, not advice.\n\n' + lines.join('\n');
}
