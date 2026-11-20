/**
 * Regular weekday trading hours of the major exchanges, mirroring internal/market/session.go, and
 * the session of each at a moment. Public holidays and early closes are not known, so "open" means
 * "inside regular hours" (the server's regular_hours basis) and may be wrong on a holiday. Computed
 * in the browser with Intl; nothing is fetched.
 */

/** One trading window in minutes after local midnight. */
export interface TradingWindow { open: number; close: number }

export interface ExchangeHours {
  id: string;
  name: string;
  /** The city, for compact views. */
  city: string;
  zone: string;
  windows: readonly TradingWindow[];
  /** Trades around the clock every day (crypto venues). */
  always?: boolean;
}

export type SessionState = 'open' | 'break' | 'closed';

export interface ExchangeSession {
  state: SessionState;
  /** When the current window closes, while open (never for venues that always trade). */
  closesAt?: number;
  /** When the next window opens, while on a break or closed. */
  opensAt?: number;
  /** The share of the current window that has passed, 0..1, while open. */
  progress?: number;
}

/** A session on the 24-hour UTC day: from and to as shares of the day, start and end as instants. */
export interface DaySegment { from: number; to: number; start: number; end: number }

const MINUTE = 60_000;
const DAY = 24 * 60 * MINUTE;

const hm = (h: number, m = 0) => h * 60 + m;
const single = (id: string, name: string, city: string, zone: string, open: number, close: number): ExchangeHours =>
  ({ id, name, city, zone, windows: [{ open, close }] });

/** The exchanges of the world clock, in the order their day starts in UTC. */
export const WORLD_EXCHANGES: readonly ExchangeHours[] = [
  single('XASX', 'ASX', 'Sydney', 'Australia/Sydney', hm(10), hm(16)),
  { id: 'XTKS', name: 'Tokyo Stock Exchange', city: 'Tokyo', zone: 'Asia/Tokyo', windows: [{ open: hm(9), close: hm(11, 30) }, { open: hm(12, 30), close: hm(15, 30) }] },
  { id: 'XHKG', name: 'Hong Kong Exchange', city: 'Hong Kong', zone: 'Asia/Hong_Kong', windows: [{ open: hm(9, 30), close: hm(12) }, { open: hm(13), close: hm(16) }] },
  single('XSTO', 'Nasdaq Stockholm', 'Stockholm', 'Europe/Stockholm', hm(9), hm(17, 30)),
  single('XETR', 'Xetra / Frankfurt', 'Frankfurt', 'Europe/Berlin', hm(9), hm(17, 30)),
  single('XPAR', 'Euronext Paris', 'Paris', 'Europe/Paris', hm(9), hm(17, 30)),
  single('XLON', 'London Stock Exchange', 'London', 'Europe/London', hm(8), hm(16, 30)),
  single('US', 'US exchanges', 'New York', 'America/New_York', hm(9, 30), hm(16)),
  single('XTSE', 'Toronto Stock Exchange', 'Toronto', 'America/Toronto', hm(9, 30), hm(16)),
  { id: 'CRYPTO', name: 'Crypto venues', city: 'Crypto', zone: 'UTC', windows: [{ open: 0, close: hm(24) }], always: true },
];

export function exchangeById(id: string): ExchangeHours | undefined {
  return WORLD_EXCHANGES.find(e => e.id === id);
}

interface LocalParts { year: number; month: number; day: number; minute: number; second: number }

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(zone: string): Intl.DateTimeFormat {
  let f = formatters.get(zone);
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone: zone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric',
    });
    formatters.set(zone, f);
  }
  return f;
}

/** The wall-clock date and time of an instant in a zone. */
function localParts(t: number, zone: string): LocalParts {
  const p: Record<string, number> = {};
  for (const part of formatter(zone).formatToParts(t)) if (part.type !== 'literal') p[part.type] = Number(part.value);
  return { year: p['year'], month: p['month'], day: p['day'], minute: (p['hour'] % 24) * 60 + p['minute'], second: p['second'] };
}

/** How far the zone's wall clock is ahead of UTC at an instant. */
function offsetMs(t: number, zone: string): number {
  const p = localParts(t, zone);
  return Date.UTC(p.year, p.month - 1, p.day, 0, p.minute, p.second) - Math.floor(t / 1000) * 1000;
}

