import type { QuoteSnapshot } from '../models';

/** Most instruments the shared quote board fetches; each one is a quote request a minute. */
export const MAX_BOARD = 20;

export interface InstrumentLike { symbol: string; provider: string; name?: string }

/** An instrument on the quote board: tracked, starred, or both. */
export interface BoardInstrument { symbol: string; provider: string; name?: string; tracked: boolean; favorite: boolean }

export type PriceMove = 'up' | 'down' | '';

/** The latest quote of one instrument and which way its price last moved. */
export interface BoardEntry { quote?: QuoteSnapshot; failed?: boolean; move: PriceMove; moveSeq: number }

export const boardKey = (a: Pick<InstrumentLike, 'symbol' | 'provider'>): string =>
  `${a.provider.trim().toUpperCase()}|${a.symbol.trim().toUpperCase()}`;

/** Tracked instruments first, then starred ones, each once, at most `max`. */
export function boardInstruments(tracked: readonly InstrumentLike[], favorites: readonly InstrumentLike[], max = MAX_BOARD): BoardInstrument[] {
  const out = new Map<string, BoardInstrument>();
  const add = (a: InstrumentLike, flag: 'tracked' | 'favorite') => {
    if (!a.symbol?.trim() || !a.provider?.trim()) return;
    const key = boardKey(a);
    const seen = out.get(key);
    if (seen) { seen[flag] = true; if (!seen.name && a.name) seen.name = a.name; return; }
    if (out.size >= max) return;
    const entry: BoardInstrument = { symbol: a.symbol.trim().toUpperCase(), provider: a.provider.trim().toUpperCase(), tracked: flag === 'tracked', favorite: flag === 'favorite' };
    if (a.name) entry.name = a.name;
    out.set(key, entry);
  };
  tracked.forEach(a => add(a, 'tracked'));
  favorites.forEach(a => add(a, 'favorite'));
  return [...out.values()];
}

/** Whether two board lists hold the same instruments in the same order. */
export const sameBoard = (a: readonly BoardInstrument[], b: readonly BoardInstrument[]): boolean =>
  a.length === b.length && a.every((x, i) => boardKey(x) === boardKey(b[i]) && x.tracked === b[i].tracked && x.favorite === b[i].favorite);

export function priceMove(previous: number | undefined, next: number): PriceMove {
  if (!previous || !Number.isFinite(previous) || !Number.isFinite(next) || previous === next) return '';
  return next > previous ? 'up' : 'down';
}

/** The entry after a new quote: the move and its counter change only when the price did. */
export function nextEntry(prev: BoardEntry | undefined, quote: QuoteSnapshot): BoardEntry {
  const move = priceMove(prev?.quote?.price, quote.price);
  return { quote, move: move || (prev?.move ?? ''), moveSeq: (prev?.moveSeq ?? 0) + (move ? 1 : 0) };
}

/**
 * The day's change in percent. With a newer price (a live tick) it is measured from the same
 * previous close; without the day's figures there is no change to show (null), never a guess.
 */
export function dayPercent(quote: QuoteSnapshot | undefined, price?: number): number | null {
  const day = quote?.day;
  if (!day) return null;
  if (price && price > 0 && day.previous_close > 0 && price !== quote.price) return (price / day.previous_close - 1) * 100;
  return Number.isFinite(day.change_percent) ? day.change_percent : null;
}

/** Colour step of a day change: -3 (deep red) … 3 (deep green); 0 is flat; null when unknown. */
export function heatLevel(pct: number | null): number | null {
  if (pct === null || !Number.isFinite(pct)) return null;
  const a = Math.abs(pct);
  const step = a < 0.1 ? 0 : a < 1 ? 1 : a < 2.5 ? 2 : 3;
  return pct < 0 ? -step : step;
}

export interface Mover<T> { item: T; pct: number }

/** The biggest gainers and losers of the day, `n` of each, strongest first. */
export function topMovers<T>(items: readonly T[], pctOf: (item: T) => number | null, n = 3): { gainers: Mover<T>[]; losers: Mover<T>[] } {
  const known = items.map(item => ({ item, pct: pctOf(item) })).filter((m): m is Mover<T> => m.pct !== null && Number.isFinite(m.pct));
  const gainers = known.filter(m => m.pct > 0).sort((a, b) => b.pct - a.pct).slice(0, n);
  const losers = known.filter(m => m.pct < 0).sort((a, b) => a.pct - b.pct).slice(0, n);
  return { gainers, losers };
}

/** Where a price sits in the day's range, 0 (low) … 1 (high); null without a range. */
export function rangePosition(price: number, low: number, high: number): number | null {
  if (!(high > low) || !Number.isFinite(price)) return null;
  return Math.min(1, Math.max(0, (price - low) / (high - low)));
}

/** "+1.84%" / "−0.92%" with a real minus sign; an em dash when unknown. */
export function signedPercent(pct: number | null, digits = 2): string {
  if (pct === null || !Number.isFinite(pct)) return '—';
  const fixed = Math.abs(pct).toFixed(digits);
  if (Number(fixed) === 0) return `${(0).toFixed(digits)}%`;
  return `${pct > 0 ? '+' : '−'}${fixed}%`;
}

/** Alternating class names restart the flash animation on every new move. */
export function flashClass(entry: Pick<BoardEntry, 'move' | 'moveSeq'> | undefined): string {
  if (!entry?.move || !entry.moveSeq) return '';
  return `fs-flash-${entry.move}-${entry.moveSeq % 2 ? 'a' : 'b'}`;
}
