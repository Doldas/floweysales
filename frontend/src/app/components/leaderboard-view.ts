/**
 * Display helpers for the Markets leaderboard. Pure functions so they run
 * under node --test; the scoring itself lives in the server (leaderboard.go).
 */
import type { IndexInfo, LeaderboardBreakdown, LeaderboardMethodology, LeaderboardRow, LeaderboardSelection, RankingPreferences } from '../models';

export const FACTOR_LABELS: Record<string, string> = {
  value: 'Value', quality: 'Quality', growth: 'Growth', momentum: 'Momentum', timing: 'Entry timing', risk: 'Low risk',
};

export const FLAG_LABELS: Record<string, string> = {
  overbought: 'Overbought', below_sma200: 'Below 200-day', death_cross: 'Death cross', unprofitable: 'Unprofitable',
  small_comparison_group: 'Few peers in region', shrinking_revenue: 'Shrinking revenue', no_fundamentals: 'No fundamentals', stale_price: 'Stale price', stale_fundamentals: 'Old fundamentals',
};

/** Which flow playbook automates a strategy. */
export const STRATEGY_PLAYBOOK: Record<string, string> = {
  best: 'top10-buy-now', value: 'value-bargains', growth: 'garp-growth', momentum: 'momentum-leaders', quality: 'top10-buy-now', dip: 'quality-dip',
  owner_compounder: 'owner-compounders', teacher_defensive: 'teacher-defensive', pickerupper_garp: 'pickerupper-garp',
};

export function scoreTone(score: number | undefined): 'pos' | 'neg' | 'mid' {
  if (score === undefined) return 'mid';
  return score >= 62 ? 'pos' : score < 40 ? 'neg' : 'mid';
}

export function flagList(row: Pick<LeaderboardRow, 'flags'>): { id: string; label: string }[] {
  return (row.flags || '').split(',').map(f => f.trim()).filter(Boolean).map(id => ({ id, label: FLAG_LABELS[id] ?? id.replace(/_/g, ' ') }));
}

/** Competing explanations for a rank, from the server. */
export function alternativeList(row: Pick<LeaderboardRow, 'alternatives'>): string[] {
  return (row.alternatives || '').split(';').map(r => r.trim()).filter(Boolean);
}

export function reasonList(row: Pick<LeaderboardRow, 'reasons'>): string[] {
  return (row.reasons || '').split(';').map(r => r.trim()).filter(Boolean);
}

/** Signed percent with no decimals, or an en dash when absent. */
export function pct(v: number | undefined, signed = true): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '–';
  return `${signed && v > 0 ? '+' : ''}${v.toFixed(0)}%`;
}

export function num(v: number | undefined, digits = 1): string {
  return v === undefined || v === null || !Number.isFinite(v) ? '–' : v.toFixed(digits);
}

/** A prompt asking the agent to dig into the top of the board, figures included. */
export function leaderboardPrompt(rows: LeaderboardRow[], universe: string, strategy: string, asOf: string, top = 5): string {
  const lines = rows.slice(0, top).map(r =>
    `${r.rank}. ${r.symbol} score ${r.score} (${r.grade})${r.reasons ? ': ' + r.reasons : ''}${r.flags ? ' [flags: ' + r.flags + ']' : ''}`);
  return `My ${strategy} leaderboard for ${universe} (data as of ${asOf || 'today'}) ranks these first:\n${lines.join('\n')}\n\n` +
    'For each, check the latest quarterly report and filings: is the score backed by durable fundamentals, what is the main risk, and what would make it a better or worse buy now? Scores are percentiles within the universe, not predictions.';
}

/** One factor of the methodology panel, ready to show. */
export interface MethodFactor {
  id: string; label: string; question: string; weightPct: number;
  metrics: { label: string; note: string; direction: string; shareOfFactorPct: number }[];
}

/** The factors a strategy uses, heaviest first, with their metrics and weights; unused factors are listed apart. */
export function methodFactors(m: LeaderboardMethodology | undefined): { used: MethodFactor[]; unused: string[] } {
  if (!m?.factors) return { used: [], unused: [] };
  const used: MethodFactor[] = [];
  const unused: string[] = [];
  for (const id of m.factors) {
    const info = m.factor_info?.[id];
    const label = info?.label ?? FACTOR_LABELS[id] ?? id;
    const weight = m.weights?.[id] ?? 0;
    if (weight <= 0) { unused.push(label); continue; }
    const metrics = m.metrics?.[id] ?? [];
    const total = metrics.reduce((s, x) => s + x.weight, 0) || 1;
    used.push({
      id, label, question: info?.question ?? '', weightPct: Math.round(weight * 100),
      metrics: metrics.map(x => ({ label: x.label || x.field, note: x.note, direction: x.higher_is_better ? 'higher is better' : 'lower is better', shareOfFactorPct: Math.round((x.weight / total) * 100) })),
    });
  }
  used.sort((a, b) => b.weightPct - a.weightPct);
  return { used, unused };
}

