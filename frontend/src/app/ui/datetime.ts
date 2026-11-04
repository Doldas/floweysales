/**
 * Dates, times and numbers as the app shows them: always UTC and en-US,
 * whatever the browser's time zone or language. Pure and free of imports so
 * node --test can run it. Use these instead of toLocale*String(); a CI check
 * (scripts/check-utc-formatting.sh) rejects the browser-dependent forms.
 */
export type DateInput = string | number | Date | null | undefined;

const LOCALE = 'en-US';
const base = { timeZone: 'UTC', hourCycle: 'h23' } as const;
const fmtDateTime = new Intl.DateTimeFormat(LOCALE, { ...base, year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
const fmtDateTimeSeconds = new Intl.DateTimeFormat(LOCALE, { ...base, year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
const fmtDate = new Intl.DateTimeFormat(LOCALE, { ...base, year: 'numeric', month: 'short', day: 'numeric' });
const fmtMonthDay = new Intl.DateTimeFormat(LOCALE, { ...base, month: 'short', day: 'numeric' });
const fmtTime = new Intl.DateTimeFormat(LOCALE, { ...base, hour: '2-digit', minute: '2-digit' });
const fmtLongDay = new Intl.DateTimeFormat(LOCALE, { ...base, weekday: 'long', month: 'long', day: 'numeric' });

/** Milliseconds since the epoch, or NaN for a missing or unusable value. */
export function toMillis(value: DateInput): number {
  if (value === null || value === undefined || value === '') return NaN;
  const t = value instanceof Date ? value.getTime() : typeof value === 'number' ? value : Date.parse(value);
  return Number.isFinite(t) && t > 0 ? t : NaN;
}

function format(f: Intl.DateTimeFormat, value: DateInput, suffix = ''): string {
  const t = toMillis(value);
  return Number.isNaN(t) ? '—' : f.format(t) + suffix;
}

/** "Nov 3, 2026, 14:32 UTC" */
export const formatUtcDateTime = (value: DateInput): string => format(fmtDateTime, value, ' UTC');
/** "Nov 3, 2026, 14:32:05 UTC" */
export const formatUtcDateTimeSeconds = (value: DateInput): string => format(fmtDateTimeSeconds, value, ' UTC');
/** "Nov 3, 2026" */
export const formatUtcDate = (value: DateInput): string => format(fmtDate, value);
/** "Nov 3" */
export const formatUtcMonthDay = (value: DateInput): string => format(fmtMonthDay, value);
/** "14:32" (the caller says it is UTC, for example in an axis title) */
export const formatUtcTime = (value: DateInput): string => format(fmtTime, value);
/** "Tuesday, November 3" */
export const formatUtcLongDay = (value: DateInput): string => format(fmtLongDay, value);

/** The calendar year of an instant in UTC. */
export const utcYear = (value: DateInput): number => {
  const t = toMillis(value);
  return Number.isNaN(t) ? NaN : new Date(t).getUTCFullYear();
};

/** Today's date in UTC as YYYY-MM-DD. */
export const todayUtc = (now: DateInput = Date.now()): string => new Date(toMillis(now)).toISOString().slice(0, 10);

/** A number grouped and rounded the en-US way, whatever the browser's language. */
export function formatNumber(value: number | null | undefined, options: Intl.NumberFormatOptions = {}): string {
  return value === null || value === undefined || !Number.isFinite(value) ? '—' : new Intl.NumberFormat(LOCALE, options).format(value);
}
