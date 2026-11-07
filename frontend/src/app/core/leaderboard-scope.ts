import type { LeaderboardScope, ScopeRequest } from '../models';

/**
 * The scope controls of the Leaderboard (#209): which regions, countries,
 * exchanges and currencies to rank, and whether percentiles are taken within
 * each region or across all companies. Pure functions, so they run under
 * node --test.
 */

/** Region ids the server understands, with the labels shown. */
export const SCOPE_REGIONS: readonly { id: string; label: string }[] = [
  { id: 'north_america', label: 'North America' }, { id: 'south_america', label: 'South America' }, { id: 'europe', label: 'Europe' },
  { id: 'asia', label: 'Asia' }, { id: 'africa', label: 'Africa' }, { id: 'oceania', label: 'Oceania' },
];

/** What the scope form holds, as typed. */
export interface ScopeForm { regions: string[]; countries: string; exchanges: string; currencies: string; compare: '' | 'region' | 'all' }

export const EMPTY_SCOPE: ScopeForm = { regions: [], countries: '', exchanges: '', currencies: '', compare: '' };

/** Splits "se, dk" into ["SE", "DK"], dropping empties and duplicates. */
export function parseCodes(raw: string): string[] {
  return [...new Set(raw.split(/[\s,;]+/).map(s => s.trim().toUpperCase()).filter(Boolean))];
}

/** Checks the codes the way the server will, so a typo is caught before a slow request. */
export function scopeError(f: ScopeForm): string {
  for (const [name, raw] of [['country', f.countries], ['exchange', f.exchanges], ['currency', f.currencies]] as const) {
    const bad = parseCodes(raw).find(c => !/^[A-Z0-9]{2,4}$/.test(c));
    if (bad) return `"${bad}" is not a valid ${name} code. Use 2 to 4 letters or digits, for example SE, XSTO or SEK.`;
  }
  return '';
}

export function formToScope(f: ScopeForm): ScopeRequest {
  const scope: ScopeRequest = {};
  if (f.regions.length) scope.regions = [...f.regions].sort();
  const countries = parseCodes(f.countries), exchanges = parseCodes(f.exchanges), currencies = parseCodes(f.currencies);
  if (countries.length) scope.countries = countries;
  if (exchanges.length) scope.exchanges = exchanges;
  if (currencies.length) scope.currencies = currencies;
  if (f.compare) scope.compare = f.compare;
  return scope;
}

/** The query parameters of a scope; empty parts are left out. */
export function scopeParams(scope: ScopeRequest): Record<string, string> {
  const p: Record<string, string> = {};
  if (scope.regions?.length) p['regions'] = scope.regions.join(',');
  if (scope.countries?.length) p['countries'] = scope.countries.join(',');
  if (scope.exchanges?.length) p['exchanges'] = scope.exchanges.join(',');
  if (scope.currencies?.length) p['currencies'] = scope.currencies.join(',');
  if (scope.compare) p['compare'] = scope.compare;
  return p;
}

export function scopeIsActive(f: ScopeForm): boolean {
  return Object.keys(formToScope({ ...f, compare: '' })).length > 0;
}

/** One line per region of the coverage the server measured, newest data first stated. */
export function coverageLines(scope: LeaderboardScope | undefined): string[] {
  if (!scope) return [];
  return scope.coverage.map(c => {
    const dates = !c.price_newest ? 'no prices' : c.price_newest === c.price_oldest ? `prices ${c.price_newest}` : `prices ${c.price_oldest} to ${c.price_newest}`;
    return `${c.label || 'Region not known'}: ${c.ranked} of ${c.listed} ranked; ${dates} (UTC); ${c.currencies.join(', ') || 'no currency'}; ${c.exchanges.join(', ') || 'exchange not recorded'}; ${c.providers.join(', ')}`;
  });
}

/** Regions with no reviewed list, which the scope must say are unsupported instead of leaving out. */
export function unsupportedRegions(covered: readonly string[]): string[] {
  return SCOPE_REGIONS.filter(r => !covered.includes(r.id)).map(r => r.label);
}
