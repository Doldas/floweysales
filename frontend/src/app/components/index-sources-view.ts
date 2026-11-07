import type { IndexInfo, IndexSourceRefusal } from '../models';

/** The statement the user must confirm before any list is stored. */
export const LICENSE_STATEMENT = 'I am allowed to use this constituent data in FloweySales for my own research, under the terms of its provider.';

/** What a form needs before it may be submitted; the server checks everything again. */
export function importProblems(f: { csv: string; asOf: string; sourceName: string; terms: string; confirmed: boolean }, todayUtc: string): string[] {
  const out: string[] = [];
  if (!f.csv.trim()) out.push('Choose a CSV file.');
  if (!/^\d{4}-\d{2}-\d{2}$/.test(f.asOf) || !Number.isFinite(Date.parse(`${f.asOf}T00:00:00Z`)) || new Date(`${f.asOf}T00:00:00Z`).toISOString().slice(0, 10) !== f.asOf) out.push('Give a real as-of date of the list (YYYY-MM-DD, UTC).');
  else if (f.asOf > todayUtc) out.push('The as-of date is in the future.');
  if (!f.sourceName.trim()) out.push('Name the source of the file.');
  if (f.terms.trim().length < 10) out.push('Write the licence or terms that let you use this data (at least 10 characters).');
  if (!f.confirmed) out.push('Confirm that you may use this data.');
  return out;
}

export function fetchProblems(f: { terms: string; confirmed: boolean; keySaved: boolean }): string[] {
  const out: string[] = [];
  if (!f.keySaved) out.push('Save your Financial Modeling Prep key first.');
  if (f.terms.trim().length < 10) out.push('Write the terms that let you use this data (at least 10 characters).');
  if (!f.confirmed) out.push('Confirm that you may use this data.');
  return out;
}

/** The reasons from a refused request, or the one message when there are none. */
export function refusalLines(err: unknown): string[] {
  const body = (err as { error?: IndexSourceRefusal } | null)?.error;
  if (body && Array.isArray(body.problems) && body.problems.length) return [body.error, ...body.problems].filter(Boolean);
  if (body && typeof body.error === 'string') return [body.error];
  return ['The request failed.'];
}

/** One line on how a user-supplied list knows (or does not know) past membership. */
export function historyLine(i: IndexInfo): string {
  const s = i.source;
  if (!s) return 'No list supplied.';
  return s.point_in_time
    ? `Past membership known from ${s.history_from ?? 'an unrecorded date'} (${s.change_records} add/remove records). Earlier dates use today's members and are labeled.`
    : "Only today's members are known. Backtests on this list are survivorship-biased.";
}

/** Which ways of supplying data this index offers, in words. */
export function sourceChoices(i: IndexInfo): string {
  return (i.sources ?? ['csv']).includes('fmp') ? 'CSV file or your own Financial Modeling Prep key' : 'CSV file only (no data API is supported for this index)';
}
