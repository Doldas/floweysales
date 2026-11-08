import { ChangeDetectionStrategy, Component, DestroyRef, effect, inject, input, output, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';

import { MarketService, type TrackedAsset } from '../services';
import type { QuoteSnapshot } from '../models';
import { quoteBadge } from '../core/quote-badge';

/** How often the prices are fetched again while the page is open and visible. */
const REFRESH_MS = 60_000;

interface Row { quote?: QuoteSnapshot; failed?: boolean }

const keyOf = (a: TrackedAsset) => `${a.provider}|${a.symbol}`;

/**
 * The tracked instruments with their latest price and how fresh it is. Click a row to view it; the
 * selected instrument shows the live tick when there is one. Each price is one quote request, made
 * again every minute while the tab is visible, so the list stays within the provider's limits.
 */
@Component({
  selector: 'fse-watchlist',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe],
  template: `
    <div class="wl-head"><span class="wl-title"><i class="bi bi-eye" aria-hidden="true"></i> Watchlist</span><span class="wl-count tabular">{{ assets().length }}</span></div>
    @if (!assets().length) {
      <p class="wl-empty">Nothing tracked yet. Search for a ticker to add it.</p>
    } @else {
      <ul class="wl-rows" aria-label="Watchlist prices">
        @for (a of assets(); track key(a)) {
          <li>
            <button type="button" class="wl-row" [class.selected]="isSelected(a)" [attr.aria-pressed]="isSelected(a)" [attr.data-symbol]="a.symbol" (click)="view.emit(a)" [title]="'View ' + a.symbol + ' on ' + a.provider">
              <span class="wl-id"><strong>{{ a.symbol }}</strong><small>{{ a.provider }}</small></span>
              @if (priceOf(a); as p) {
                <span class="wl-price tabular">{{ p.price | number: '1.2-4' }} <small>{{ p.currency }}</small></span>
                <span class="wl-badge" [attr.data-tone]="p.tone">{{ p.text }}</span>
              } @else if (rows()[key(a)]?.failed) {
                <span class="wl-price muted">No price</span>
              } @else {
                <span class="wl-price muted">…</span>
              }
            </button>
          </li>
        }
      </ul>
    }
  `,
  styles: [`
    :host { display: block; }
    .wl-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.4rem; font-weight: 650; font-size: 0.85rem; .bi { color: var(--fs-accent-text); margin-right: 0.35rem; } .wl-count { color: var(--fs-muted); font-weight: 500; } }
    .wl-empty { color: var(--fs-muted); font-size: 0.8rem; margin: 0; }
    .wl-rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.2rem; }
    .wl-row { width: 100%; display: grid; grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: 'id price' 'id badge'; gap: 0 0.6rem; align-items: center; text-align: left; padding: 0.4rem 0.55rem; border: 1px solid transparent; border-radius: var(--bs-border-radius); background: transparent; color: var(--fs-text); }
    .wl-row:hover { background: var(--fs-surface-2); }
    .wl-row.selected { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
    .wl-id { grid-area: id; display: flex; flex-direction: column; min-width: 0; strong { font-size: 0.85rem; overflow: hidden; text-overflow: ellipsis; } small { color: var(--fs-muted); font-size: 0.68rem; } }
    .wl-price { grid-area: price; text-align: right; font-weight: 600; font-size: 0.85rem; small { color: var(--fs-muted); font-weight: 500; font-size: 0.68rem; } &.muted { color: var(--fs-muted); font-weight: 400; } }
    .wl-badge { grid-area: badge; text-align: right; font-size: 0.68rem; color: var(--fs-muted); }
    .wl-badge[data-tone='live'] { color: var(--fs-pos); }
    .wl-badge[data-tone='stale'] { color: var(--fs-neg); }
    .wl-badge[data-tone='closed'] { color: var(--fs-muted); font-style: italic; }
  `],
})
export class WatchlistComponent {
  private readonly api = inject(MarketService);
  private readonly destroy = inject(DestroyRef);

  readonly assets = input.required<TrackedAsset[]>();
  readonly selected = input<TrackedAsset | null>(null);
  /** The live price of the selected instrument, when a tick has arrived. */
  readonly livePrice = input<number>(0);
  readonly view = output<TrackedAsset>();

  readonly rows = signal<Record<string, Row>>({});
  readonly key = keyOf;

  constructor() {
    effect((onCleanup) => {
      const list = this.assets();
      if (!list.length) { this.rows.set({}); return; }
      this.load(list);
      const timer = setInterval(() => { if (typeof document === 'undefined' || document.visibilityState === 'visible') this.load(list); }, REFRESH_MS);
      onCleanup(() => clearInterval(timer));
    });
    this.destroy.onDestroy(() => { this.closed = true; });
  }

  private closed = false;

  private load(list: TrackedAsset[]): void {
    for (const a of list) {
      this.api.getQuote(a.symbol, a.provider).subscribe({
        next: (quote) => { if (!this.closed) this.rows.update(r => ({ ...r, [keyOf(a)]: { quote } })); },
        error: () => { if (!this.closed) this.rows.update(r => ({ ...r, [keyOf(a)]: { ...r[keyOf(a)], failed: !r[keyOf(a)]?.quote } })); },
      });
    }
  }

  isSelected(a: TrackedAsset): boolean {
    const s = this.selected();
    return !!s && s.symbol === a.symbol && s.provider === a.provider;
  }

  /** The price to show: the live tick for the selected instrument, else its last quote. */
  priceOf(a: TrackedAsset): { price: number; currency: string; text: string; tone: string } | null {
    const q = this.rows()[keyOf(a)]?.quote;
    if (this.isSelected(a) && this.livePrice() > 0) return { price: this.livePrice(), currency: q?.currency ?? '', text: 'live tick', tone: 'live' };
    if (!q) return null;
    const b = quoteBadge(q);
    return { price: q.price, currency: q.currency, text: b.text, tone: b.tone };
  }
}
