/**
 * The facts a price chart must state where it is drawn: provider, bar size,
 * currency, window and the time of the newest bar (UTC), plus what the
 * overlaid studies are. Pure and import-free so node --test can run it.
 * Missing values are omitted or named, never shown as zero.
 */
export interface ChartMetaInput {
  provider?: string;
  interval?: string;
  currency?: string;
  range?: string;
  /** Unix seconds of the bars, oldest first. */
  times?: readonly number[];
  emaFast?: number;
  emaSlow?: number;
  /** Labels of the studies switched on. */
  studies?: readonly string[];
}

export interface MetaFact { label: string; value: string }

const INTERVALS: Record<string, string> = { '1m': '1-minute', '5m': '5-minute', '15m': '15-minute', '30m': '30-minute', '60m': 'hourly', '1h': 'hourly', '1d': 'daily', '1wk': 'weekly', '1mo': 'monthly' };

export function intervalLabel(interval: string | undefined): string {
  if (!interval) return 'unknown bar size';
  return INTERVALS[interval.toLowerCase()] ?? interval;
}

/** Facts in reading order. `formatUtc` renders an instant, for example formatUtcDateTime. */
export function chartMeta(input: ChartMetaInput, formatUtc: (ms: number) => string): MetaFact[] {
  const facts: MetaFact[] = [];
  facts.push({ label: 'Source', value: input.provider || 'unknown provider' });
  facts.push({ label: 'Bars', value: `${input.interval ? intervalLabel(input.interval) + ' bars' : 'bars of unknown size'}, ${input.range || 'range unknown'}` });
  facts.push({ label: 'Units', value: input.currency ? `prices in ${input.currency}` : 'currency not reported' });
  const last = input.times?.length ? input.times[input.times.length - 1] : NaN;
  facts.push({ label: 'Newest bar', value: Number.isFinite(last) && last > 0 ? formatUtc(last * 1000) : 'no bars loaded' });
  if (input.studies?.length) {
    const ema = input.emaFast && input.emaSlow ? ` (EMA periods ${input.emaFast} and ${input.emaSlow})` : '';
    facts.push({ label: 'Studies', value: `${input.studies.join(', ')}${ema}. Calculated here from the bars above, not reported by the provider.` });
  }
  return facts;
}

/** One line for the folded header. */
export function chartSummary(input: ChartMetaInput): string {
  const parts = [input.range, input.provider, input.currency].filter(Boolean);
  return parts.length ? parts.join(' · ') : 'No data loaded';
}
