/**
 * What the price feed is doing, as four independent facts. A connection alone
 * never means the prices are live: that needs a provider that says its quotes
 * are real time AND a recent update. Pure and import-light so node --test can
 * run it.
 */
export type FeedConnection = 'none' | 'connecting' | 'connected' | 'reconnecting' | 'offline';
export type FeedFreshness = 'no-data' | 'fresh' | 'stale';
export type FeedDataType = 'realtime' | 'delayed' | 'bar-close' | 'unknown';
export type MarketSession = 'open' | 'closed' | 'unknown';
export type FeedTone = 'is-success' | 'is-warning' | 'is-danger' | 'is-info' | '';

export interface FeedInput {
  research: boolean;
  /** The selected instrument has a tracker (live) or watchlist entry. */
  hasTracker: boolean;
  /** An instrument is selected, tracked or not. */
  hasSelection?: boolean;
  connection: FeedConnection;
  /** When this browser last received a price update (ms); null before the first one. */
  lastUpdateAt: number | null;
  /** The provider's declared quote kind, such as `ohlc_bar_close`; empty when unknown. */
  quoteKind: string;
  providerLabel: string;
  officialApi: boolean | null;
  /** An update older than this counts as stale. */
  staleAfterMs?: number;
  /** The provider's own time of the latest quote (ms); null or absent when it did not say. */
  quoteTimeAt?: number | null;
  /** How far behind the market the provider says the quote is; null or absent when unknown. */
  delaySeconds?: number | null;
  /** The market session of the latest tick; absent or `unknown` when nobody can say. */
  marketState?: MarketSession;
  /** `provider` when the provider stated it; `regular_hours` when computed (holidays unknown). */
  marketBasis?: string;
}

export interface FeedStatus {
  /** Short text for the badge. */
  headline: string;
  tone: FeedTone;
  /** Only true when the connection, the provider and the age of the last update all say live. */
  pulse: boolean;
  connection: FeedConnection;
  freshness: FeedFreshness;
  dataType: FeedDataType;
  session: MarketSession;
  /** One plain sentence per fact, for the details popover. */
  details: { label: string; text: string }[];
  /** Reconnecting by hand is meaningful. */
  canReconnect: boolean;
}

const formatUtcTime = (ms: number): string => new Date(ms).toISOString().slice(11, 16);

export const DEFAULT_STALE_AFTER_MS = 5 * 60_000;

/** Minutes from a kind such as `delayed_15m`; null when the kind does not say. */
export function delayMinutesOf(quoteKind: string): number | null {
  const m = /^delayed[_-]?(\d{1,3})m?$/.exec(quoteKind.trim().toLowerCase());
  return m ? Number(m[1]) : null;
}

export function dataTypeOf(quoteKind: string): FeedDataType {
  const kind = quoteKind.trim().toLowerCase();
  if (kind === 'realtime' || kind === 'real_time' || kind === 'live') return 'realtime';
  if (kind.startsWith('delayed')) return 'delayed';
  if (kind === 'ohlc_bar_close' || kind.includes('bar')) return 'bar-close';
  return 'unknown';
}

const connectionText: Record<FeedConnection, string> = {
  none: 'No instrument is being tracked.',
  connecting: 'Connecting to the price stream.',
  connected: 'The stream to the FloweySales server is open.',
  reconnecting: 'The stream dropped and the browser is retrying.',
  offline: 'The stream is closed and not delivering prices. A retry is scheduled.',
};

const dataTypeText: Record<FeedDataType, string> = {
  realtime: 'The provider says its quotes are real time.',
  delayed: 'The provider says its quotes are delayed.',
  'bar-close': 'Prices are the close of the latest bar, not a streaming quote.',
  unknown: 'The provider does not say whether quotes are real time or delayed.',
};

function sessionText(session: MarketSession, basis: string | undefined): string {
  if (session === 'unknown') return 'Unknown. Neither the provider nor the exchange could be identified for this instrument.';
  const state = session === 'open' ? 'Open' : 'Closed';
  return basis === 'provider'
    ? `${state}, as stated by the provider.`
    : `${state} by the exchange's regular weekday hours. Holidays and early closes are not known.`;
}

