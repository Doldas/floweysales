/**
 * Instrument intelligence (#441 phase 2): the wire types of /api/instruments
 * and /api/collections/{id}/intelligence, and the pure helpers the
 * collection page uses. Import-free so node --test can run it.
 *
 * A profile is what the instrument's own provider states; FloweySales never
 * fills it from another provider. A classification value always says where it
 * came from: the user's override, the provider's profile or what was stored
 * with the member. Unmapped provider wording stays visible as `raw`.
 */

export interface InstrumentKey {
  provider: string;
  symbol: string;
}

export interface Term {
  id: string;
  label: string;
}

export interface Taxonomy {
  asset_classes: Term[];
  sectors: Term[];
  regions: Term[];
  limits: { themes: number; theme_length: number; industry_length: number };
}

export interface ProvenanceFact {
  kind: string;
  status: 'known' | 'partial' | 'unknown';
  provider?: string;
  url?: string;
  as_of?: string;
  retrieved_at?: string;
  note?: string;
}

export interface InstrumentProfile {
  symbol: string;
  kind: string;
  name?: string;
  description?: string;
  isin?: string;
  exchange?: string;
  mic?: string;
  currency?: string;
  country?: string;
  website?: string;
  sector?: string;
  industry?: string;
  company?: { employees?: number; market_cap?: number; shares_outstanding?: number; ipo_date?: string; headquarters?: string };
  fund?: {
    issuer?: string; category?: string; asset_class?: string; benchmark?: string; expense_ratio?: number; total_assets?: number;
    inception_date?: string; top_holdings?: { symbol?: string; name?: string; weight?: number }[];
  };
  commodity?: { underlying?: string; unit?: string; contract_size?: number; expiry?: string };
  crypto?: { network?: string; categories?: string[]; circulating_supply?: number; max_supply?: number; genesis_date?: string };
  as_of?: string;
  source_url?: string;
  provider?: string;
}

export type ProfileStatus = 'ok' | 'not_found' | 'no_source' | 'unavailable' | 'missing';

export interface ProfileResult {
  key: InstrumentKey;
  status: ProfileStatus;
  profile?: InstrumentProfile;
  retrieved_at?: string;
  expires_at?: string;
  stale: boolean;
  error?: string;
  provenance?: ProvenanceFact;
}

export type FactSource = 'user' | 'profile' | 'member';

export interface Fact {
  value?: string;
  source?: FactSource;
  provider?: string;
  raw?: string;
}

export interface Classification {
  asset_class: Fact;
  sector: Fact;
  industry: Fact;
  country: Fact;
  region: Fact;
  themes: string[];
}

export interface Override extends InstrumentKey {
  asset_class?: string;
  sector?: string;
  industry?: string;
  country?: string;
  themes: string[];
  updated_at?: string;
}

export interface ProfileView {
  profile: ProfileResult;
  classification: Classification;
  override: Override | null;
}

export type LinkKind = 'same_listing' | 'same_security' | 'same_ticker' | 'different_instruments';

export interface Link {
  kind: LinkKind;
  isin?: string;
  symbol?: string;
  members: number[];
  reason: string;
}

export interface MemberIntelligence {
  member_id: number;
  key: InstrumentKey;
  name?: string;
  profile_status: ProfileStatus;
  stale: boolean;
  retrieved_at?: string;
  provenance?: ProvenanceFact;
  classification: Classification;
  overridden: boolean;
  weight?: number;
}

export interface ServerCompositionRow {
  id: string;
  count: number;
  weight?: number;
}

export type TaxonomyDimension = 'asset_class' | 'sector' | 'region' | 'theme';

export interface Intelligence {
  collection_id: number;
  kind: string;
  members: MemberIntelligence[];
  coverage: Record<'ok' | 'not_found' | 'missing' | 'no_source' | 'stale' | 'overridden', number>;
  links: Link[];
  composition: Record<TaxonomyDimension, ServerCompositionRow[]>;
  profile_providers: string[];
}

export interface RefreshResult {
  fetched: number;
  not_found: number;
  failed: number;
  no_source: number;
  fresh: number;
  remaining: number;
}

export const TAXONOMY_DIMENSIONS: TaxonomyDimension[] = ['asset_class', 'sector', 'region', 'theme'];