/** The instant of a wall-clock time in a zone. Trading hours never fall in a DST gap. */
export function zonedInstant(year: number, month: number, day: number, minute: number, zone: string): number {
  const wall = Date.UTC(year, month - 1, day, 0, minute);
  const first = wall - offsetMs(wall, zone);
  return wall - offsetMs(first, zone);
}

/** The exchange's trading windows that overlap [from, to), in time order. */
function windowsBetween(ex: ExchangeHours, from: number, to: number): Array<{ start: number; end: number }> {
  const a = localParts(from, ex.zone);
  const b = localParts(to, ex.zone);
  const out: Array<{ start: number; end: number }> = [];
  for (let d = Date.UTC(a.year, a.month - 1, a.day) - DAY; d <= Date.UTC(b.year, b.month - 1, b.day) + DAY; d += DAY) {
    const date = new Date(d);
    const weekday = date.getUTCDay();
    if (!ex.always && (weekday === 0 || weekday === 6)) continue;
    for (const w of ex.windows) {
      const start = zonedInstant(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate(), w.open, ex.zone);
      const end = zonedInstant(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate(), w.close, ex.zone);
      if (end > from && start < to) out.push({ start, end });
    }
  }
  return out;
}

function sameLocalDay(a: number, b: number, zone: string): boolean {
  const x = localParts(a, zone);
  const y = localParts(b, zone);
  return x.year === y.year && x.month === y.month && x.day === y.day;
}

/** The regular session of the exchange at an instant, with when it next closes or opens. */
export function sessionAt(ex: ExchangeHours, now: number): ExchangeSession {
  if (ex.always) return { state: 'open' };
  const windows = windowsBetween(ex, now - DAY, now + 8 * DAY);
  const current = windows.find(w => w.start <= now && now < w.end);
  if (current) return { state: 'open', closesAt: current.end, progress: (now - current.start) / (current.end - current.start) };
  const next = windows.find(w => w.start > now);
  const previous = windows.filter(w => w.end <= now).at(-1);
  // Between two windows of the same local day is a break (Tokyo and Hong Kong close for lunch).
  const onBreak = !!previous && !!next && sameLocalDay(previous.end, next.start, ex.zone);
  return next ? { state: onBreak ? 'break' : 'closed', opensAt: next.start } : { state: 'closed' };
}

/** The exchange's sessions on the UTC day of `now`, clipped to that day. */
export function utcDaySegments(ex: ExchangeHours, now: number): DaySegment[] {
  const dayStart = Math.floor(now / DAY) * DAY;
  return windowsBetween(ex, dayStart, dayStart + DAY)
    .map(w => ({ start: Math.max(w.start, dayStart), end: Math.min(w.end, dayStart + DAY) }))
    .reduce<Array<{ start: number; end: number }>>((all, w) => {
      // Back-to-back windows (a venue that always trades) read as one session.
      const last = all.at(-1);
      if (last && last.end === w.start) last.end = w.end;
      else all.push({ ...w });
      return all;
    }, [])
    .map(w => ({ ...w, from: (w.start - dayStart) / DAY, to: (w.end - dayStart) / DAY }));
}

/** The windows of the trading day that is in session now, or else the next one. */
export function tradingDayWindows(ex: ExchangeHours, now: number): Array<{ start: number; end: number }> {
  if (ex.always) return [];
  const windows = windowsBetween(ex, now - DAY, now + 8 * DAY);
  const anchor = windows.find(w => w.end > now);
  return anchor ? windows.filter(w => sameLocalDay(w.start, anchor.start, ex.zone)) : [];
}

/** A short time-to-go: "45 min", "2 h 05 min", "1 d 4 h". */
export function formatCountdown(ms: number): string {
  const minutes = Math.floor(ms / MINUTE);
  if (minutes < 1) return 'less than a minute';
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ${String(minutes % 60).padStart(2, '0')} min`;
  return `${Math.floor(hours / 24)} d ${hours % 24} h`;
}

/** The UTC clock time of an instant, "09:05". */
export function utcClock(t: number): string {
  const d = new Date(t);
  return `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`;
}
