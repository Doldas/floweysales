/**
 * Wording and geometry for the Strategies page. Pure functions so they run
 * under node --test; the rules themselves run on the server (internal/strategy).
 */
import type {
  Persona, StrategyComparison, StrategyCondition, StrategyDef, StrategyLevel, StrategyOperand, StrategyOperandType, StrategyRunResult, StrategyTrade,
} from '../models';

export const FIB_RATIOS = [0.236, 0.382, 0.5, 0.618, 0.65, 0.786, 0.886, 1, 1.272, 1.414, 1.618, 2, 2.618];
export const PRICE_FIELDS = ['close', 'open', 'high', 'low', 'volume'] as const;

export const OPERAND_TYPES: { type: StrategyOperandType; label: string; group: string }[] = [
  { type: 'price', label: 'Price', group: 'Price' },
  { type: 'const', label: 'Number', group: 'Price' },
  { type: 'ema', label: 'Exponential average (EMA)', group: 'Indicators' },
  { type: 'sma', label: 'Simple average (SMA)', group: 'Indicators' },
  { type: 'rsi', label: 'RSI', group: 'Indicators' },
  { type: 'atr', label: 'ATR (typical daily range)', group: 'Indicators' },
  { type: 'highest', label: 'Highest high of the last N days', group: 'Indicators' },
  { type: 'lowest', label: 'Lowest low of the last N days', group: 'Indicators' },
  { type: 'fib_retr', label: 'Fibonacci retracement level', group: 'Fibonacci' },
  { type: 'fib_ext', label: 'Fibonacci extension level', group: 'Fibonacci' },
  { type: 'swing_high', label: 'High of the latest swing', group: 'Fibonacci' },
  { type: 'swing_low', label: 'Low of the latest swing', group: 'Fibonacci' },
];

export const COMPARISONS: { op: StrategyComparison; label: string }[] = [
  { op: '>', label: 'is above' }, { op: '>=', label: 'is at or above' },
  { op: '<', label: 'is below' }, { op: '<=', label: 'is at or below' },
  { op: 'crosses_above', label: 'crosses above' }, { op: 'crosses_below', label: 'crosses below' },
];

/** A fresh operand of a type, with the values a user most likely wants. */
export function defaultOperand(type: StrategyOperandType): StrategyOperand {
  switch (type) {
    case 'price': return { type, field: 'close' };
    case 'const': return { type, value: 0 };
    case 'ema': case 'sma': return { type, period: 21, field: 'close' };
    case 'rsi': case 'atr': return { type, period: 14 };
    case 'highest': case 'lowest': return { type, period: 20 };
    case 'fib_retr': return { type, ratio: 0.618 };
    case 'fib_ext': return { type, ratio: 1.618 };
    default: return { type };
  }
}

const pct = (r: number) => `${+(r * 100).toFixed(1)}%`;

export function describeOperand(o: StrategyOperand): string {
  switch (o.type) {
    case 'price': return o.field === 'volume' ? 'volume' : `the ${o.field}`;
    case 'const': return String(o.value ?? 0);
    case 'ema': return `EMA(${o.period})${o.field && o.field !== 'close' ? ` of the ${o.field}` : ''}`;
    case 'sma': return `SMA(${o.period})${o.field && o.field !== 'close' ? ` of the ${o.field}` : ''}`;
    case 'rsi': return `RSI(${o.period})`;
    case 'atr': return `ATR(${o.period})`;
    case 'highest': return `the highest high of the last ${o.period} days`;
    case 'lowest': return `the lowest low of the last ${o.period} days`;
    case 'fib_retr': return `the ${pct(o.ratio ?? 0)} Fibonacci retracement`;
    case 'fib_ext': return `the ${pct(o.ratio ?? 0)} Fibonacci extension`;
    case 'swing_high': return 'the high of the latest swing';
    case 'swing_low': return 'the low of the latest swing';
  }
}

export function describeCondition(c: StrategyCondition): string {
  const op = COMPARISONS.find(x => x.op === c.op)?.label ?? c.op;
  return `${describeOperand(c.left)} ${op} ${describeOperand(c.right)}`;
}

