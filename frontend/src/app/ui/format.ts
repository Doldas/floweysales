/** Small display helpers shared by pages. Pure functions; no Angular. */

/** "3 min ago", "2 h ago", "yesterday", or a date for older timestamps. */
export function timeAgo(value: string | number | null | undefined, now = Date.now()): string {
  if (value === null || value === undefined || value === '') return '—';
  const t = typeof value === 'number' ? value : Date.parse(value);
  if (!Number.isFinite(t) || t <= 0) return '—';
  const s = Math.round((now - t) / 1000);
  if (s < 0) return 'just now';
  if (s < 45) return 'just now';
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  if (s < 172800) return 'yesterday';
  if (s < 7 * 86400) return `${Math.round(s / 86400)} days ago`;
  return new Date(t).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

/** Human duration for milliseconds: 820 ms, 4.2 s, 3 min 10 s. */
export function duration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms)) return '—';
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)} s`;
  return `${Math.floor(ms / 60000)} min ${Math.round((ms % 60000) / 1000)} s`;
}

/** Status pill modifier for a flow run status. */
export function runTone(status: string): string {
  switch (status) {
    case 'SUCCESS': return 'is-success';
    case 'FAILED':
    case 'DEAD': return 'is-danger';
    case 'QUEUED':
    case 'RUNNING': return 'is-info';
    case 'AWAITING_APPROVAL': return 'is-warning';
    default: return '';
  }
}

/** Title-cased run status: AWAITING_APPROVAL → Awaiting approval. */
export function runLabel(status: string): string {
  const s = (status || 'unknown').toLowerCase().replace(/_/g, ' ');
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** Time-of-day greeting. */
export function greeting(date = new Date()): string {
  const h = date.getHours();
  if (h < 5) return 'Working late';
  if (h < 12) return 'Good morning';
  if (h < 18) return 'Good afternoon';
  return 'Good evening';
}

/** Readable schedule: 15 → "Every 15 min", 60 → "Hourly", 1440 → "Daily". */
export function everyLabel(minutes: number): string {
  if (minutes === 43200) return 'Monthly';
  if (minutes % 10080 === 0) return minutes === 10080 ? 'Weekly' : `Every ${minutes / 10080} weeks`;
  if (minutes % 1440 === 0) return minutes === 1440 ? 'Daily' : `Every ${minutes / 1440} days`;
  if (minutes % 60 === 0) return minutes === 60 ? 'Hourly' : `Every ${minutes / 60} h`;
  return `Every ${minutes} min`;
}

/**
 * Whole-unit money: 1234567 and "USD" → "$1,234,567". Without a usable
 * currency code it is the plain number, so a bad code never throws.
 */
export function formatMoney(value: number | null | undefined, currency = '', locale?: string): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  try {
    if (/^[A-Za-z]{3}$/.test(currency)) {
      return new Intl.NumberFormat(locale, { style: 'currency', currency: currency.toUpperCase(), maximumFractionDigits: 0 }).format(value);
    }
  } catch { /* unknown code: fall through */ }
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(value);
}
