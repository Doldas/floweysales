/**
 * Price alerts kept in this browser: the price of an instrument reaching a level, from below or from
 * above. Checked against the quote board's prices, which are provider quotes and live ticks, so an
 * alert says the provider reported the price, not that an order could have been filled there.
 */

export type AlertDirection = 'above' | 'below';

export interface PriceAlert {
  id: string;
  symbol: string;
  provider: string;
  direction: AlertDirection;
  level: number;
  created_at: number;
  /** When a price reached the level; the alert then stays as a record until it is removed. */
  triggered_at?: number;
  triggered_price?: number;
}

export const MAX_ALERTS = 50;

/** A level or price as alerts show it: two to four decimals. */
export const formatPrice = (n: number): string => n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 4 });
/** How far away, in percent, the proximity bar starts to fill. */
export const PROXIMITY_SPAN = 10;

/** The direction a new alert watches: toward the level from the price now. Null when they are equal. */
export function directionFor(level: number, price: number): AlertDirection | null {
  if (!(level > 0) || !(price > 0) || level === price) return null;
  return level > price ? 'above' : 'below';
}

export function isCrossed(a: Pick<PriceAlert, 'direction' | 'level'>, price: number): boolean {
  if (!(price > 0)) return false;
  return a.direction === 'above' ? price >= a.level : price <= a.level;
}

/** How far the price still has to move to the level, in percent of the price; positive is up. */
export function distancePercent(level: number, price: number): number | null {
  return price > 0 && level > 0 ? ((level - price) / price) * 100 : null;
}

/** How near the price is to the level: 0 at PROXIMITY_SPAN percent away or more, 1 at the level. */
export function proximity(a: Pick<PriceAlert, 'direction' | 'level'>, price: number): number {
  if (isCrossed(a, price)) return 1;
  const d = distancePercent(a.level, price);
  return d === null ? 0 : Math.max(0, 1 - Math.abs(d) / PROXIMITY_SPAN);
}

/** The instrument of an alert, keyed like the quote board's rows (boardKey) so its price can be found. */
export const alertInstrumentKey = (a: { symbol: string; provider: string }): string =>
  `${a.provider.trim().toUpperCase()}|${a.symbol.trim().toUpperCase()}`;

/** The alerts in stored data, dropping anything malformed; newest first, at most MAX_ALERTS. */
export function parseAlerts(raw: unknown): PriceAlert[] {
  if (!Array.isArray(raw)) return [];
  const out: PriceAlert[] = [];
  for (const x of raw) {
    if (!x || typeof x !== 'object') continue;
    const a = x as Record<string, unknown>;
    const ok = typeof a['id'] === 'string' && typeof a['symbol'] === 'string' && a['symbol'] && typeof a['provider'] === 'string' && a['provider']
      && (a['direction'] === 'above' || a['direction'] === 'below') && typeof a['level'] === 'number' && Number.isFinite(a['level']) && a['level'] > 0
      && typeof a['created_at'] === 'number';
    if (!ok) continue;
    const alert: PriceAlert = {
      id: a['id'] as string, symbol: a['symbol'] as string, provider: a['provider'] as string,
      direction: a['direction'] as AlertDirection, level: a['level'] as number, created_at: a['created_at'] as number,
    };
    if (typeof a['triggered_at'] === 'number') alert.triggered_at = a['triggered_at'];
    if (typeof a['triggered_price'] === 'number') alert.triggered_price = a['triggered_price'];
    out.push(alert);
  }
  return out.sort((x, y) => y.created_at - x.created_at).slice(0, MAX_ALERTS);
}

/** Marks the active alerts whose instrument's price has reached the level. */
export function checkAlerts(alerts: readonly PriceAlert[], priceOf: (a: PriceAlert) => number, now: number): { alerts: PriceAlert[]; fired: PriceAlert[] } {
  const fired: PriceAlert[] = [];
  const next = alerts.map(a => {
    if (a.triggered_at) return a;
    const price = priceOf(a);
    if (!isCrossed(a, price)) return a;
    const hit = { ...a, triggered_at: now, triggered_price: price };
    fired.push(hit);
    return hit;
  });
  return { alerts: fired.length ? next : [...alerts], fired };
}