function levelText(l: StrategyLevel, kind: 'stop' | 'target'): string {
  const atr = l.atr ?? 0;
  const offset = atr === 0 ? '' : kind === 'stop' ? ` ${atr > 0 ? 'beyond' : 'inside'} by ${Math.abs(atr)} ATR` : ` ${atr > 0 ? 'stretched' : 'pulled in'} by ${Math.abs(atr)} ATR`;
  return `${describeOperand(l.of)}${offset}`;
}

/** The whole strategy in plain sentences, so a user can read back what they built. */
export function describeStrategy(s: StrategyDef): string[] {
  const long = s.direction === 'long';
  const lines = [`${long ? 'Buy' : 'Sell short'} at the next open when ${s.entry.length ? s.entry.map(describeCondition).join(' and ') : '(no entry rule yet)'}.`];
  if (s.stop) lines.push(`Stop: ${levelText(s.stop, 'stop')}.`);
  if (s.target) lines.push(`Target: ${levelText(s.target, 'target')}.`);
  if (s.exit?.length) lines.push(`Close at the next open when ${s.exit.map(describeCondition).join(' or ')}.`);
  if (s.max_bars) lines.push(`Close after ${s.max_bars} days at the latest.`);
  lines.push(s.sizing.mode === 'risk_pct' ? `Size each trade so a stop-out loses ${s.sizing.value}% of equity.` : `Put ${s.sizing.value}% of equity in each trade.`);
  return lines;
}

/** A blank strategy that already passes the server's checks apart from its name. */
export function blankStrategy(): StrategyDef {
  return {
    name: '', direction: 'long', swing_bars: 5, min_leg_atr: 2,
    entry: [{ left: { type: 'price', field: 'close' }, op: 'crosses_above', right: { type: 'ema', period: 50, field: 'close' } }],
    exit: [], stop: { of: { type: 'lowest', period: 10 }, atr: 0.5 }, target: null, max_bars: 0,
    sizing: { mode: 'risk_pct', value: 1 },
  };
}

/** A copy a user can edit without touching the preset it came from. */
export function cloneForEditing(s: StrategyDef, asNew: boolean): StrategyDef {
  const copy: StrategyDef = JSON.parse(JSON.stringify(s));
  delete copy.family;
  if (asNew) { delete copy.id; copy.name = `${s.name} (copy)`.slice(0, 60); }
  return copy;
}

export function usesFibonacci(s: StrategyDef): boolean {
  const fib = (o: StrategyOperand) => o.type === 'fib_retr' || o.type === 'fib_ext' || o.type === 'swing_high' || o.type === 'swing_low';
  return [...s.entry, ...(s.exit ?? [])].some(c => fib(c.left) || fib(c.right)) || !!(s.stop && fib(s.stop.of)) || !!(s.target && fib(s.target.of));
}

/** What is wrong before the server is asked, in the order a user fixes things. */
export function problems(s: StrategyDef): string[] {
  const out: string[] = [];
  if (!s.name.trim()) out.push('Give the strategy a name.');
  if (!s.entry.length) out.push('Add at least one entry condition.');
  if (!s.stop && !s.target && !s.exit?.length && !s.max_bars) out.push('Add a way out: a stop, a target, an exit condition or a day limit.');
  if (s.sizing.mode === 'risk_pct' && !s.stop) out.push('Sizing by risk needs a stop.');
  return out;
}

// ---- results ----

export interface Tile { id: string; label: string; value: string; tone: 'pos' | 'neg' | 'mid'; hint: string }

const signed = (v: number, unit: string, digits = 1) => `${v > 0 ? '+' : ''}${v.toFixed(digits)}${unit}`;
const tone = (v: number): Tile['tone'] => (v > 0.005 ? 'pos' : v < -0.005 ? 'neg' : 'mid');