export const TAXONOMY_LABELS: Record<TaxonomyDimension, string> = {
  asset_class: 'Asset class',
  sector: 'Sector',
  region: 'Region',
  theme: 'Theme',
};

export const SOURCE_LABELS: Record<FactSource, string> = {
  user: 'Your classification',
  profile: 'Provider profile',
  member: 'Stored with the instrument',
};

export const LINK_LABELS: Record<LinkKind, string> = {
  same_listing: 'Same listing',
  same_security: 'Same security, other venue',
  same_ticker: 'Same ticker, unconfirmed',
  different_instruments: 'Different instruments',
};

export const STATUS_LABELS: Record<ProfileStatus, string> = {
  ok: 'Profile',
  not_found: 'Not found',
  no_source: 'No profile source',
  unavailable: 'Unavailable',
  missing: 'Not fetched',
};

/** Builds a lookup of ids to labels from the taxonomy. */
export function labelMap(t: Taxonomy | null): Record<TaxonomyDimension, Map<string, string>> {
  const m = (list: Term[] | undefined) => new Map((list ?? []).map(x => [x.id, x.label]));
  return { asset_class: m(t?.asset_classes), sector: m(t?.sectors), region: m(t?.regions), theme: new Map() };
}

export interface TaxonomyRow {
  /** Taxonomy id or theme; "" collects the unclassified members. */
  key: string;
  label: string;
  count: number;
  weight?: number;
  /** Share of the bar: of the instruments (watch, themes) or of 100 % (model). */
  pct: number;
}

export interface TaxonomyComposition {
  mode: 'count' | 'weight';
  rows: TaxonomyRow[];
  /** Members with a value in this dimension. */
  classified: number;
  total: number;
  /** Model only, not for themes: 100 minus the summed weights. */
  unallocated?: number;
}

const round = (n: number) => Math.round(n * 100) / 100;

/**
 * Turns the server's composition rows into bars. Watch collections count;
 * model collections show weights. Themes always count: one instrument may
 * have several, so their weights would add up to more than the whole.
 */
export function taxonomyComposition(kind: string, intel: Intelligence | null, d: TaxonomyDimension, labels: Record<TaxonomyDimension, Map<string, string>>): TaxonomyComposition {
  const total = intel?.members.length ?? 0;
  const rows: TaxonomyRow[] = (intel?.composition[d] ?? []).map(r => ({
    key: r.id,
    label: r.id ? labels[d].get(r.id) ?? r.id : 'Unclassified',
    count: r.count,
    ...(r.weight !== undefined && r.weight !== null ? { weight: round(r.weight) } : {}),
    pct: 0,
  }));
  const classified = d === 'theme'
    ? (intel?.members.filter(m => m.classification.themes.length).length ?? 0)
    : total - (rows.find(r => !r.key)?.count ?? 0);
  if (kind === 'model' && d !== 'theme') {
    let weighted = 0;
    for (const r of rows) { r.pct = r.weight ?? 0; weighted += r.weight ?? 0; }
    rows.sort((a, b) => (b.weight ?? 0) - (a.weight ?? 0) || b.count - a.count || unclassifiedLast(a, b));
    return { mode: 'weight', rows, classified, total, unallocated: round(Math.max(0, 100 - weighted)) };
  }
  for (const r of rows) { r.pct = total ? round((r.count / total) * 100) : 0; delete r.weight; }
  return { mode: 'count', rows, classified, total };
}

function unclassifiedLast(a: TaxonomyRow, b: TaxonomyRow): number {
  if (!a.key !== !b.key) return a.key ? -1 : 1;
  return a.label.localeCompare(b.label, 'en');
}

/** The coverage line under the profile summary. */
export function profileCoverageText(intel: Intelligence): string {
  const c = intel.coverage;
  const total = intel.members.length;
  if (!total) return 'No instruments yet.';
  const parts = [`${c.ok} of ${total} instruments have a provider profile`];
  if (c.missing) parts.push(`${c.missing} not fetched yet`);
  if (c.not_found) parts.push(`${c.not_found} not found by their provider`);
  if (c.no_source) parts.push(`${c.no_source} on a provider that offers no profiles`);
  let text = parts.join(', ') + '.';
  if (c.stale) text += ` ${c.stale} ${c.stale === 1 ? 'profile is past its' : 'profiles are past their'} refresh date.`;
  if (c.overridden) text += ` You classified ${c.overridden} yourself.`;
  return text;
}

