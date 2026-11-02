/** Wording for a character's reference portfolio. Pure, tested with node --test. */
import type { ReferencePortfolio, ReferencePosition } from '../models';

export const KIND_LABEL: Record<ReferencePortfolio['kind'], string> = {
  '13f': 'SEC 13F filing', etf_daily: "Fund's published daily holdings", rules: 'Portfolio rules of the approach',
};

/** "$299.3 billion", "$15.4 billion", "$850 million". */
export function usdCompact(value: number | undefined): string {
  if (value === undefined || !Number.isFinite(value) || value <= 0) return '–';
  if (value >= 1e12) return `$${(value / 1e12).toFixed(1)} trillion`;
  if (value >= 1e9) return `$${(value / 1e9).toFixed(1)} billion`;
  return `$${Math.round(value / 1e6)} million`;
}

/** Herfindahl index on a 0-10000 scale, in words. */
export function concentrationLabel(hhi: number): string {
  if (hhi >= 1000) return 'very concentrated';
  if (hhi >= 400) return 'concentrated';
  if (hhi >= 150) return 'moderately diversified';
  return 'very diversified';
}

/** Change in shares since the previous quarter, with a sign so colour is not the only cue. */
export function changeLabel(p: ReferencePosition, kind: ReferencePortfolio['kind']): string {
  if (kind !== '13f') return '';
  if (p.shares_change_pct === undefined || p.shares_change_pct === null) return kind === '13f' ? 'new or unknown' : '';
  const v = p.shares_change_pct;
  if (Math.abs(v) < 0.05) return 'unchanged';
  return `${v > 0 ? '▲ +' : '▼ '}${v.toFixed(1)}%`;
}

export function summaryLine(p: ReferencePortfolio): string {
  if (p.kind === 'rules') return p.filer;
  const s = p.stats;
  return `${p.filer} · as of ${p.period} · ${s.positions.toLocaleString('en-US')} positions, top 5 = ${s.top5_pct.toFixed(0)}%, top 10 = ${s.top10_pct.toFixed(0)}% (${concentrationLabel(s.hhi)})`;
}

/** Days between the data's date and a reference date, for a staleness note. */
export function ageDays(period: string, today: string): number | undefined {
  const a = Date.parse(period), b = Date.parse(today);
  return Number.isFinite(a) && Number.isFinite(b) ? Math.max(0, Math.round((b - a) / 86_400_000)) : undefined;
}

export function stalenessNote(p: ReferencePortfolio, today: string): string {
  if (p.kind === 'rules') return '';
  const d = ageDays(p.period, today);
  if (d === undefined) return '';
  if (p.kind === '13f') return `Holdings are ${d} days old, and a 13F is only filed up to 45 days after its quarter ends.`;
  return d === 0 ? 'As published today.' : `As published ${d} days ago.`;
}
