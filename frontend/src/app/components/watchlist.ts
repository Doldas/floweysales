import { ChangeDetectionStrategy, Component, computed, inject, input, output } from '@angular/core';
import { DecimalPipe, NgTemplateOutlet } from '@angular/common';

import type { TrackedAsset } from '../services';
import { quoteBadge } from '../core/quote-badge';
import { boardKey, flashClass, heatLevel, signedPercent } from '../core/quote-board';
import { type BoardRow, QuoteBoardService } from '../core/quote-board.service';
import { SparklineComponent } from '../ui/sparkline';

/**
 * The tracked instruments with their latest price, the day's change, a five-day trend line and how
 * fresh the price is; starred instruments that are not tracked follow in their own list. Click a row
 * to view it; the selected instrument shows the live tick when there is one. Prices come from the
 * shared quote board, one request per instrument a minute while the tab is visible.
 */
@Component({
  selector: 'fse-watchlist',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, NgTemplateOutlet, SparklineComponent],
  template: `
    <div class="wl-head"><span class="wl-title"><i class="bi bi-eye" aria-hidden="true"></i> Watchlist</span><span class="wl-count tabular">{{ assets().length }}</span></div>
    @if (!assets().length && !starred().length) {
      <p class="wl-empty">Nothing tracked yet. Search for a ticker to add it.</p>
    } @else {
      @if (assets().length) {
        <ul class="wl-rows" aria-label="Watchlist prices">
          @for (a of assets(); track key(a)) {
            <li><ng-container *ngTemplateOutlet="rowTpl; context: { $implicit: a, r: rowOf(a) }" /></li>
          }
        </ul>
      }
      @if (starred().length) {
        <div class="wl-sub"><i class="bi bi-star-fill" aria-hidden="true"></i> Starred</div>
        <ul class="wl-rows" aria-label="Starred prices">
          @for (r of starred(); track r.key) {
            <li><ng-container *ngTemplateOutlet="rowTpl; context: { $implicit: r, r: r }" /></li>
          }
        </ul>
      }
    }
    <ng-template #rowTpl let-a let-r="r">
      <button type="button" class="wl-row" [class.selected]="isSelected(a)" [attr.aria-pressed]="isSelected(a)" [attr.data-symbol]="a.symbol" (click)="view.emit({ symbol: a.symbol, provider: a.provider })" [title]="'View ' + a.symbol + ' on ' + a.provider">
        <span class="wl-id">
          <strong>{{ a.symbol }}</strong>
          @if (badgeOf(r); as b) {
            <small><span class="wl-badge" [attr.data-tone]="b.tone">{{ b.text }}</span></small>
          } @else {
            <small>{{ a.provider }}</small>
          }
        </span>
        <fs-sparkline class="wl-spark" [values]="r?.quote?.spark ?? []" [width]="64" [height]="22" [label]="a.symbol + ' five-day trend'" />
        @if (r?.price) {
          <span class="wl-price tabular" [class]="flash(r)">{{ r.price | number: '1.2-4' }} <small>{{ r.quote?.currency }}</small></span>
          <span class="wl-day tabular" [attr.data-heat]="heat(r)">{{ pct(r) }}</span>
        } @else if (r?.entry?.failed) {
          <span class="wl-price muted">No price</span>
        } @else {
          <span class="wl-price muted"><span class="fs-shimmer wl-wait" aria-hidden="true"></span><span class="visually-hidden">Loading price</span></span>
        }
      </button>
    </ng-template>
  `,
  styles: [`
    :host { display: block; }
    .wl-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.4rem; font-weight: 650; font-size: 0.85rem; .bi { color: var(--fs-accent-text); margin-right: 0.35rem; } .wl-count { color: var(--fs-muted); font-weight: 500; } }
    .wl-sub { margin: 0.6rem 0 0.25rem; font-size: 0.72rem; font-weight: 650; text-transform: uppercase; letter-spacing: 0.04em; color: var(--fs-muted); .bi { color: var(--fs-warn, #d4a21a); margin-right: 0.25rem; } }
    .wl-empty { color: var(--fs-muted); font-size: 0.8rem; margin: 0; }
    .wl-rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.2rem; }
    .wl-rows > li { animation: fs-rise var(--fs-dur-3, 0.36s) var(--fs-ease-out, ease-out) both; }
    @for $i from 1 through 12 { .wl-rows > li:nth-child(#{$i}) { animation-delay: #{($i - 1) * 35}ms; } }
    .wl-row { width: 100%; display: grid; grid-template-columns: minmax(0, 1fr) 64px auto; grid-template-areas: 'id spark price' 'id spark day'; gap: 0 0.6rem; align-items: center; text-align: left; padding: 0.4rem 0.55rem; border: 1px solid transparent; border-radius: var(--bs-border-radius); background: transparent; color: var(--fs-text); position: relative; transition: background-color var(--fs-dur-1, 0.12s) ease, border-color var(--fs-dur-1, 0.12s) ease, transform var(--fs-dur-1, 0.12s) ease; }
    .wl-row:hover { background: var(--fs-surface-2); transform: translateX(2px); }
    .wl-row.selected { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
    .wl-row.selected::before { content: ''; position: absolute; left: -1px; top: 20%; bottom: 20%; width: 3px; border-radius: 3px; background: var(--fs-accent); }
    .wl-id { grid-area: id; display: flex; flex-direction: column; min-width: 0; strong { font-size: 0.85rem; overflow: hidden; text-overflow: ellipsis; } small { color: var(--fs-muted); font-size: 0.68rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; } }
    .wl-spark { grid-area: spark; height: 22px; opacity: 0.9; }
    .wl-price { grid-area: price; text-align: right; font-weight: 600; font-size: 0.85rem; border-radius: 4px; padding: 0 0.2rem; small { color: var(--fs-muted); font-weight: 500; font-size: 0.68rem; } &.muted { color: var(--fs-muted); font-weight: 400; grid-row: 1 / span 2; } }
    .wl-wait { display: inline-block; width: 3.5rem; height: 0.8rem; border-radius: 4px; vertical-align: middle; }
    .wl-day { grid-area: day; justify-self: end; font-size: 0.7rem; font-weight: 650; padding: 0 0.35rem; border-radius: 999px; color: var(--fs-muted); background: var(--fs-surface-2); }
    .wl-day[data-heat^='-'] { color: var(--fs-neg); background: var(--fs-neg-soft); }
    .wl-day[data-heat='1'], .wl-day[data-heat='2'], .wl-day[data-heat='3'] { color: var(--fs-pos); background: var(--fs-pos-soft); }
    .wl-badge[data-tone='live'] { color: var(--fs-pos); }
    .wl-badge[data-tone='stale'] { color: var(--fs-neg); }
    .wl-badge[data-tone='closed'] { font-style: italic; }
  `],
})
export class WatchlistComponent {
  private readonly board = inject(QuoteBoardService);

