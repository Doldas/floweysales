/**
 * Market Collections (#441): the wire types of /api/collections and the pure
 * helpers the page uses. Import-free so node --test can run it.
 *
 * A watch collection has no weights: its composition is a count of
 * instruments and must never be shown as an allocation. A model collection
 * may carry hypothetical weights; whatever is not weighted is shown as
 * unallocated, never spread over the members.
 */

export type CollectionKind = 'watch' | 'model' | 'linked';
export type ArchiveFilter = 'exclude' | 'include' | 'only';
export type CollectionSort = 'name' | 'updated' | 'created' | 'members';

export interface Collection {
  id: number;
  name: string;
  description?: string;
  kind: CollectionKind;
  category?: string;
  tags?: string[];
  archived: boolean;
  member_count: number;
  created_at: string;
  updated_at: string;
}

export interface CollectionInstrument {
  provider: string;
  symbol: string;
  name?: string;
  type?: string;
  exchange?: string;
  mic?: string;
  isin?: string;
  currency?: string;
  country?: string;
}

export interface CollectionMember {
  id: number;
  collection_id: number;
  instrument: CollectionInstrument;
  note?: string;
  tags?: string[];
  weight?: number;
  added_at: string;
}

export interface MemberPage {
  members: CollectionMember[];
  total: number;
  limit: number;
  offset: number;
}

export interface CollectionDetail {
  collection: Collection;
  members: MemberPage;
}

export interface CollectionFilter {
  q?: string;
  kind?: CollectionKind | '';
  category?: string;
  tag?: string;
  archived?: ArchiveFilter;
  sort?: CollectionSort;
}

export interface ImportResult {
  name: string;
  status: 'created' | 'renamed' | 'skipped';
  id?: number;
  saved_as?: string;
  members: number;
}

/** Same limits as internal/collections, so the form refuses what the server would. */
export const COLLECTION_LIMITS = { name: 80, description: 2000, category: 60, note: 1000, tags: 20, tag: 40 } as const;

export const KIND_LABELS: Record<CollectionKind, string> = {
  watch: 'Watch collection',
  model: 'Model portfolio',
  linked: 'Linked portfolio view',
};

/** Kinds a user can create today; linked views wait for a portfolio source with permissions. */
export const CREATABLE_KINDS: CollectionKind[] = ['watch', 'model'];

/** Query parameters for GET /api/collections, leaving out defaults. */
export function filterParams(f: CollectionFilter): Record<string, string> {
  const out: Record<string, string> = {};
  const q = (f.q ?? '').trim();
  if (q) out['q'] = q;
  if (f.kind) out['kind'] = f.kind;
  if (f.category?.trim()) out['category'] = f.category.trim();
  if (f.tag?.trim()) out['tag'] = f.tag.trim();
  if (f.archived && f.archived !== 'exclude') out['archived'] = f.archived;
  if (f.sort && f.sort !== 'name') out['sort'] = f.sort;
  return out;
}

/** "AI, chips , ai" → ["AI", "chips"]: trimmed, empty ones dropped, duplicates ignoring case removed. */
export function parseTags(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split(',')) {
    const tag = raw.trim();
    if (!tag || seen.has(tag.toLowerCase())) continue;
    seen.add(tag.toLowerCase());
    out.push(tag);
  }
  return out;
}

/** Categories and tags in use, sorted, for the filter pickers. */
export function facets(list: Collection[]): { categories: string[]; tags: string[] } {
  const categories = new Map<string, string>();
  const tags = new Map<string, string>();
  for (const c of list) {
    if (c.category) categories.set(c.category.toLowerCase(), c.category);
    for (const t of c.tags ?? []) tags.set(t.toLowerCase(), t);
  }
  const sorted = (m: Map<string, string>) => [...m.values()].sort((a, b) => a.localeCompare(b, 'en'));
  return { categories: sorted(categories), tags: sorted(tags) };
}

/** A search hit or the selected instrument as a member instrument. Only what is known is sent. */
export function instrumentFrom(provider: string, hit: { symbol: string; name?: string; exchange?: string; type?: string }): CollectionInstrument {
  const out: CollectionInstrument = { provider: provider.trim().toUpperCase(), symbol: hit.symbol.trim().toUpperCase() };
  if (hit.name?.trim()) out.name = hit.name.trim();
  if (hit.exchange?.trim()) out.exchange = hit.exchange.trim();
  const type = normalizeType(hit.type);
  if (type) out.type = type;
  return out;
}

const TYPES: Record<string, string> = {
  equity: 'equity', stock: 'equity', 'common stock': 'equity', share: 'equity',
  etf: 'etf', fund: 'fund', 'mutual fund': 'fund', mutualfund: 'fund',
  index: 'index', crypto: 'crypto', cryptocurrency: 'crypto',
  commodity: 'commodity', future: 'future', futures: 'future',
  forex: 'currency', currency: 'currency', fx: 'currency', bond: 'bond',
};

/** Maps a provider's free-text type onto the server's set; an unknown one is left out rather than guessed. */
export function normalizeType(type: string | undefined): string | undefined {
  return type ? TYPES[type.trim().toLowerCase()] : undefined;
}

export type Dimension = 'type' | 'exchange' | 'currency' | 'country' | 'provider';

export const DIMENSION_LABELS: Record<Dimension, string> = {
  type: 'Asset type',
  exchange: 'Exchange',
  currency: 'Currency',
  country: 'Country',
  provider: 'Data provider',
};

