import { computed, DestroyRef, effect, inject, Injectable, signal, untracked } from '@angular/core';

import { MarketService } from '../services';
import type { QuoteSnapshot } from '../models';
import { MarketState } from './market-state';
import { type BoardEntry, type BoardInstrument, boardInstruments, boardKey, dayPercent, nextEntry, sameBoard } from './quote-board';

/** How often every price on the board is fetched again while the tab is visible. */
const REFRESH_MS = 60_000;

/** One instrument of the board with its latest quote, the price to show and its day change. */
export interface BoardRow extends BoardInstrument {
  key: string;
  entry?: BoardEntry;
  quote?: QuoteSnapshot;
  /** The live tick for the instrument on screen, else the last quote's close; 0 when none yet. */
  price: number;
  live: boolean;
  dayPct: number | null;
}

/**
 * The latest quote of every tracked and starred instrument, shared by the watchlist, the ticker
 * tape, the heatmap and the top movers so each price is one request a minute, not one per widget.
 * Prices are provider bar closes (see QuoteSnapshot.note), never executable bids or asks.
 */
@Injectable({ providedIn: 'root' })
export class QuoteBoardService {
  private readonly api = inject(MarketService);
  private readonly market = inject(MarketState);

  readonly instruments = computed(() => boardInstruments(this.market.trackedAssets(), this.market.favorites()), { equal: sameBoard });
  readonly entries = signal<Record<string, BoardEntry>>({});
  /** When the last full refresh started, in ms since the epoch. */
  readonly refreshedAt = signal(0);

  /** The instrument on screen in Markets; its quote is fetched with the board even when it is neither tracked nor starred. */
  readonly focus = computed<BoardInstrument | null>(() => {
    const symbol = this.market.trackedSymbol().trim().toUpperCase();
    const provider = this.market.trackedProvider().trim().toUpperCase();
    return symbol && provider ? { symbol, provider, tracked: false, favorite: false } : null;
  }, { equal: (a, b) => (a && b ? boardKey(a) === boardKey(b) : a === b) });

  readonly rows = computed<BoardRow[]>(() => this.instruments().map(i => this.toRow(i)));

  /** The board row of the instrument on screen, with the live tick when it is tracked. */
  readonly focusRow = computed<BoardRow | null>(() => {
    const focus = this.focus();
    if (!focus) return null;
    return this.rows().find(r => r.key === boardKey(focus)) ?? this.toRow(focus);
  });

  /** Rows with a price, for widgets that show nothing until the first quote arrives. */
  readonly priced = computed(() => this.rows().filter(r => r.price > 0));

  private closed = false;

  constructor() {
    // A new instrument is fetched at once; the timer refreshes the whole board.
    effect(() => {
      const list = this.watched();
      untracked(() => {
        if (!this.refreshedAt()) this.refreshedAt.set(Date.now());
        this.load(list.filter(i => !this.entries()[boardKey(i)]));
      });
    });
    const visible = () => typeof document === 'undefined' || document.visibilityState === 'visible';
    const timer = setInterval(() => { if (visible()) this.refresh(); }, REFRESH_MS);
    const onVisible = () => { if (visible() && Date.now() - this.refreshedAt() >= REFRESH_MS) this.refresh(); };
    if (typeof document !== 'undefined') document.addEventListener('visibilitychange', onVisible);
    inject(DestroyRef).onDestroy(() => {
      this.closed = true;
      clearInterval(timer);
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', onVisible);
    });
  }

  row(a: { symbol: string; provider: string }): BoardRow | undefined {
    const key = boardKey(a);
    return this.rows().find(r => r.key === key);
  }

  /** Fetches every price on the board, and the one on screen, again now. */
  refresh(): void {
    this.refreshedAt.set(Date.now());
    this.load(this.watched());
  }

  /** Instruments other features need prices for, such as price alerts; refreshed with the board. */
  readonly extra = signal<readonly BoardInstrument[]>([], { equal: sameBoard });

  /** The board, the instrument on screen and the extra instruments, each once. */
  private readonly watched = computed(() => {
    const list = [...this.instruments()];
    const keys = new Set(list.map(boardKey));
    for (const i of [this.focus(), ...this.extra()]) {
      if (i && !keys.has(boardKey(i))) { keys.add(boardKey(i)); list.push(i); }
    }
    return list;
  });

  /** A row for every instrument whose price is fetched, the board's and the extra ones. */
  readonly watchedRows = computed<BoardRow[]>(() => this.watched().map(i => this.toRow(i)));

  private toRow(i: BoardInstrument): BoardRow {
    const key = boardKey(i);
    const entry = this.entries()[key];
    // Only a tick from the live stream is newer than the quote; the history fallback of livePrice is not.
    const t = this.market.selectedTracked() ? this.market.latestTick() : null;
    const tick = t?.marketData?.price ?? t?.price ?? 0;
    const focus = this.focus();
    const live = tick > 0 && !!focus && boardKey(focus) === key;
    const price = live ? tick : entry?.quote?.price ?? 0;
    return { ...i, key, entry, quote: entry?.quote, price, live, dayPct: dayPercent(entry?.quote, price) };
  }

  private load(list: readonly BoardInstrument[]): void {
    for (const i of list) {
      const key = boardKey(i);
      this.api.getQuote(i.symbol, i.provider).subscribe({
        next: (quote) => { if (!this.closed) this.entries.update(e => ({ ...e, [key]: nextEntry(e[key], quote) })); },
        error: () => {
          if (this.closed) return;
          // A failed refresh keeps the last price; only an instrument that never had one is marked.
          this.entries.update(e => (e[key]?.quote ? e : { ...e, [key]: { move: '', moveSeq: 0, failed: true } }));
        },
      });
    }
  }
}
