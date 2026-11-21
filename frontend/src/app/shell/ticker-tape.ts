import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { DecimalPipe, NgTemplateOutlet } from '@angular/common';
import { Router } from '@angular/router';

import { MarketState } from '../core/market-state';
import { flashClass, signedPercent } from '../core/quote-board';
import { type BoardRow, QuoteBoardService } from '../core/quote-board.service';

const PAUSE_KEY = 'fs.tickerPaused';

/**
 * A scrolling band of every tracked and starred instrument with its price and day change, under the
 * top bar on every page. It pauses on hover, on focus and with its pause button, and stands still
 * (scrollable by hand) under prefers-reduced-motion. A click opens the instrument in Markets.
 */
@Component({
  selector: 'fs-ticker-tape',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, NgTemplateOutlet],
  template: `
    @if (rows().length) {
      <section class="tape" aria-label="Ticker tape" [class.paused]="paused()">
        <div class="tape-viewport">
          <div class="tape-track" [style.--tape-duration]="duration() + 's'">
            <ul class="tape-list" aria-label="Prices of tracked and starred instruments">
              @for (r of rows(); track r.key) { <li><ng-container *ngTemplateOutlet="item; context: { $implicit: r, copy: false }" /></li> }
            </ul>
            <!-- The second copy makes the loop seamless; it is hidden from assistive technology and the keyboard. -->
            <ul class="tape-list tape-copy" aria-hidden="true" inert>
              @for (r of rows(); track r.key) { <li><ng-container *ngTemplateOutlet="item; context: { $implicit: r, copy: true }" /></li> }
            </ul>
          </div>
        </div>
        <button type="button" class="tape-toggle" (click)="togglePause()" [attr.aria-pressed]="paused()" [attr.aria-label]="paused() ? 'Play ticker tape' : 'Pause ticker tape'" [title]="paused() ? 'Play ticker tape' : 'Pause ticker tape'">
          <i class="bi" [class.bi-play-fill]="paused()" [class.bi-pause-fill]="!paused()" aria-hidden="true"></i>
        </button>
      </section>
    }
    <ng-template #item let-r let-copy="copy">
      <button type="button" class="tape-item" [attr.tabindex]="copy ? -1 : null" (click)="open(r)" [attr.data-symbol]="r.symbol"
              [attr.aria-label]="r.symbol + ' ' + (r.price | number: '1.2-4') + ' ' + (r.quote?.currency ?? '') + (r.dayPct === null ? '' : ', ' + pct(r) + ' today') + '. Open in Markets'">
        @if (r.favorite) { <i class="bi bi-star-fill tape-star" aria-hidden="true"></i> }
        <span class="tape-sym">{{ r.symbol }}</span>
        <span class="tape-price tabular" [class]="flash(r)">{{ r.price | number: '1.2-2' }}</span>
        @if (r.dayPct !== null) {
          <span class="tape-chg tabular" [class.up]="r.dayPct > 0" [class.down]="r.dayPct < 0">
            <i class="bi" [class.bi-caret-up-fill]="r.dayPct > 0" [class.bi-caret-down-fill]="r.dayPct < 0" [class.bi-dash]="r.dayPct === 0" aria-hidden="true"></i>{{ pct(r) }}
          </span>
        }
        @if (r.live) { <span class="fs-pulse tape-live" aria-hidden="true"></span> }
      </button>
    </ng-template>
  `,
  styles: [`
    :host { display: block; position: sticky; top: var(--fs-topbar-height); z-index: 1020; }
    .tape {
      display: flex; align-items: stretch; height: 2.1rem; border-bottom: 1px solid var(--fs-border);
      background: color-mix(in srgb, var(--fs-surface) 88%, transparent); backdrop-filter: saturate(160%) blur(10px);
      animation: fs-fade var(--fs-dur-3) ease-out backwards;
    }
    .tape-viewport {
      flex: 1 1 auto; min-width: 0; overflow: hidden;
      mask-image: linear-gradient(to right, transparent 0, #000 2.5rem, #000 calc(100% - 2.5rem), transparent 100%);
    }
    .tape-track { display: flex; width: max-content; height: 100%; animation: fs-marquee var(--tape-duration, 40s) linear infinite; }
    .tape:hover .tape-track, .tape:focus-within .tape-track, .tape.paused .tape-track { animation-play-state: paused; }
    .tape-list { display: flex; align-items: center; list-style: none; margin: 0; padding: 0 0.75rem 0 0; gap: 0.25rem; }
    .tape-item {
      display: inline-flex; align-items: center; gap: 0.4rem; height: 1.7rem; padding: 0 0.6rem; border: 0; border-radius: 999px;
      background: transparent; color: var(--fs-text); font-size: 0.78rem; white-space: nowrap;
      transition: background-color var(--fs-dur-1) ease;
      &:hover, &:focus-visible { background: var(--fs-surface-3); }
    }
    .tape-star { color: var(--fs-warn); font-size: 0.65rem; }
    .tape-sym { font-weight: 700; letter-spacing: 0.01em; }
    .tape-price { color: var(--fs-text-2); border-radius: 4px; padding: 0 0.15rem; }
    .tape-chg { display: inline-flex; align-items: center; gap: 0.1rem; font-weight: 650; color: var(--fs-muted); .bi { font-size: 0.6rem; } &.up { color: var(--fs-pos); } &.down { color: var(--fs-neg); } }
    .tape-live { width: 6px; height: 6px; }
    .tape-toggle {
      flex: 0 0 auto; width: 2.1rem; border: 0; border-left: 1px solid var(--fs-border); background: transparent; color: var(--fs-muted);
      &:hover { color: var(--fs-text); background: var(--fs-surface-3); }
    }
    @media (prefers-reduced-motion: reduce) {
      .tape-viewport { overflow-x: auto; mask-image: none; scrollbar-width: none; }
      .tape-track { animation: none; }
      .tape-copy, .tape-toggle { display: none; }
    }
    @media (max-width: 991.98px) { :host { top: 0; position: relative; } }
  `],
})
export class TickerTapeComponent {
  private readonly board = inject(QuoteBoardService);
  private readonly market = inject(MarketState);
  private readonly router = inject(Router);

  readonly rows = this.board.priced;
  readonly paused = signal(this.readPaused());
  /** About five seconds per instrument, so the speed stays readable however long the band is. */
  readonly duration = computed(() => Math.max(30, this.rows().length * 5));

  open(r: BoardRow): void {
    this.market.view({ symbol: r.symbol, provider: r.provider });
    void this.router.navigate(['/markets']);
  }

  togglePause(): void {
    this.paused.update(v => !v);
    try { localStorage.setItem(PAUSE_KEY, this.paused() ? '1' : '0'); } catch { /* the choice lasts for this visit */ }
  }

  pct(r: BoardRow): string { return signedPercent(r.dayPct); }
  flash(r: BoardRow): string { return flashClass(r.entry); }

  private readPaused(): boolean {
    try { return localStorage.getItem(PAUSE_KEY) === '1'; } catch { return false; }
  }
}