/** The score of one row as a sum, factor by factor: "Value 82 × 22% = 18.0". */
export function scoreSum(b: LeaderboardBreakdown | undefined): string[] {
  if (!b) return [];
  return b.factors.filter(f => f.weight > 0).map(f =>
    `${FACTOR_LABELS[f.factor] ?? f.factor} ${f.score}${f.missing ? ' (no data, counted as ' + b.missing_factor_score + ')' : ''} × ${Math.round(f.weight * 100)}% = ${f.contribution.toFixed(1)}`);
}

/** A metric's value as shown in the breakdown. */
export function metricValue(v: number | boolean | string | undefined): string {
  if (v === undefined || v === null) return '–';
  if (typeof v === 'number') return Number.isInteger(v) ? String(v) : v.toFixed(2);
  return String(v);
}

/** What the response says about the list behind the ranking, in one line. */
export function coverageSummary(rows: Pick<LeaderboardRow, 'coverage_pct'>[], unranked: number): string {
  if (!rows.length) return unranked ? `None of the ${unranked} companies had enough data to rank.` : 'No companies.';
  const avg = Math.round(rows.reduce((s, r) => s + r.coverage_pct, 0) / rows.length);
  const full = rows.filter(r => r.coverage_pct >= 100).length;
  return `${rows.length} ranked${unranked ? `, ${unranked} left out for lack of data` : ''}; ${full} have data for all six factors, and the average company has ${avg}% of them.`;
}

export const SOURCE_KIND_LABEL: Record<string, string> = {
  curated: 'Hand-picked by the maintainers', composite: 'Union of reviewed lists', disclosure: 'Public disclosure', custom: 'Supplied by you', index: 'Published market index',
};

export function sourceKindLabel(kind: string | undefined): string {
  return kind ? SOURCE_KIND_LABEL[kind] ?? kind : 'Source unknown';
}

/** The pipeline from the list to the ranking, one line per step: "30 on the list", "-2 no usable data", "28 ranked". */
export function selectionLines(sel: LeaderboardSelection | undefined): string[] {
  if (!sel) return [];
  return sel.steps.map(s => `${s.count > 0 ? '' : '−'}${Math.abs(s.count)} ${s.step.toLowerCase()}`);
}

/** Counts per country or exchange as "SE 28, US 2", largest first. */
export function coverageBreakdown(counts: Record<string, number> | undefined): string {
  return Object.entries(counts ?? {}).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([k, n]) => `${k} ${n}`).join(', ');
}

