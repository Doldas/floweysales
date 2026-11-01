/** Summary of corporate events for the Financials "Upcoming" strip. Pure, tested with node --test. */
import type { CorporateEvent } from '../models';

export interface UpcomingSummary {
  nextEarnings?: CorporateEvent;
  nextDividend?: CorporateEvent;
  /** Sum of dividends per share over the trailing twelve months; absent with none on record. */
  ttmDividend?: number;
  /** TTM dividend as a share of the last price; absent without a price or dividends. */
  yieldPct?: number;
}

const DAY = 86_400_000;

/**
 * `past` are events up to today (for the trailing twelve months), `upcoming`
 * are events from today on. Dates are ISO days; `today` is one too.
 */
export function summarizeEvents(past: CorporateEvent[], upcoming: CorporateEvent[], price: number, today: string): UpcomingSummary {
  const future = [...upcoming].filter(e => e.date >= today).sort((a, b) => a.date.localeCompare(b.date));
  const out: UpcomingSummary = {
    nextEarnings: future.find(e => e.kind === 'earnings'),
    nextDividend: future.find(e => e.kind === 'dividend'),
  };
  const since = new Date(Date.parse(today) - 365 * DAY).toISOString().slice(0, 10);
  const paid = past.filter(e => e.kind === 'dividend' && e.amount !== undefined && e.date > since && e.date <= today);
  if (paid.length) {
    const total = paid.reduce((sum, e) => sum + (e.amount ?? 0), 0);
    out.ttmDividend = Math.round(total * 10000) / 10000;
    if (price > 0) out.yieldPct = Math.round((total / price) * 10000) / 100;
  }
  return out;
}

/** "in 12 days", "tomorrow", "today" for an ISO day. */
export function daysUntil(date: string, today: string): string {
  const n = Math.round((Date.parse(date) - Date.parse(today)) / DAY);
  if (n <= 0) return 'today';
  return n === 1 ? 'tomorrow' : `in ${n} days`;
}
