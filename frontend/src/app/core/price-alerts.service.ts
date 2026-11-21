import { computed, effect, inject, Injectable, signal, untracked } from '@angular/core';

import { NotifyService } from './notify.service';
import { type PriceAlert, alertInstrumentKey, checkAlerts, directionFor, formatPrice, MAX_ALERTS, parseAlerts } from './price-alerts';
import { QuoteBoardService } from './quote-board.service';

const STORAGE_KEY = 'fs.price-alerts';

/**
 * The user's price alerts, kept in this browser and checked whenever the quote board has a new price
 * (a refresh or a live tick). A hit shows a toast and, when the user allowed it, a browser
 * notification; the alert then stays as a record until it is removed. Nothing trades.
 */
@Injectable({ providedIn: 'root' })
export class PriceAlertsService {
  private readonly board = inject(QuoteBoardService);
  private readonly notify = inject(NotifyService);

  readonly alerts = signal<PriceAlert[]>(load());
  readonly active = computed(() => this.alerts().filter(a => !a.triggered_at));
  readonly triggered = computed(() => this.alerts().filter(a => a.triggered_at));

  constructor() {
    // The board fetches a price for every instrument with an active alert.
    effect(() => {
      const seen = new Set<string>();
      const list = this.active().filter(a => !seen.has(alertInstrumentKey(a)) && !!seen.add(alertInstrumentKey(a)));
      untracked(() => this.board.extra.set(list.map(a => ({ symbol: a.symbol, provider: a.provider, tracked: false, favorite: false }))));
    });
    effect(() => {
      const prices = new Map(this.board.watchedRows().map(r => [r.key, r.price]));
      if (!this.active().length) return;
      untracked(() => {
        const r = checkAlerts(this.alerts(), a => prices.get(alertInstrumentKey(a)) ?? 0, Date.now());
        if (!r.fired.length) return;
        this.alerts.set(r.alerts);
        for (const a of r.fired) this.announce(a);
      });
    });
    effect(() => save(this.alerts()));
  }

  forInstrument(i: { symbol: string; provider: string }): PriceAlert[] {
    const key = alertInstrumentKey(i);
    return this.alerts().filter(a => alertInstrumentKey(a) === key);
  }

  /** Adds an alert toward the level from the price now; false when the level equals the price. */
  add(i: { symbol: string; provider: string }, level: number, price: number): boolean {
    const direction = directionFor(level, price);
    if (!direction) return false;
    const alert: PriceAlert = {
      id: `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`,
      symbol: i.symbol.trim().toUpperCase(), provider: i.provider.trim().toUpperCase(), direction, level, created_at: Date.now(),
    };
    this.alerts.update(list => [alert, ...list].slice(0, MAX_ALERTS));
    askPermission();
    return true;
  }

  remove(id: string): void { this.alerts.update(list => list.filter(a => a.id !== id)); }
  clearTriggered(): void { this.alerts.update(list => list.filter(a => !a.triggered_at)); }

  private announce(a: PriceAlert): void {
    const title = `${a.symbol} is ${a.direction} ${formatPrice(a.level)}`;
    const body = `The provider reported ${formatPrice(a.triggered_price ?? 0)}. An alert, not an order.`;
    this.notify.info(body, title);
    if (typeof Notification !== 'undefined' && Notification.permission === 'granted' && document.visibilityState !== 'visible') {
      try { new Notification(title, { body, tag: a.id }); } catch { /* not available in this context */ }
    }
  }
}

function load(): PriceAlert[] {
  try { return parseAlerts(JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '[]')); } catch { return []; }
}

function save(list: readonly PriceAlert[]): void {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(list)); } catch { /* storage full or blocked */ }
}

/** Asks once, on the user's own action, whether hits may also show as system notifications. */
function askPermission(): void {
  if (typeof Notification !== 'undefined' && Notification.permission === 'default') void Notification.requestPermission().catch(() => undefined);
}
