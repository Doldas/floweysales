import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { DecimalPipe, NgTemplateOutlet } from '@angular/common';
import { Router, RouterLink } from '@angular/router';

import { MarketState } from '../core/market-state';
import { utcClock } from '../core/market-hours';
import { type Mover, flashClass, heatLevel, signedPercent, topMovers } from '../core/quote-board';
import { type BoardRow, QuoteBoardService } from '../core/quote-board.service';

/** The heat legend, from the strongest fall to the strongest rise (see heatLevel). */
const LEGEND = [-3, -2, -1, 0, 1, 2, 3];

/**
 * The day at a glance for every tracked and starred instrument: a heatmap whose tiles are coloured
 * by the day's change, and the biggest gainers and losers with bars scaled to the largest move.
 * Prices and changes come from the shared quote board; a click opens the instrument in Markets.
 */
@Component({
  selector: 'fse-market-pulse',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, NgTemplateOutlet, RouterLink],
  template: `
    <section class="card pulse" aria-labelledby="pulse-title">
      <div class="card-header">
        <h2 class="card-title me-auto" id="pulse-title"><i class="bi bi-grid-3x3-gap me-2 text-primary" aria-hidden="true"></i>Market pulse</h2>
        @if (updated()) { <span class="pulse-updated small text-muted">Updated {{ updated() }} UTC</span> }
        @if (board.instruments().length) {
          <button type="button" class="btn btn-sm btn-ghost" (click)="board.refresh()" aria-label="Refresh prices" title="Refresh prices"><i class="bi bi-arrow-clockwise" aria-hidden="true"></i></button>
        }
      </div>
      <div class="card-body">
        @if (!board.instruments().length) {
          <p class="text-muted small mb-0">Track or star instruments to see how their day is going. <a routerLink="/markets">Open Markets</a></p>
        } @else if (!rows().length) {
          <div class="heat-grid" [attr.aria-busy]="true">
            <span class="visually-hidden" role="status">Loading prices</span>
            @for (i of board.instruments(); track $index) { <span class="heat-tile fs-shimmer" aria-hidden="true"></span> }
          </div>
        } @else {
          <ul class="heat-grid fs-stagger" aria-label="Day change heatmap">
            @for (r of rows(); track r.key) {
              <li>
                <button type="button" class="heat-tile" [attr.data-symbol]="r.symbol" [attr.data-heat]="heat(r)" (click)="open(r)"
                        [attr.aria-label]="tileLabel(r)">
                  <span class="heat-sym">@if (r.favorite) { <i class="bi bi-star-fill" aria-hidden="true"></i> }{{ r.symbol }}</span>
                  <span class="heat-pct tabular">{{ pct(r.dayPct) }}</span>
                  <span class="heat-price tabular" [class]="flash(r)">{{ r.price | number: '1.2-2' }} <small>{{ r.quote?.currency }}</small></span>
                </button>
              </li>
            }
          </ul>
          <div class="heat-legend" aria-hidden="true">
            <span>−2.5%</span>
            @for (l of legend; track l) { <i [attr.data-heat]="l"></i> }
            <span>+2.5%</span>
          </div>

          <div class="movers">
            <div class="movers-col">
              <h3 class="movers-title"><i class="bi bi-arrow-up-right text-pos" aria-hidden="true"></i>Top gainers</h3>
              @if (movers().gainers.length) {
                <ol class="movers-list" aria-label="Top gainers today">
                  @for (m of movers().gainers; track m.item.key) { <li><ng-container [ngTemplateOutlet]="mover" [ngTemplateOutletContext]="{ $implicit: m }" /></li> }
                </ol>
              } @else { <p class="movers-empty">Nothing is up today.</p> }
            </div>
            <div class="movers-col">
              <h3 class="movers-title"><i class="bi bi-arrow-down-right text-neg" aria-hidden="true"></i>Top losers</h3>
              @if (movers().losers.length) {
                <ol class="movers-list" aria-label="Top losers today">
                  @for (m of movers().losers; track m.item.key) { <li><ng-container [ngTemplateOutlet]="mover" [ngTemplateOutletContext]="{ $implicit: m }" /></li> }
                </ol>
              } @else { <p class="movers-empty">Nothing is down today.</p> }
            </div>
          </div>
          <p class="pulse-note">Day change from each quote's previous close. Provider bar closes, not executable prices.</p>
        }
      </div>
    </section>

    <ng-template #mover let-m>
      <button type="button" class="mover" [class.down]="m.pct < 0" (click)="open(m.item)" [attr.data-symbol]="m.item.symbol">
        <span class="mover-sym">{{ m.item.symbol }}</span>
        <span class="mover-bar" aria-hidden="true"><span class="fs-grow-x" [style.width.%]="barWidth(m)"></span></span>
        <span class="mover-pct tabular">{{ pct(m.pct) }}</span>
      </button>
    </ng-template>
  `,
  styles: [`
    :host { display: block; }
    .pulse-updated { font-variant-numeric: tabular-nums; }
    .heat-grid { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(min(7.25rem, 100%), 1fr)); gap: 0.5rem; }
    .heat-tile {
      --heat: var(--fs-surface-3);
      display: flex; flex-direction: column; align-items: flex-start; gap: 0.1rem; width: 100%; min-height: 4.6rem; padding: 0.55rem 0.7rem;
      border: 1px solid color-mix(in srgb, var(--fs-text) 8%, transparent); border-radius: 0.7rem; color: var(--fs-text); text-align: left;
      background: linear-gradient(160deg, color-mix(in srgb, var(--heat) 100%, transparent), color-mix(in srgb, var(--heat) 70%, var(--fs-surface)));
      transition: transform var(--fs-dur-2) var(--fs-ease-out), box-shadow var(--fs-dur-2) ease, border-color var(--fs-dur-2) ease;
      &:hover, &:focus-visible { transform: translateY(-2px) scale(1.02); box-shadow: var(--fs-shadow); border-color: color-mix(in srgb, var(--fs-text) 22%, transparent); }
    }
    span.heat-tile { display: block; border: 0; }
    .heat-sym { font-weight: 750; font-size: 0.82rem; letter-spacing: 0.01em; .bi { font-size: 0.6rem; color: var(--fs-warn); margin-right: 0.25rem; vertical-align: 0.1em; } }
    .heat-pct { font-size: 1.15rem; font-weight: 750; line-height: 1.15; }
    .heat-price { font-size: 0.72rem; color: var(--fs-text-2); border-radius: 4px; small { color: var(--fs-muted); font-size: 0.65rem; } }
    [data-heat='-3'] { --heat: color-mix(in srgb, var(--fs-neg) 55%, var(--fs-surface)); }
    [data-heat='-2'] { --heat: color-mix(in srgb, var(--fs-neg) 34%, var(--fs-surface)); }
    [data-heat='-1'] { --heat: color-mix(in srgb, var(--fs-neg) 16%, var(--fs-surface)); }
    [data-heat='0'] { --heat: var(--fs-surface-3); }
    [data-heat='1'] { --heat: color-mix(in srgb, var(--fs-pos) 16%, var(--fs-surface)); }
    [data-heat='2'] { --heat: color-mix(in srgb, var(--fs-pos) 34%, var(--fs-surface)); }
    [data-heat='3'] { --heat: color-mix(in srgb, var(--fs-pos) 55%, var(--fs-surface)); }
    .heat-legend {
      display: flex; align-items: center; justify-content: flex-end; gap: 2px; margin: 0.6rem 0 0; font-size: 0.65rem; color: var(--fs-muted);
      span { margin: 0 0.35rem; font-variant-numeric: tabular-nums; }
      i { width: 1.1rem; height: 0.45rem; border-radius: 2px; background: var(--heat); }
    }
    .movers { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(13rem, 100%), 1fr)); gap: 0.5rem 1.5rem; margin-top: 1rem; padding-top: 0.9rem; border-top: 1px solid var(--fs-border); }
    .movers-title { display: flex; align-items: center; gap: 0.35rem; margin: 0 0 0.4rem; font-size: 0.75rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; color: var(--fs-muted); }
    .movers-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.2rem; }
    .movers-empty { margin: 0; font-size: 0.8rem; color: var(--fs-muted); }
    .mover {
      display: grid; grid-template-columns: minmax(0, 5.5rem) minmax(1.5rem, 1fr) auto; align-items: center; gap: 0.6rem; width: 100%; padding: 0.3rem 0.4rem;
      border: 0; border-radius: 0.45rem; background: transparent; color: var(--fs-text); font-size: 0.82rem; text-align: left;
      transition: background-color var(--fs-dur-1) ease;
      &:hover, &:focus-visible { background: var(--fs-surface-3); }
    }
    .mover-sym { font-weight: 650; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .mover-bar { height: 0.4rem; border-radius: 999px; background: var(--fs-surface-3); overflow: hidden; span { display: block; height: 100%; border-radius: inherit; background: linear-gradient(90deg, color-mix(in srgb, var(--fs-pos) 55%, transparent), var(--fs-pos)); } }
    .mover.down .mover-bar span { background: linear-gradient(90deg, color-mix(in srgb, var(--fs-neg) 55%, transparent), var(--fs-neg)); }
    .mover-pct { text-align: right; font-weight: 650; color: var(--fs-pos); }
    .mover.down .mover-pct { color: var(--fs-neg); }
    .pulse-note { margin: 0.8rem 0 0; font-size: 0.7rem; color: var(--fs-muted); }
    @media (prefers-reduced-motion: reduce) { .heat-tile:hover, .heat-tile:focus-visible { transform: none; } }
  `],
})
export class MarketPulseComponent {
  readonly board = inject(QuoteBoardService);
  private readonly market = inject(MarketState);
  private readonly router = inject(Router);

