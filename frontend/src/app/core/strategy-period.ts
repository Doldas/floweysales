// Pure helpers for choosing the period a strategy is tested on. No imports, so `node --test` can run the spec.

export const PERIOD_PRESETS = ['1Y', '3Y', '5Y', '10Y', 'YTD', 'MAX'] as const;
export type PeriodPreset = (typeof PERIOD_PRESETS)[number];
export type PeriodChoice = PeriodPreset | 'CUSTOM';

export interface SavedPeriod { name: string; from: string; to: string }

const DATE = /^\d{4}-\d{2}-\d{2}$/;

function validDate(text: string): boolean {
  if (!DATE.test(text)) return false;
  const d = new Date(text + 'T00:00:00Z');
  return !Number.isNaN(d.getTime()) && d.toISOString().slice(0, 10) === text;
}

/** An error message for a custom period, or '' when it can be sent. Dates are UTC, YYYY-MM-DD. */
export function periodError(from: string, to: string, todayUtc: string): string {
  if (!from && !to) return 'Choose a start date, an end date, or both.';
  if (from && !validDate(from)) return 'The start date is not a valid date.';
  if (to && !validDate(to)) return 'The end date is not a valid date.';
  if (from && from < '1990-01-01') return 'The start date must be 1990-01-01 or later.';
  if (from && from > todayUtc) return 'The start date is in the future.';
  if (from && to && from >= to) return 'The start date must be before the end date.';
  return '';
}

/** The period fields of a test request. */
export function periodRequest(choice: PeriodChoice, from: string, to: string): { range?: PeriodPreset; from?: string; to?: string } {
  if (choice === 'CUSTOM') return { ...(from ? { from } : {}), ...(to ? { to } : {}) };
  return { range: choice };
}

/** Reads saved periods back from storage, dropping anything malformed. */
export function parseSavedPeriods(raw: string | null | undefined): SavedPeriod[] {
  if (!raw) return [];
  try {
    const data: unknown = JSON.parse(raw);
    if (!Array.isArray(data)) return [];
    return data
      .filter((p): p is SavedPeriod => !!p && typeof p.name === 'string' && typeof p.from === 'string' && typeof p.to === 'string')
      .filter(p => p.name.trim() !== '' && (p.from === '' || validDate(p.from)) && (p.to === '' || validDate(p.to)) && (p.from !== '' || p.to !== ''))
      .slice(0, 20)
      .map(p => ({ name: p.name.slice(0, 40), from: p.from, to: p.to }));
  } catch {
    return [];
  }
}

/** Adds or replaces a saved period by name, newest first, at most 20. */
export function withSavedPeriod(list: SavedPeriod[], period: SavedPeriod): SavedPeriod[] {
  const name = period.name.trim().slice(0, 40);
  return [{ ...period, name }, ...list.filter(p => p.name !== name)].slice(0, 20);
}

/** Text for a period in a result heading. */
export function periodText(from: string, to: string): string {
  return `${from || 'start of data'} to ${to || 'latest bar'} (UTC)`;
}