  readonly assets = input.required<TrackedAsset[]>();
  readonly selected = input<TrackedAsset | null>(null);
  readonly view = output<TrackedAsset>();

  readonly key = boardKey;
  /** Starred instruments that are not tracked. */
  readonly starred = computed(() => {
    const tracked = new Set(this.assets().map(boardKey));
    return this.board.rows().filter(r => r.favorite && !tracked.has(r.key));
  });
  private readonly byKey = computed(() => new Map(this.board.rows().map(r => [r.key, r])));

  rowOf(a: TrackedAsset): BoardRow | undefined { return this.byKey().get(boardKey(a)); }

  isSelected(a: TrackedAsset): boolean {
    const s = this.selected();
    return !!s && boardKey(s) === boardKey(a);
  }

  /** How fresh the price is: the live tick for the instrument on screen, else the quote's age. */
  badgeOf(r: BoardRow | undefined): { text: string; tone: string } | null {
    if (r?.live) return { text: 'live tick', tone: 'live' };
    return r?.quote ? quoteBadge(r.quote) : null;
  }

  pct(r: BoardRow): string { return signedPercent(r.dayPct); }
  heat(r: BoardRow): number | null { return heatLevel(r.dayPct); }
  flash(r: BoardRow): string { return flashClass(r.entry); }
}
