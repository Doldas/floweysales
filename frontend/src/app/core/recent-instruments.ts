/**
 * The instruments a user looked at last, newest first. Pure and import-free so
 * node --test can run it. Identity is provider plus symbol, so the same ticker
 * on two providers is two entries.
 */
export interface RecentInstrument {
  symbol: string;
  provider: string;
  name?: string;
  exchange?: string;
}

export const MAX_RECENT = 8;

const key = (r: Pick<RecentInstrument, 'symbol' | 'provider'>): string => `${r.provider.toUpperCase()}:${r.symbol.toUpperCase()}`;

/** Puts `ref` first. A repeat moves up and keeps a name it already had when the new one has none. */
export function addRecent(list: readonly RecentInstrument[], ref: RecentInstrument, max = MAX_RECENT): RecentInstrument[] {
  const symbol = ref.symbol.trim().toUpperCase();
  const provider = ref.provider.trim().toUpperCase();
  if (!symbol || !provider) return [...list];
  const previous = list.find(r => key(r) === key({ symbol, provider }));
  const entry: RecentInstrument = { symbol, provider };
  const name = ref.name || previous?.name;
  const exchange = ref.exchange || previous?.exchange;
  if (name) entry.name = name;
  if (exchange) entry.exchange = exchange;
  return [entry, ...list.filter(r => key(r) !== key(entry))].slice(0, max);
}

/** Reads stored JSON and drops anything that is not a well-formed entry. Never throws. */
export function parseRecent(raw: string | null, max = MAX_RECENT): RecentInstrument[] {
  if (!raw) return [];
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(data)) return [];
  const out: RecentInstrument[] = [];
  for (const item of data) {
    if (!item || typeof item !== 'object') continue;
    const { symbol, provider, name, exchange } = item as Record<string, unknown>;
    if (typeof symbol !== 'string' || typeof provider !== 'string' || !symbol || !provider) continue;
    if (symbol.length > 32 || !/^[A-Za-z0-9.^=\-_:]+$/.test(symbol)) continue;
    const entry: RecentInstrument = { symbol: symbol.toUpperCase(), provider: provider.toUpperCase() };
    if (typeof name === 'string' && name) entry.name = name.slice(0, 120);
    if (typeof exchange === 'string' && exchange) entry.exchange = exchange.slice(0, 40);
    if (!out.some(r => key(r) === key(entry))) out.push(entry);
  }
  return out.slice(0, max);
}
