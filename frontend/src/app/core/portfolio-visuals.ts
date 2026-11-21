/**
 * Shapes for the paper portfolio's charts: how the account is split between positions and cash, and
 * each position's unrealized result. Pure functions over the positions the server reports.
 */

export interface PositionLike { symbol: string; quantity: number; market_value: number; unrealized_pnl: number; cost_basis: number }

export interface Slice { label: string; value: number; share: number; tone: number; short?: boolean; cash?: boolean }

/** Named tones a slice takes, in order; the last one is for the rest and for cash is separate. */
export const SLICE_TONES = 6;

/**
 * The account split by gross exposure: the largest positions, the rest as "Other", and positive cash.
 * Shorts count by their absolute value and are marked.
 */
export function allocation(positions: readonly PositionLike[], cash: number, max = 5): Slice[] {
  const held = positions.filter(p => p.quantity !== 0 && Number.isFinite(p.market_value) && p.market_value !== 0)
    .map(p => ({ label: p.symbol, value: Math.abs(p.market_value), short: p.quantity < 0 }))
    .sort((a, b) => b.value - a.value);
  const top = held.slice(0, max);
  const rest = held.slice(max).reduce((sum, p) => sum + p.value, 0);
  const parts: Array<Omit<Slice, 'share' | 'tone'>> = [...top];
  if (rest > 0) parts.push({ label: 'Other', value: rest });
  if (cash > 0) parts.push({ label: 'Cash', value: cash, cash: true });
  const total = parts.reduce((sum, p) => sum + p.value, 0);
  if (!(total > 0)) return [];
  return parts.map((p, i) => ({ ...p, share: p.value / total, tone: p.cash ? -1 : Math.min(i, SLICE_TONES - 1) }));
}

/** A conic-gradient that draws the slices clockwise from the top, with hairline gaps between them. */
export function conicStops(slices: readonly Slice[], color: (s: Slice) => string, gapTurn = 0.004): string {
  let at = 0;
  const stops: string[] = [];
  for (const s of slices) {
    const end = at + s.share;
    const gap = slices.length > 1 ? Math.min(gapTurn, s.share / 3) : 0;
    stops.push(`${color(s)} ${turn(at)} ${turn(end - gap)}`, `transparent ${turn(end - gap)} ${turn(end)}`);
    at = end;
  }
  return `conic-gradient(${stops.join(', ')})`;
}

const turn = (t: number) => `${+(Math.min(1, Math.max(0, t)) * 360).toFixed(2)}deg`;

export interface PnlBar { symbol: string; pnl: number; pct: number | null; width: number }

/** Each position's unrealized result, best first, with bar widths relative to the largest result. */
export function pnlBars(positions: readonly PositionLike[]): PnlBar[] {
  const rows = positions.filter(p => p.quantity !== 0 && Number.isFinite(p.unrealized_pnl));
  const max = Math.max(0, ...rows.map(p => Math.abs(p.unrealized_pnl)));
  return rows
    .map(p => ({
      symbol: p.symbol, pnl: p.unrealized_pnl,
      pct: p.cost_basis ? (p.unrealized_pnl / Math.abs(p.cost_basis)) * 100 : null,
      width: max ? Math.max(2, (Math.abs(p.unrealized_pnl) / max) * 100) : 0,
    }))
    .sort((a, b) => b.pnl - a.pnl);
}
