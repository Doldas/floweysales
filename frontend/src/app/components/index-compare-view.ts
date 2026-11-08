import type { IndexBrief, IndexComparison, IndexInfo, IndexPair, IndexVerification } from '../models';

/** Indices the user may compare: those with a list of their own. */
export function comparable(indices: IndexInfo[]): IndexInfo[] {
  return indices.filter(i => i.available);
}

/** Whether the selection can be sent (2 to 6 different indices). */
export function selectionProblem(ids: string[]): string {
  const n = new Set(ids).size;
  if (n < 2) return 'Choose at least two indices.';
  if (n > 6) return 'Choose at most six indices.';
  return '';
}

export function levelLabel(v: IndexVerification | undefined): string {
  if (!v) return 'No list';
  return v.level === 'verified' ? 'Verified' : 'Approximate';
}

/** The checks that did not pass, in words, for an approximate list. */
export function failedChecks(v: IndexVerification | undefined): string[] {
  return (v?.checks ?? []).filter(c => !c.ok).map(c => c.detail);
}

export function percent(share: number): string {
  return `${Math.round(share * 100)}%`;
}

/** The name of an index in the comparison, falling back to its id. */
export function nameOf(c: IndexComparison, id: string): string {
  return c.indices.find(i => i.id === id)?.name ?? id;
}

/** One sentence per pair: how many securities are shared, and how much of their union that is. */
export function pairLine(c: IndexComparison, p: IndexPair): string {
  const a = nameOf(c, p.a), b = nameOf(c, p.b);
  return p.shared === 0 ? `${a} and ${b} share no securities.` : `${a} and ${b} share ${p.shared} securities (${percent(p.jaccard)} of their union).`;
}

export function benchmarkLine(i: IndexBrief): string {
  return `${i.benchmark_symbol} (${i.benchmark_total_return ? 'total return' : 'price index, no dividends'})`;
}