const TYPE_LABELS: Record<string, string> = {
  equity: 'Equity', etf: 'ETF', fund: 'Fund', index: 'Index', crypto: 'Crypto', commodity: 'Commodity',
  future: 'Future', currency: 'Currency', bond: 'Bond', other: 'Other',
};

export interface CompositionRow {
  /** The value, or "" for members where it is unknown. */
  key: string;
  label: string;
  /** Instruments with this value. */
  count: number;
  /** Model collections only: summed weight in percent. */
  weight?: number;
  /** Share of the bar: of the instruments (watch) or of 100 % (model). */
  pct: number;
}

export interface Composition {
  mode: 'count' | 'weight';
  rows: CompositionRow[];
  /** Members where the value is known. */
  known: number;
  total: number;
  /** Model only: 100 minus the summed weights, never negative. */
  unallocated?: number;
  /** Model only: members without a weight. */
  unweighted?: number;
}

function valueOf(m: CollectionMember, d: Dimension): string {
  return (m.instrument[d] ?? '').trim();
}

function labelOf(d: Dimension, key: string): string {
  if (!key) return 'Unknown';
  return d === 'type' ? TYPE_LABELS[key] ?? key : key;
}

const round = (n: number) => Math.round(n * 100) / 100;

/**
 * Composition of a collection along one dimension. Watch collections count
 * instruments; model collections sum weights, and an unweighted member adds
 * to its row's count but not its weight. Unknown values form their own row
 * so coverage is visible instead of silently dropped.
 */
export function composition(kind: CollectionKind, members: CollectionMember[], d: Dimension): Composition {
  const byKey = new Map<string, CompositionRow>();
  let known = 0;
  let weighted = 0;
  let unweighted = 0;
  for (const m of members) {
    const key = d === 'country' || d === 'currency' || d === 'provider' ? valueOf(m, d).toUpperCase() : valueOf(m, d);
    if (key) known++;
    const row = byKey.get(key) ?? { key, label: labelOf(d, key), count: 0, pct: 0, ...(kind === 'model' ? { weight: 0 } : {}) };
    row.count++;
    if (kind === 'model') {
      if (m.weight !== undefined && m.weight !== null) {
        row.weight = round((row.weight ?? 0) + m.weight);
        weighted += m.weight;
      } else {
        unweighted++;
      }
    }
    byKey.set(key, row);
  }
  const rows = [...byKey.values()];
  const total = members.length;
  if (kind === 'model') {
    for (const r of rows) r.pct = r.weight ?? 0;
    rows.sort((a, b) => (b.weight ?? 0) - (a.weight ?? 0) || b.count - a.count || unknownLast(a, b));
    return { mode: 'weight', rows, known, total, unallocated: round(Math.max(0, 100 - weighted)), unweighted };
  }
  for (const r of rows) r.pct = total ? round((r.count / total) * 100) : 0;
  rows.sort((a, b) => b.count - a.count || unknownLast(a, b));
  return { mode: 'count', rows, known, total };
}

function unknownLast(a: CompositionRow, b: CompositionRow): number {
  if (!a.key !== !b.key) return a.key ? -1 : 1;
  return a.label.localeCompare(b.label, 'en');
}

/** "Type known for 3 of 5 instruments", the coverage line under a composition. */
export function coverageText(c: Composition, d: Dimension): string {
  if (!c.total) return 'No instruments yet.';
  const what = DIMENSION_LABELS[d].toLowerCase();
  if (c.known === c.total) return `${DIMENSION_LABELS[d]} known for all ${c.total} instruments.`;
  return `${DIMENSION_LABELS[d]} known for ${c.known} of ${c.total} instruments; the rest show as Unknown because no ${what} was stored.`;
}

/** Summed weight of a model collection's members, and what is left. */
export function weightSummary(members: CollectionMember[]): { total: number; remaining: number; weighted: number } {
  let total = 0;
  let weighted = 0;
  for (const m of members) {
    if (m.weight === undefined || m.weight === null) continue;
    total += m.weight;
    weighted++;
  }
  return { total: round(total), remaining: round(Math.max(0, 100 - total)), weighted };
}

/** Why a weight cannot be saved, or "" when it can. `others` is the sum of the other members' weights. */
export function weightProblem(value: number | null, others: number): string {
  if (value === null) return '';
  if (!Number.isFinite(value) || value <= 0) return 'A weight must be above 0.';
  if (value > 100) return 'A weight cannot be above 100%.';
  if (others + value > 100 + 1e-6) return `Together with the other weights this is ${round(others + value)}%; the total cannot exceed 100%.`;
  return '';
}

/** Server file name for an export, e.g. "collections-2026-11-14.json" (UTC). */
export function exportFileName(now: Date): string {
  return `collections-${now.toISOString().slice(0, 10)}.json`;
}

/** One line per import result, e.g. "Technology saved as Technology (2) with 3 instruments". */
export function importSummary(results: ImportResult[]): string[] {
  return results.map(r => {
    const n = `${r.members} instrument${r.members === 1 ? '' : 's'}`;
    if (r.status === 'skipped') return `${r.name} skipped: the name is taken.`;
    if (r.status === 'renamed') return `${r.name} saved as ${r.saved_as} with ${n}.`;
    return `${r.name} created with ${n}.`;
  });
}
