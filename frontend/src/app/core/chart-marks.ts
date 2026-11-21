/**
 * Horizontal price lines on the chart: the user's alert levels and the average entry of an open paper
 * position, so the chart shows where a trade stands and what is being waited for.
 */

export type ChartMarkTone = 'alert' | 'hit' | 'entry';

export interface ChartMark { price: number; label: string; tone: ChartMarkTone }

interface AlertLike { level: number; direction: 'above' | 'below'; triggered_at?: number }
interface PositionLike { quantity: number; avg_entry: number }

/** The marks for one instrument: its entry first, then alert levels from the highest down. */
export function chartMarks(alerts: readonly AlertLike[], position: PositionLike | null | undefined): ChartMark[] {
  const out: ChartMark[] = [];
  if (position && position.quantity !== 0 && position.avg_entry > 0) {
    out.push({ price: position.avg_entry, label: position.quantity > 0 ? 'Avg entry' : 'Avg short entry', tone: 'entry' });
  }
  const levels = alerts.filter(a => a.level > 0).sort((a, b) => b.level - a.level);
  for (const a of levels) {
    out.push({ price: a.level, label: a.triggered_at ? 'Alert hit' : a.direction === 'above' ? 'Alert above' : 'Alert below', tone: a.triggered_at ? 'hit' : 'alert' });
  }
  return out;
}

/** Where a mark sits against the visible price range: inside it, or off the top or bottom. */
export function markPlacement(price: number, min: number, max: number): 'in' | 'above' | 'below' {
  if (price > max) return 'above';
  if (price < min) return 'below';
  return 'in';
}
