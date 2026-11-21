import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { Router, RouterLink } from '@angular/router';

import { MarketState } from '../core/market-state';
import { utcClock } from '../core/market-hours';
import { type PriceAlert, alertInstrumentKey, distancePercent, formatPrice, proximity } from '../core/price-alerts';
import { PriceAlertsService } from '../core/price-alerts.service';
import { signedPercent } from '../core/quote-board';
import { QuoteBoardService } from '../core/quote-board.service';

interface AlertRow { alert: PriceAlert; price: number; near: number; status: string }

/** Every price alert with how near the price is to its level, and the ones that were hit. */
@Component({
  selector: 'fse-price-alerts',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, RouterLink],
  template: `
    <section class="card alerts" aria-labelledby="alerts-title">
      <div class="card-header">
        <h2 class="card-title me-auto" id="alerts-title"><i class="bi bi-bell me-2 text-primary" aria-hidden="true"></i>Price alerts</h2>
        @if (svc.triggered().length) { <button type="button" class="btn btn-sm btn-ghost" (click)="svc.clearTriggered()">Clear hits</button> }
      </div>
      <div class="card-body">
        @if (!rows().length) {
          <p class="text-muted small mb-0">No alerts yet. Set one with the bell beside the price in <a routerLink="/markets">Markets</a>.</p>
        } @else {
          <ul class="alert-rows fs-stagger" aria-label="Price alerts">
            @for (r of rows(); track r.alert.id) {
              <li class="alert-row" [attr.data-alert]="r.alert.symbol" [attr.data-state]="r.alert.triggered_at ? 'hit' : 'active'">
                <span class="alert-icon" aria-hidden="true"><i class="bi" [class.bi-bell-fill]="!!r.alert.triggered_at" [class.bi-bell]="!r.alert.triggered_at"></i></span>
                <button type="button" class="alert-main" (click)="open(r.alert)" [attr.aria-label]="label(r)">
                  <span class="alert-sym">{{ r.alert.symbol }}</span>
                  <span class="alert-level">{{ r.alert.direction === 'above' ? 'Above' : 'Below' }} <strong class="tabular">{{ r.alert.level | number: '1.2-4' }}</strong></span>
                  <span class="alert-status">{{ r.status }}</span>
                  <span class="alert-prox" aria-hidden="true"><span class="fs-grow-x" [style.width.%]="r.near * 100"></span></span>
                </button>
                <button type="button" class="btn btn-sm btn-ghost" (click)="svc.remove(r.alert.id)" [attr.aria-label]="'Remove alert for ' + r.alert.symbol + ' ' + r.alert.direction + ' ' + fmt(r.alert.level)"><i class="bi bi-x-lg" aria-hidden="true"></i></button>
              </li>
            }
          </ul>
          <p class="alerts-note">Kept in this browser; checked against provider quotes while FloweySales is open. Alerts never place orders.</p>
        }
      </div>
    </section>
  `,
  styles: [`
    :host { display: block; }
    .alert-rows { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
    .alert-row { display: grid; grid-template-columns: 1.6rem 1fr auto; align-items: center; gap: 0.4rem; }
    .alert-icon { display: grid; place-items: center; width: 1.6rem; height: 1.6rem; border-radius: 50%; background: var(--fs-surface-3); color: var(--fs-muted); font-size: 0.8rem; }
    .alert-main {
      display: grid; grid-template-columns: auto 1fr; grid-template-areas: 'sym status' 'level level' 'prox prox'; gap: 0.1rem 0.5rem; padding: 0.3rem 0.45rem;
      border: 0; border-radius: 0.5rem; background: transparent; color: var(--fs-text); text-align: left; font-size: 0.8rem;
      transition: background-color var(--fs-dur-1) ease;
      &:hover, &:focus-visible { background: var(--fs-surface-3); }
    }
    .alert-sym { grid-area: sym; font-weight: 700; }
    .alert-status { grid-area: status; justify-self: end; color: var(--fs-muted); font-size: 0.72rem; font-variant-numeric: tabular-nums; }
    .alert-level { grid-area: level; color: var(--fs-text-2); }
    .alert-prox { grid-area: prox; height: 0.3rem; margin-top: 0.2rem; border-radius: 999px; background: var(--fs-surface-3); overflow: hidden;
      span { display: block; height: 100%; border-radius: inherit; background: linear-gradient(90deg, color-mix(in srgb, var(--fs-warn) 45%, transparent), var(--fs-warn)); } }
    .alert-row[data-state='hit'] {
      .alert-icon { color: #1b1300; background: var(--fs-warn); animation: alert-ring 1.2s var(--fs-ease-out) 2; }
      .alert-status { color: var(--fs-warn); font-weight: 650; }
    }
    @keyframes alert-ring { 0%, 100% { transform: rotate(0); } 15% { transform: rotate(14deg); } 30% { transform: rotate(-12deg); } 45% { transform: rotate(8deg); } 60% { transform: rotate(-4deg); } }
    .alerts-note { margin: 0.7rem 0 0; font-size: 0.7rem; color: var(--fs-muted); }
    @media (prefers-reduced-motion: reduce) { .alert-row[data-state='hit'] .alert-icon { animation: none; } }
  `],
})
export class PriceAlertsCardComponent {
  readonly svc = inject(PriceAlertsService);
  private readonly board = inject(QuoteBoardService);
  private readonly market = inject(MarketState);
  private readonly router = inject(Router);

  readonly rows = computed<AlertRow[]>(() => {
    const prices = new Map(this.board.watchedRows().map(r => [r.key, r.price]));
    return [...this.svc.triggered(), ...this.svc.active()].map(alert => {
      const price = prices.get(alertInstrumentKey(alert)) ?? 0;
      const status = alert.triggered_at
        ? `Hit at ${formatPrice(alert.triggered_price ?? 0)} · ${utcClock(alert.triggered_at)} UTC`
        : price > 0 ? `${signedPercent(distancePercent(alert.level, price))} to go` : 'Waiting for a price';
      return { alert, price, near: proximity(alert, price), status };
    });
  });

  label(r: AlertRow): string {
    return `${r.alert.symbol} ${r.alert.direction} ${formatPrice(r.alert.level)}, ${r.status}. Open in Markets`;
  }

  fmt(n: number): string { return formatPrice(n); }

  open(a: PriceAlert): void {
    this.market.view({ symbol: a.symbol, provider: a.provider });
    void this.router.navigate(['/markets']);
  }
}