function csvCell(v: unknown): string {
  const s = v === undefined || v === null ? '' : String(v);
  // A cell starting with = + - @ would be run as a formula by spreadsheets.
  const safe = /^[=+\-@\t\r]/.test(s) ? "'" + s : s;
  return /[",\n]/.test(safe) ? '"' + safe.replace(/"/g, '""') + '"' : safe;
}

/** The ranked companies with their listing, discovery source and metric sources, for export. */
export function universeCsv(rows: LeaderboardRow[]): string {
  const head = ['rank', 'symbol', 'isin', 'exchange', 'mic', 'country', 'currency', 'compared_with', 'why_on_list', 'how_matched', 'list_source', 'list_source_kind', 'list_source_url', 'list_as_of', 'provider', 'price_as_of', 'fundamentals_through', 'provider_record_url'];
  const lines = rows.map(r => [r.rank, r.symbol, r.identifier?.isin ?? 'not recorded', r.listing?.exchange, r.listing?.mic, r.listing?.country, r.currency, r.compared_with, r.discovery?.why, r.discovery?.how, r.discovery?.source, r.discovery?.kind,
    r.discovery?.url, r.discovery?.as_of, r.metrics_source?.provider, r.metrics_source?.price_as_of, r.metrics_source?.fundamentals_through, recordLink(r.metrics_source?.record_url)].map(csvCell).join(','));
  return [head.join(','), ...lines].join('\n') + '\n';
}

export const REGIONS: readonly { id: string; label: string }[] = [
  { id: 'north_america', label: 'North America' }, { id: 'south_america', label: 'South America' }, { id: 'europe', label: 'Europe' },
  { id: 'asia', label: 'Asia' }, { id: 'africa', label: 'Africa' }, { id: 'oceania', label: 'Oceania' },
];

export interface RegionCoverage { id: string; label: string; universes: string[]; countries: string[] }

/**
 * Coverage by world region, from the universes the server actually offers.
 * A region with none keeps its row (empty), so "No supported market yet" is
 * shown rather than the region being hidden or padded.
 */
export function regionCoverage(universes: readonly { label: string; region?: string; countries?: string[] }[]): RegionCoverage[] {
  return REGIONS.map(r => {
    const inRegion = universes.filter(u => u.region === r.id);
    const countries = [...new Set(inRegion.flatMap(u => u.countries ?? []))].sort();
    return { ...r, universes: inRegion.map(u => u.label), countries };
  });
}

/** The address of a safe, followable record: only http(s) links are shown. */
export function recordLink(url: string | null | undefined): string | null {
  if (!url || /[\\\s\u0000-\u001f\u007f]/.test(url)) return null;
  try {
    const parsed = new URL(url);
    return ['http:', 'https:'].includes(parsed.protocol) && parsed.hostname && !parsed.username && !parsed.password ? parsed.href : null;
  } catch {
    return null;
  }
}

export const OBJECTIVE_OPTIONS: readonly { id: string; label: string }[] = [
  { id: '', label: 'None: use the strategy as it is' },
  { id: 'preserve', label: 'Protect capital: more weight on low risk, quality and value' },
  { id: 'growth', label: 'Grow capital: more weight on growth and momentum' },
];

/** What the preferences form holds, as typed. */
export interface PreferencesForm { objective: string; maxVolatility: string; excludeUnprofitable: boolean; requireFundamentals: boolean; excludeSymbols: string }

export function preferencesToForm(p: RankingPreferences | undefined): PreferencesForm {
  return {
    objective: p?.objective === 'balanced' ? '' : p?.objective ?? '',
    maxVolatility: p?.max_volatility_pct ? String(p.max_volatility_pct) : '',
    excludeUnprofitable: !!p?.exclude_unprofitable, requireFundamentals: !!p?.require_fundamentals,
    excludeSymbols: (p?.exclude_symbols ?? []).join(', '),
  };
}

/** Turns the form into preferences for the server, or says what is wrong. The server checks the symbols again. */
export function formToPreferences(f: PreferencesForm): { prefs?: RankingPreferences; error?: string } {
  const prefs: RankingPreferences = { objective: f.objective };
  const vol = f.maxVolatility.trim();
  if (vol) {
    const n = Number(vol);
    if (!Number.isFinite(n) || n <= 0 || n > 200) return { error: 'The volatility limit is a percentage between 1 and 200, or empty for no limit.' };
    prefs.max_volatility_pct = n;
  }
  if (f.excludeUnprofitable) prefs.exclude_unprofitable = true;
  if (f.requireFundamentals) prefs.require_fundamentals = true;
  const symbols = [...new Set(f.excludeSymbols.split(/[\s,;]+/).map(s => s.trim().toUpperCase()).filter(Boolean))];
  if (symbols.length > 50) return { error: 'At most 50 symbols can be excluded.' };
  if (symbols.some(s => !/^[A-Z0-9._:/-]{1,40}$/.test(s))) return { error: 'Symbols may contain letters, digits and . - _ : / only.' };
  if (symbols.length) prefs.exclude_symbols = symbols;
  return { prefs };
}

/** True when the ranking mixes data ages, or its newest price or list review is old: the statements are then shown as a warning. */
export function dataAgeIsWarning(age: { mixed: boolean; list_stale?: boolean; newest_price_age_days?: number } | undefined): boolean {
  return !!age && (age.mixed || !!age.list_stale || (age.newest_price_age_days ?? 0) > 5);
}

/** The provenance of an index, one labeled fact per row. Missing facts say so; none is filled in. */
export function indexFacts(i: IndexInfo): { label: string; value: string; link?: string | null }[] {
  const known = (v: string | null | undefined, whenMissing: string) => (v && v.trim() ? v : whenMissing);
  return [
    { label: 'Provider', value: i.provider },
    { label: 'Methodology', value: i.methodology.summary, link: recordLink(i.methodology.url) },
    { label: 'Constituent source', value: known(i.constituent_source.name, 'Not recorded'), link: recordLink(i.constituent_source.url) },
    { label: 'As of (UTC)', value: known(i.as_of, 'No list loaded') },
    { label: 'Securities', value: i.available ? `${i.count} (index name says ${i.nominal_count})` : `None loaded (index name says ${i.nominal_count})` },
    { label: 'Exclusions', value: i.exclusions },
    { label: 'Share classes', value: i.share_class_policy },
    { label: 'Currency', value: `${i.currency}; ${i.benchmark_note}` },
    { label: 'Licensing', value: i.licensing + (i.constituent_source.license_verified ? ' You confirmed you may use it.' : i.source ? '' : ' No list has been supplied.') },
    ...(i.source ? [{ label: 'Your data', value: `${i.source.provider === 'fmp' ? 'Financial Modeling Prep' : 'CSV import'}, ${i.source.count} securities, imported ${i.source.imported_at} UTC. You confirmed: "${i.source.license_terms}"` }] : []),
    { label: 'History', value: (i.history.point_in_time ? 'Point-in-time membership. ' : 'Current members only. ') + i.history.note },
  ];
}

/** The line shown on an index button: count and date, or "unavailable". */
export function indexBadge(i: IndexInfo): string {
  return i.available ? `${i.count} · ${i.as_of ?? 'undated'}${i.stale ? ' · stale' : ''}` : 'unavailable';
}