export function resultTiles(r: StrategyRunResult): Tile[] {
  const s = r.stats;
  return [
    { id: 'total', label: 'Strategy return', value: signed(r.total_return_pct, '%'), tone: tone(r.total_return_pct), hint: 'Total return over the period, after costs' },
    { id: 'bh', label: 'Buy and hold', value: signed(r.buy_hold_return_pct, '%'), tone: 'mid', hint: 'Owning the instrument over the same days' },
    { id: 'dd', label: 'Worst fall', value: signed(r.metrics.max_drawdown_pct, '%'), tone: r.metrics.max_drawdown_pct <= -30 ? 'neg' : 'mid', hint: `Buy and hold: ${signed(r.buy_hold.max_drawdown_pct, '%')}` },
    { id: 'trades', label: 'Trades', value: String(s.trades), tone: s.trades < 30 ? 'neg' : 'mid', hint: 'Fewer than 30 trades say little' },
    { id: 'win', label: 'Win rate', value: s.trades ? `${s.win_rate_pct.toFixed(0)}%` : '–', tone: 'mid', hint: 'Share of trades that ended in profit' },
    { id: 'pf', label: 'Profit factor', value: s.profit_factor ? s.profit_factor.toFixed(2) : '–', tone: s.profit_factor ? (s.profit_factor >= 1.3 ? 'pos' : s.profit_factor < 1 ? 'neg' : 'mid') : 'mid', hint: 'Gross gains divided by gross losses; above 1 means it made money' },
    { id: 'r', label: 'Average R', value: s.avg_r !== undefined ? signed(s.avg_r, 'R', 2) : '–', tone: s.avg_r !== undefined ? tone(s.avg_r) : 'mid', hint: 'Average result in multiples of the money risked to the stop' },
    { id: 'exp', label: 'Time in market', value: `${s.exposure_pct.toFixed(0)}%`, tone: 'mid', hint: 'Share of days with an open trade' },
  ];
}

const PAD = 4;

/** Index of the curve point nearest a date (the curve may be thinned). */
export function nearestIndex(dates: string[], date: string): number {
  let lo = 0, hi = dates.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (dates[mid] < date) lo = mid + 1; else hi = mid;
  }
  return lo > 0 && Math.abs(Date.parse(dates[lo - 1]) - Date.parse(date)) < Math.abs(Date.parse(dates[lo]) - Date.parse(date)) ? lo - 1 : lo;
}

export interface ChartGeometry {
  width: number; height: number; lo: number; hi: number;
  x(i: number): number; y(v: number): number;
}

export function geometry(values: number[], count: number, width: number, height: number): ChartGeometry {
  const finite = values.filter(Number.isFinite);
  let lo = Math.min(...finite), hi = Math.max(...finite);
  if (!finite.length) { lo = 0; hi = 1; }
  const span = hi - lo || 1;
  lo -= span * 0.03; hi += span * 0.03;
  return {
    width, height, lo, hi,
    x: i => PAD + (count <= 1 ? 0 : i / (count - 1)) * (width - 2 * PAD),
    y: v => PAD + (1 - (v - lo) / (hi - lo)) * (height - 2 * PAD),
  };
}

export function linePath(values: number[], g: ChartGeometry): string {
  let d = '';
  values.forEach((v, i) => { if (Number.isFinite(v)) d += `${d ? 'L' : 'M'}${g.x(i).toFixed(1)} ${g.y(v).toFixed(1)}`; });
  return d;
}

export interface Marker { x: number; y: number; kind: 'entry' | 'exit'; win: boolean; label: string }

/** Entry and exit marks on the instrument line, one pair per trade. */
export function tradeMarkers(r: StrategyRunResult, g: ChartGeometry): Marker[] {
  const dates = r.curve.map(p => p.date);
  if (!dates.length) return [];
  const out: Marker[] = [];
  const at = (date: string, price: number) => {
    const i = nearestIndex(dates, date);
    return { x: g.x(i), y: g.y(price / r.base_price * 100) };
  };
  r.trades.forEach((t: StrategyTrade) => {
    const win = t.return_pct > 0;
    out.push({ ...at(t.entry_date, t.entry_price), kind: 'entry', win, label: `Entry ${t.entry_date} at ${t.entry_price}` });
    out.push({ ...at(t.exit_date, t.exit_price), kind: 'exit', win, label: `Exit ${t.exit_date} at ${t.exit_price} (${t.reason}, ${signed(t.return_pct, '%')})` });
  });
  return out;
}

