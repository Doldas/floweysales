import { Injectable, signal } from '@angular/core';
import { FeedConnection } from './core/feed-status';
import { TradingRecord } from './models';

/**
 * LiveSseService keeps a resilient EventSource connection to the realtime
 * price stream and exposes the latest tick plus connection state as signals.
 */
@Injectable({ providedIn: 'root' })
export class LiveSseService {
  private source: EventSource | null = null;
  private currentConfigKey: string | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private retryDelay = 1000;

  readonly latestTick = signal<TradingRecord | null>(null);
  readonly connected = signal(false);
  readonly attempted = signal(false);
  /** Whether the browser is opening, holding, retrying or has lost the stream. */
  readonly state = signal<FeedConnection>('none');
  /** When this browser last received a price update (ms). */
  readonly lastUpdateAt = signal<number | null>(null);

  /** (Re)connects for the given symbol. Reopening the source is cheap. */
  connectFor(symbol: string,provider=''): void {
    const key = (symbol || '__all__')+'|'+provider;
    // A source the browser gave up on (error status, proxy reset) is not "connected".
    if (this.currentConfigKey === key && this.source && this.source.readyState !== EventSource.CLOSED) {
      return;
    }
    this.close();
    this.currentConfigKey = key;
    this.latestTick.set(null);
    this.lastUpdateAt.set(null);
    this.attempted.set(true);
    this.state.set('connecting');

    const open = () => {
      if (this.currentConfigKey !== key) return;
      const query=new URLSearchParams();
      if(symbol)query.set('symbol',symbol);
      if(provider)query.set('provider',provider);
      const es = new EventSource('/api/prices/stream?'+query.toString());
      this.source = es;

      es.onopen = () => { this.retryDelay = 1000; this.connected.set(true); this.state.set('connected'); };
      es.onmessage = (ev) => {
        try {
          if(this.currentConfigKey!==key)return;
          const record: TradingRecord = JSON.parse(ev.data);
          // Explicit client-side guard even when older backends do not filter SSE.
          if(record.symbol!==symbol || (provider && record.provider && record.provider!==provider))return;
          this.latestTick.set(record);
          this.lastUpdateAt.set(Date.now());
          this.connected.set(true);
          this.state.set('connected');
        } catch (err) {
          console.error('Malformed SSE frame', err, ev.data);
        }
      };
      // The browser retries automatically on transient errors; surface the
      // state through the signal so the UI can show "reconnecting".
      es.onerror = () => {
        this.connected.set(false);
        // CONNECTING: the browser is retrying by itself. CLOSED: it gave up and we retry.
        this.state.set(es.readyState === EventSource.CLOSED ? 'offline' : 'reconnecting');
        // After a non-recoverable failure the browser stops retrying; reopen ourselves.
        if (es.readyState === EventSource.CLOSED && this.source === es && this.currentConfigKey === key) {
          es.close();
          this.source = null;
          this.retryTimer = setTimeout(open, this.retryDelay);
          this.retryDelay = Math.min(this.retryDelay * 2, 30000);
        }
      };
    };
    open();
  }

  /** Drops the connection entirely (used when the app reloads a new asset). */
  close(): void {
    if (this.retryTimer) {
      clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
    if (this.source) {
      this.source.close();
      this.source = null;
    }
    this.connected.set(false);
    this.state.set('none');
  }

  /** Drops the stream and forgets its last tick, for an instrument that has no tracker. */
  clear(): void {
    this.close();
    this.currentConfigKey = null;
    this.latestTick.set(null);
    this.lastUpdateAt.set(null);
  }

  /** Drops the stream and opens a fresh one for the current instrument. */
  reconnect(): void {
    const key = this.currentConfigKey;
    if (!key) return;
    const [symbol, provider] = key.split('|');
    this.currentConfigKey = null;
    this.connectFor(symbol === '__all__' ? '' : symbol, provider);
  }

  /** A tick is newer if it changed the timestamp for the tracked symbol. */
  static isFreshTick(current: TradingRecord | null, incoming: TradingRecord): boolean {
    if (!current) return true;
    if (current.symbol !== incoming.symbol) return true;
    return incoming.timestamp > current.timestamp;
  }
}
