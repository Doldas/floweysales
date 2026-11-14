/**
 * News for a collection (#441 phase 4): headlines from each instrument's own
 * provider with how they relate to the collection. Pure and free of imports
 * so node --test can run it.
 */

export type NewsRelevance = 'direct' | 'thematic' | 'listed';
export type NewsFilter = 'all' | NewsRelevance;

export interface NewsMatch {
  relevance: NewsRelevance;
  members: number[];
  term?: string;
  reason: string;
}

export interface CollectionNewsItem {
  title: string;
  url: string;
  source?: string;
  /** Unix seconds; absent when the provider gave no date. */
  published?: number;
  summary?: string;
  symbol?: string;
  provider?: string;
  relevance: NewsRelevance;
  matches: NewsMatch[];
}

export interface NewsCoverage {
  members: number;
  fetched: number;
  cached: number;
  failed: number;
  no_source: number;
  skipped: number;
}

export interface CollectionNews {
  collection_id: number;
  items: CollectionNewsItem[];
  coverage: NewsCoverage;
  news_providers: string[];
  retrieved_at: string;
}

export const NEWS_FILTERS: readonly NewsFilter[] = ['all', 'direct', 'thematic', 'listed'];

export const RELEVANCE_LABELS: Record<NewsFilter, string> = {
  all: 'All',
  direct: 'Direct',
  thematic: 'Thematic',
  listed: 'Listed',
};

/** What each relevance means, for the legend under the filter. */
export const RELEVANCE_HELP: Record<NewsRelevance, string> = {
  direct: 'names an instrument in the collection',
  thematic: 'mentions a theme or industry of one',
  listed: 'only filed under one by its provider',
};

/** Items that pass the filter. */
export function filterNews(items: readonly CollectionNewsItem[], f: NewsFilter): CollectionNewsItem[] {
  return f === 'all' ? [...items] : items.filter(i => i.relevance === f);
}

/** How many items each filter shows. */
export function relevanceCounts(items: readonly CollectionNewsItem[]): Record<NewsFilter, number> {
  const out: Record<NewsFilter, number> = { all: items.length, direct: 0, thematic: 0, listed: 0 };
  for (const i of items) out[i.relevance]++;
  return out;
}

/** The link only when it is a web address; anything else is shown as text. */
export function safeNewsUrl(url: string): string {
  return /^https?:\/\//i.test(url) ? url : '';
}

/** The reasons of one item, strongest first, as the server ordered them. */
export function newsReasons(item: CollectionNewsItem): string[] {
  return item.matches.map(m => m.reason);
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** One line on where the headlines came from and what is missing. */
export function newsCoverageText(news: CollectionNews | null): string {
  if (!news) return '';
  const c = news.coverage;
  if (!c.members) return 'No instruments yet.';
  const asked = c.fetched + c.cached;
  const parts = [`Headlines from ${asked} of ${plural(c.members, 'instrument', 'instruments')}`];
  if (c.no_source) parts.push(`${c.no_source} on a provider that offers no news`);
  if (c.failed) parts.push(`${c.failed} could not be reached`);
  if (c.skipped) parts.push(`${c.skipped} not asked (at most 25 per load)`);
  return parts.join(', ') + '.';
}