/** Horizontal Fibonacci lines of the latest swing, in chart coordinates. */
export function levelLines(r: StrategyRunResult, g: ChartGeometry): { y: number; label: string; ext: boolean }[] {
  return (r.latest.levels ?? [])
    .map(l => ({ y: g.y(l.price / r.base_price * 100), label: `${l.label} ${l.price}`, ext: l.label.endsWith('ext'), price: l.price }))
    .filter(l => l.y >= 0 && l.y <= g.height)
    .map(({ y, label, ext }) => ({ y, label, ext }));
}

export function chartSummary(r: StrategyRunResult): string {
  const last = r.curve[r.curve.length - 1];
  if (!last) return 'No curve to show.';
  return `${r.symbol}: 100 invested on ${r.from} became ${last.equity.toFixed(0)} with the strategy and ${last.close.toFixed(0)} by holding, until ${r.to}. ${r.stats.trades} trades.`;
}

/** The sentence about what the rules say on the last bar. */
export function nowText(r: StrategyRunResult): string {
  const l = r.latest;
  if (l.in_position) return `A trade is open as of ${l.date}.`;
  if (l.entry_signal) return `All entry conditions hold on the ${l.date} close, so the rules would enter at the next open.`;
  return `No entry signal on the ${l.date} close.`;
}

/** A prompt that gives the agent exactly this result and asks it to stay inside it. */
export function strategyPrompt(r: StrategyRunResult): string {
  const s = r.stats;
  const lines = [
    `Strategy "${r.strategy.name}" on ${r.symbol}, ${r.from} to ${r.to}, costs ${r.strategy.sizing.mode === 'risk_pct' ? 'with risk-based sizing' : 'with fixed sizing'}.`,
    ...describeStrategy(r.strategy),
    `Result: ${signed(r.total_return_pct, '%')} against ${signed(r.buy_hold_return_pct, '%')} for buy and hold; worst fall ${r.metrics.max_drawdown_pct.toFixed(1)}% against ${r.buy_hold.max_drawdown_pct.toFixed(1)}%.`,
    `${s.trades} trades, ${s.win_rate_pct.toFixed(0)}% winners, profit factor ${s.profit_factor ?? 'n/a'}, average ${s.avg_r ?? 'n/a'} R, in the market ${s.exposure_pct.toFixed(0)}% of days.`,
  ];
  return 'Assess this backtest in under 200 words. Use only the figures below and do not add numbers of your own. Say whether the trade count supports any conclusion, what could be overfitted, and what to test next. This is research, not advice.\n\n' + lines.join('\n');
}

export interface PresetGroup { key: string; label: string; note?: string; items: StrategyDef[] }

/**
 * The built-in strategies in sections: the Fibonacci ones first, then one
 * section per character that has rule strategies, in the catalogue's order.
 * A strategy whose character is unknown still shows, under "Other".
 */
export function groupPresets(presets: StrategyDef[], personas: Persona[]): PresetGroup[] {
  const groups: PresetGroup[] = [];
  const fib = presets.filter(p => p.family === 'fibonacci');
  if (fib.length) groups.push({ key: 'fibonacci', label: 'Fibonacci', items: fib });
  const owned = new Set<string>();
  for (const p of personas) {
    const items = presets.filter(s => s.family === 'persona' && s.persona === p.id);
    if (!items.length) continue;
    items.forEach(s => owned.add(s.id ?? ''));
    groups.push({ key: p.id, label: p.name, note: `${p.archetype} (fictional character)`, items });
  }
  const rest = presets.filter(s => s.family !== 'fibonacci' && !owned.has(s.id ?? ''));
  if (rest.length) groups.push({ key: 'other', label: 'Other', items: rest });
  return groups;
}

/** The character a strategy belongs to, for the heading of its panel. */
export function ownerLine(s: StrategyDef, personas: Persona[]): string {
  const p = s.persona ? personas.find(x => x.id === s.persona) : undefined;
  return p ? `${p.name}'s strategy · a fictional character following ${p.archetype.replace(/^The /, '').replace(/\.$/, '')}` : '';
}