  readonly legend = LEGEND;
  readonly rows = this.board.priced;
  readonly movers = computed(() => topMovers(this.rows(), r => r.dayPct, 3));
  private readonly maxMove = computed(() => Math.max(0, ...[...this.movers().gainers, ...this.movers().losers].map(m => Math.abs(m.pct))));
  readonly updated = computed(() => (this.rows().length && this.board.refreshedAt() ? utcClock(this.board.refreshedAt()) : ''));

  heat(r: BoardRow): number | null { return heatLevel(r.dayPct); }
  pct(p: number | null): string { return signedPercent(p); }
  flash(r: BoardRow): string { return flashClass(r.entry); }
  barWidth(m: Mover<BoardRow>): number { return this.maxMove() ? Math.max(4, (Math.abs(m.pct) / this.maxMove()) * 100) : 0; }

  tileLabel(r: BoardRow): string {
    const change = r.dayPct === null ? 'no day change yet' : `${signedPercent(r.dayPct)} today`;
    return `${r.symbol}, ${change}, ${r.price.toFixed(2)} ${r.quote?.currency ?? ''}. Open in Markets`.replace(/ +\./, '.');
  }

  open(r: BoardRow): void {
    this.market.view({ symbol: r.symbol, provider: r.provider });
    void this.router.navigate(['/markets']);
  }
}