function age(ms: number): string {
  const minutes = Math.round(ms / 60_000);
  if (minutes < 1) return 'under a minute ago';
  if (minutes < 60) return `${minutes} min ago`;
  return `${Math.round(minutes / 60)} h ago`;
}

export function feedStatus(input: FeedInput, now: number): FeedStatus {
  const stated = input.delaySeconds ?? null;
  // A stated delay beats the declared kind: "real time" with a delay is not real time.
  const delayMin = stated !== null && stated > 0 ? Math.round(stated / 60) : delayMinutesOf(input.quoteKind);
  const dataType: FeedDataType = stated !== null && stated > 0 ? 'delayed' : dataTypeOf(input.quoteKind);
  const session: MarketSession = input.marketState ?? 'unknown';
  const staleAfter = input.staleAfterMs ?? DEFAULT_STALE_AFTER_MS;
  const freshness: FeedFreshness = input.lastUpdateAt === null
    ? 'no-data'
    : now - input.lastUpdateAt > staleAfter ? 'stale' : 'fresh';
  const last = input.lastUpdateAt === null ? '' : formatUtcTime(input.lastUpdateAt) + ' UTC';

  const quoteAt = input.quoteTimeAt ?? null;
  const quoteText = quoteAt === null ? '' : formatUtcTime(quoteAt) + ' UTC';

  const base = { connection: input.connection, freshness, dataType, session, pulse: false, canReconnect: false };

  if (input.research) {
    return { ...base, connection: 'none', headline: 'Prices from history', tone: 'is-info',
      details: [{ label: 'Source', text: 'Research mode shows the loaded history. There is no live price stream.' }] };
  }
  if (!input.hasTracker && input.hasSelection) {
    return { ...base, connection: 'none', headline: 'Not tracked live', tone: 'is-info',
      details: [{ label: 'Source', text: 'This instrument is selected but has no live tracker, so only loaded history is shown. Use "Track live" to receive prices.' }] };
  }
  if (!input.hasTracker || input.connection === 'none') {
    return { ...base, connection: 'none', headline: 'No active tracker', tone: '',
      details: [{ label: 'Connection', text: connectionText.none }] };
  }

  const details = [
    { label: 'Connection', text: connectionText[input.connection] },
    { label: 'Freshness', text: freshness === 'no-data'
        ? 'No price update has arrived yet.'
        : `Last update received ${last} (${age(now - input.lastUpdateAt!)}).`
          + (quoteAt === null ? ' The provider did not state the quote\'s own time.' : ` The quote itself is from ${quoteText}.`) },
    { label: 'Data type', text: dataType === 'delayed' && delayMin !== null
        ? `The quotes are delayed by about ${delayMin} min.` : dataTypeText[dataType] },
    { label: 'Market session', text: sessionText(session, input.marketBasis) },
    { label: 'Provider', text: input.providerLabel
        + (input.officialApi === null ? '' : input.officialApi ? ' (official API)' : ' (unofficial API)') },
  ];
  const status = (headline: string, tone: FeedTone, extra: Partial<FeedStatus> = {}): FeedStatus =>
    ({ ...base, headline, tone, details, ...extra });

  if (input.connection === 'connecting') return status('Connecting…', 'is-warning');
  if (input.connection === 'reconnecting') return status('Reconnecting…', 'is-warning');
  if (input.connection === 'offline') return status('Prices not updating', 'is-danger', { canReconnect: true });

  // Connected from here on.
  if (freshness === 'no-data') return status('Waiting for the first price', 'is-warning');
  // A closed market explains old prices, so it comes before "stale".
  if (session === 'closed') return status(`Market closed · last ${quoteAt === null ? last : quoteText}`, 'is-info');
  if (freshness === 'stale') return status(`No new prices since ${last}`, 'is-warning', { canReconnect: true });
  if (dataType === 'delayed') return status(delayMin === null ? 'Delayed prices' : `Delayed ${delayMin} min`, 'is-info');
  if (dataType === 'realtime') return status('Live prices', 'is-success', { pulse: true });
  return status(quoteAt === null ? `Last update ${last}` : `Last quote ${quoteText}`, 'is-info');
}
