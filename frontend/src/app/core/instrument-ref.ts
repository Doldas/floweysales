/**
 * Which instrument is selected, how it is named, and where it comes from.
 * Pure and import-free so node --test can run it. An instrument is a provider
 * plus a symbol: the same ticker on two providers or exchanges is two
 * instruments and must never be mixed up.
 */
export interface InstrumentRef {
  symbol: string;
  provider: string;
  name?: string;
  exchange?: string;
}

export const instrumentKey = (ref: Pick<InstrumentRef, 'symbol' | 'provider'>): string =>
  `${ref.provider.toUpperCase()}:${ref.symbol.toUpperCase()}`;

/** "VOLV-B.ST · Volvo AB · Stockholm (YAHOO)": enough to tell two listings apart. */
export function instrumentLabel(ref: InstrumentRef): string {
  const place = ref.exchange ? ` · ${ref.exchange}` : '';
  const name = ref.name ? ` · ${ref.name}` : '';
  return `${ref.symbol}${name}${place} (${ref.provider})`;
}

/** Reads `?symbol=…&provider=…`. A symbol without a provider is accepted; a provider alone is not. */
export function instrumentFromQuery(search: string): { symbol: string; provider: string | null } | null {
  const params = new URLSearchParams(search);
  const symbol = (params.get('symbol') ?? '').trim().toUpperCase();
  if (!symbol || symbol.length > 32 || !/^[A-Z0-9.^=\-_:]+$/.test(symbol)) return null;
  const provider = (params.get('provider') ?? '').trim().toUpperCase();
  return { symbol, provider: provider || null };
}

/** The link is the strongest wish, then what was stored, then nothing. */
export function pickInitialInstrument<T>(fromUrl: T | null, stored: T | null): T | null {
  return fromUrl ?? stored ?? null;
}

/** A response is stale when the selection moved on while it was in flight. */
export const isStaleResponse = (requested: string, current: string): boolean => requested !== current;