/** Whether "Fetch profiles" has anything to do. */
export function canFetchProfiles(intel: Intelligence | null): boolean {
  return !!intel && (intel.coverage.missing > 0 || intel.coverage.stale > 0);
}

/** One line for the result of a refresh call. */
export function refreshSummary(r: RefreshResult): string {
  const parts: string[] = [];
  if (r.fetched) parts.push(`${r.fetched} fetched`);
  if (r.not_found) parts.push(`${r.not_found} not found`);
  if (r.failed) parts.push(`${r.failed} failed`);
  if (!parts.length) parts.push('Nothing new to fetch');
  let text = parts.join(', ') + '.';
  if (r.remaining) text += ` ${r.remaining} more to go; fetch again to continue.`;
  return text;
}

/** Display text of a fact: the label of a taxonomy id, or the provider's unmapped wording. */
export function factText(f: Fact, labels?: Map<string, string>): string {
  if (f.value) return labels?.get(f.value) ?? f.value;
  if (f.raw) return `${f.raw} (unmapped)`;
  return '—';
}

/** "Provider profile (FINNHUB)", the tooltip of a classified value. */
export function factSourceText(f: Fact): string {
  if (!f.source) return 'Unknown';
  return f.provider && f.source === 'profile' ? `${SOURCE_LABELS[f.source]} (${f.provider})` : SOURCE_LABELS[f.source];
}

/** The override form's fields. Themes are comma-separated text. */
export interface OverrideDraft {
  asset_class: string;
  sector: string;
  industry: string;
  country: string;
  themes: string;
}

export function draftFrom(o: Override | null): OverrideDraft {
  return { asset_class: o?.asset_class ?? '', sector: o?.sector ?? '', industry: o?.industry ?? '', country: o?.country ?? '', themes: (o?.themes ?? []).join(', ') };
}

/** Why the override cannot be saved, or "". Mirrors internal/instruments.NormalizeOverride. */
export function overrideProblem(d: OverrideDraft, limits: Taxonomy['limits']): string {
  const country = d.country.trim();
  if (country && !/^[A-Za-z]{2}$/.test(country)) return 'Country must be a 2-letter ISO code, such as SE or US.';
  if (d.industry.trim().length > limits.industry_length) return `Industry is at most ${limits.industry_length} characters.`;
  const themes = themeList(d.themes);
  if (themes.length > limits.themes) return `At most ${limits.themes} themes.`;
  if (themes.some(t => t.length > limits.theme_length)) return `A theme is at most ${limits.theme_length} characters.`;
  return '';
}

function themeList(text: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of text.split(',')) {
    const t = raw.trim().replace(/\s+/g, ' ');
    if (!t || seen.has(t.toLowerCase())) continue;
    seen.add(t.toLowerCase());
    out.push(t);
  }
  return out;
}

/** The PUT body; empty fields keep the profile's value. */
export function overrideBody(key: InstrumentKey, d: OverrideDraft): Override {
  return {
    provider: key.provider, symbol: key.symbol,
    asset_class: d.asset_class, sector: d.sector, industry: d.industry.trim(), country: d.country.trim().toUpperCase(),
    themes: themeList(d.themes),
  };
}

/** "NVDA (FINNHUB) and NVDA (YAHOO)": the members a link joins, by symbol and provider. */
export function linkMembers(link: Link, members: MemberIntelligence[]): string {
  const byId = new Map(members.map(m => [m.member_id, m]));
  const names = link.members.map(id => byId.get(id)).filter((m): m is MemberIntelligence => !!m).map(m => `${m.key.symbol} (${m.key.provider})`);
  if (names.length <= 2) return names.join(' and ');
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

/** Compact number for profile figures: 1.2T, 340B, 5.6M. */
export function compactNumber(n: number | undefined): string {
  if (n === undefined || n === null || !Number.isFinite(n)) return '—';
  const abs = Math.abs(n);
  for (const [div, unit] of [[1e12, 'T'], [1e9, 'B'], [1e6, 'M'], [1e3, 'K']] as const) {
    if (abs >= div) return `${round(n / div)}${unit}`;
  }
  return String(round(n));
}
