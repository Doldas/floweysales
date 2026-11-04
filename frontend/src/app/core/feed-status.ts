/**
 * What the price feed is doing, as four independent facts. A connection alone
 * never means the prices are live: that needs a provider that says its quotes
 * are real time AND a recent update. Pure and import-light so node --test can
 * run it.
 */
export type FeedConnection = 'none' | 'connecting' | 'connected' | 'reconnecting' | 'offline';
export type FeedFreshness = 'no-data' | 'fresh' | 'stale';
export type FeedDataType = 'realtime' | 'delayed' | 'bar-close' | 'unknown';
export type FeedTone = 'is-success' | 'is-warning' | 'is-danger' | 'is-info' | '';

export interface FeedInput {
  research: boolean;
  hasTracker: boolean;
  connection: FeedConnection;
  /** When this browser last received a price update (ms); null before the first one. */
  lastUpdateAt: number | null;
  /** The provider's declared quote kind, such as `ohlc_bar_close`; empty when unknown. */
  quoteKind: string;
  providerLabel: string;
  officialApi: boolean | null;
  /** An update older than this counts as stale. */
  staleAfterMs?: number;
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
  /** One plain sentence per fact, for the details popover. */
  details: { label: string; text: string }[];
  /** Reconnecting by hand is meaningful. */
  canReconnect: boolean;
}

const formatUtcTime = (ms: number): string => new Date(ms).toISOString().slice(11, 16);

export const DEFAULT_STALE_AFTER_MS = 5 * 60_000;

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

function age(ms: number): string {
  const minutes = Math.round(ms / 60_000);
  if (minutes < 1) return 'under a minute ago';
  if (minutes < 60) return `${minutes} min ago`;
  return `${Math.round(minutes / 60)} h ago`;
}

export function feedStatus(input: FeedInput, now: number): FeedStatus {
  const dataType = dataTypeOf(input.quoteKind);
  const staleAfter = input.staleAfterMs ?? DEFAULT_STALE_AFTER_MS;
  const freshness: FeedFreshness = input.lastUpdateAt === null
    ? 'no-data'
    : now - input.lastUpdateAt > staleAfter ? 'stale' : 'fresh';
  const last = input.lastUpdateAt === null ? '' : formatUtcTime(input.lastUpdateAt) + ' UTC';

  const base = { connection: input.connection, freshness, dataType, pulse: false, canReconnect: false };

  if (input.research) {
    return { ...base, connection: 'none', headline: 'Prices from history', tone: 'is-info',
      details: [{ label: 'Source', text: 'Research mode shows the loaded history. There is no live price stream.' }] };
  }
  if (!input.hasTracker || input.connection === 'none') {
    return { ...base, connection: 'none', headline: 'No active tracker', tone: '',
      details: [{ label: 'Connection', text: connectionText.none }] };
  }

  const details = [
    { label: 'Connection', text: connectionText[input.connection] },
    { label: 'Freshness', text: freshness === 'no-data'
        ? 'No price update has arrived yet.'
        : `Last update ${last} (${age(now - input.lastUpdateAt!)}).` },
    { label: 'Data type', text: dataTypeText[dataType] },
    { label: 'Market session', text: 'Unknown. The provider does not report whether the market is open.' },
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
  if (freshness === 'stale') return status(`No new prices since ${last}`, 'is-warning', { canReconnect: true });
  if (dataType === 'realtime') return status('Live prices', 'is-success', { pulse: true });
  if (dataType === 'delayed') return status('Delayed prices', 'is-info');
  return status(`Last update ${last}`, 'is-info');
}
