import type { QuoteSnapshot } from '../models';

export type QuoteTone = 'live' | 'delayed' | 'closed' | 'stale';

/** "20 s", "15 min", "3 h" or "2 d". */
export function ageText(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s} s`;
  if (s < 3600) return `${Math.round(s / 60)} min`;
  if (s < 86400) return `${Math.round(s / 3600)} h`;
  return `${Math.round(s / 86400)} d`;
}

/**
 * How fresh a quote is, in one word and an age. A quote the provider calls stale wins over everything,
 * then a closed market; a quote is "live" only when the provider says it is real time.
 */
export function quoteBadge(q: Pick<QuoteSnapshot, 'age_seconds' | 'is_realtime' | 'stale' | 'market_session'>): { text: string; tone: QuoteTone } {
  const age = ageText(q.age_seconds);
  if (q.stale) return { text: `stale · ${age}`, tone: 'stale' };
  if (q.market_session === 'closed') return { text: `closed · ${age}`, tone: 'closed' };
  if (q.is_realtime) return { text: 'live', tone: 'live' };
  return { text: `${age} ago`, tone: 'delayed' };
}
