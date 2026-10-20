import { Injectable, signal } from '@angular/core';
import { TradingRecord } from './models';

/**
 * LiveSseService keeps a resilient EventSource connection to the realtime
 * price stream and exposes the latest tick plus connection state as signals.
 */
@Injectable({ providedIn: 'root' })
export class LiveSseService {
  private source: EventSource | null = null;
  private currentConfigKey: string | null = null;

  readonly latestTick = signal<TradingRecord | null>(null);
  readonly connected = signal(false);
  readonly attempted = signal(false);

  /** (Re)connects for the given symbol. Reopening the source is cheap. */
  connectFor(symbol: string, provider = ''): void {
    const key = (symbol || '__all__') + '|' + provider;
    if (this.currentConfigKey === key && this.source) {
      return;
    }
    this.close();
    this.currentConfigKey = key;
    this.attempted.set(true);

    const open = () => {
      if (this.currentConfigKey !== key) return;
      const es = new EventSource('/api/prices/stream');
      this.source = es;

      es.onopen = () => this.connected.set(true);
      es.onmessage = (ev) => {
        try {
          const record: TradingRecord = JSON.parse(ev.data);
          if (symbol && record.symbol !== symbol) return;
          if (provider && record.provider && record.provider !== provider) return;
          this.latestTick.set(record);
          this.connected.set(true);
        } catch (err) {
          console.error('Malformed SSE frame', err, ev.data);
        }
      };
      // The browser retries automatically on transient errors; surface the
      // state through the signal so the UI can show "reconnecting".
      es.onerror = () => {
        this.connected.set(false);
      };
    };
    open();
  }

  /** Drops the connection entirely (used when the app reloads a new asset). */
  close(): void {
    if (this.source) {
      this.source.close();
      this.source = null;
    }
    this.connected.set(false);
  }

  /** A tick is newer if it changed the timestamp for the tracked symbol. */
  static isFreshTick(current: TradingRecord | null, incoming: TradingRecord): boolean {
    if (!current) return true;
    if (current.symbol !== incoming.symbol) return true;
    return incoming.timestamp > current.timestamp;
  }
}
