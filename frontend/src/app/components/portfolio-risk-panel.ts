import { ChangeDetectionStrategy, Component, computed, inject, output, signal } from '@angular/core';

import { OrdersState } from '../orders-state';
import type { PortfolioAnalyticsResponse } from '../models';
import { MarketService, describeHttpError } from '../services';
import { formatMoney } from '../ui/format';
import { CLASS_LABELS, alertText, driftRows, driftText, heatStyle, riskBars, riskTiles, ticketFor, type TicketPrefill } from './portfolio-risk';

/**
 * Risk of the paper portfolio: concentration, volatility, beta, correlation,
 * risk contribution and drift from the wealth plan. Figures come from the
 * server (internal/portfolio); suggestions are text and only prefill the
 * order ticket, they never submit.
 */
@Component({
  selector: 'fs-portfolio-risk',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<div class="risk">
  @if (loading()) { <div class="progress rounded-0" style="height: 2px" role="progressbar" aria-label="Measuring risk"><div class="progress-bar progress-bar-striped progress-bar-animated w-100"></div></div> }
  @if (error(); as err) {
    <div class="fs-empty" role="alert"><i class="bi bi-exclamation-triangle"></i><h3>Could not measure risk</h3><p class="mb-0">{{ err }}</p>
      <button type="button" class="btn btn-sm btn-outline-secondary mt-1" (click)="load()">Try again</button></div>
  } @else if (data(); as d) {
    @if (d.analytics.total_value <= 0) {
      <div class="fs-empty"><i class="bi bi-wallet2"></i><h3>Nothing to measure yet</h3><p class="mb-0">Place a paper order and the portfolio's risk shows up here.</p></div>
    } @else {
      <section class="tiles" aria-label="Risk summary">
        @for (t of tiles(); track t.id) {
          <div class="card fs-kpi" [title]="t.hint"><span class="fs-kpi-label">{{ t.label }}</span><span class="fs-kpi-value">{{ t.value }}</span></div>
        }
      </section>

      @if (d.analytics.alerts.length) {
        <div class="alert alert-warning" role="note"><strong>Worth a look</strong>
          <ul class="mb-0">@for (a of d.analytics.alerts; track a) { <li>{{ alertText(a) }}</li> }</ul></div>
      }

      <div class="grid">
        <section class="card" aria-labelledby="cls-title">
          <div class="card-header"><h3 class="card-title" id="cls-title">Asset classes vs plan</h3></div>
          <div class="card-body">
            <table class="table table-sm mb-0">
              <caption class="visually-hidden">Class weights and drift from the target allocation</caption>
              <thead><tr><th scope="col">Class</th><th scope="col" class="num">Now</th><th scope="col" class="num">Target</th><th scope="col" class="num">Drift</th></tr></thead>
              <tbody>
                @for (r of drift(); track r.id) {
                  <tr><th scope="row">{{ r.label }}</th>
                    <td class="num">{{ r.actual }}%<span class="bar" aria-hidden="true"><span [style.width.%]="r.actual"></span></span></td>
                    <td class="num">{{ r.target !== undefined ? r.target + '%' : '–' }}</td><td class="num">{{ driftText(r.drift) }}</td></tr>
                }
              </tbody>
            </table>
            @if (!d.has_plan) { <p class="note mb-0">Save a wealth plan to see drift and rebalancing suggestions.</p> }
          </div>
        </section>

        <section class="card" aria-labelledby="rc-title">
          <div class="card-header"><h3 class="card-title" id="rc-title">Share of risk</h3></div>
          <div class="card-body">
            @if (bars().length && d.analytics.volatility_pct !== undefined) {
              <table class="table table-sm mb-0">
                <caption class="visually-hidden">Each holding's weight and share of portfolio risk</caption>
                <thead><tr><th scope="col">Holding</th><th scope="col" class="num">Weight</th><th scope="col" class="num">Risk</th></tr></thead>
                <tbody>@for (b of bars(); track b.symbol) {
                  <tr><th scope="row">{{ b.symbol }}</th><td class="num">{{ b.weight }}%</td><td class="num">{{ b.risk }}%<span class="bar" aria-hidden="true"><span [style.width.%]="b.risk"></span></span></td></tr>
                }</tbody>
              </table>
            } @else { <p class="note mb-0">Not enough shared price history to split the risk.</p> }
          </div>
        </section>
      </div>

      @if (d.analytics.correlation.length > 1) {
        <section class="card" aria-labelledby="corr-title">
          <div class="card-header"><h3 class="card-title" id="corr-title">Correlation</h3></div>
          <div class="fs-scroll-x"><table class="table table-sm mb-0 heat">
            <caption class="visually-hidden">Daily return correlation between holdings</caption>
            <thead><tr><td></td>@for (s of d.analytics.symbols; track s) { <th scope="col">{{ s }}</th> }</tr></thead>
            <tbody>@for (row of d.analytics.correlation; track $index) {
              <tr><th scope="row">{{ d.analytics.symbols[$index] }}</th>
                @for (c of row; track $index) { <td class="num" [style.background]="heat(c)">{{ c.toFixed(2) }}</td> }</tr>
            }</tbody>
          </table></div>
        </section>
      }

      @if (d.analytics.suggestions.length) {
        <section class="card" aria-labelledby="sug-title">
          <div class="card-header"><h3 class="card-title" id="sug-title">Rebalancing suggestions</h3></div>
          <div class="fs-scroll-x"><table class="table table-sm align-middle mb-0">
            <caption class="visually-hidden">Suggested trades to return to the target allocation</caption>
            <thead><tr><th scope="col">Action</th><th scope="col">Symbol</th><th scope="col" class="num">Amount</th><th scope="col">Why</th><th scope="col"><span class="visually-hidden">Ticket</span></th></tr></thead>
            <tbody>@for (s of d.analytics.suggestions; track s.symbol + s.side) {
              <tr><td>{{ s.side === 'sell' ? 'Sell' : 'Buy' }}</td><th scope="row">{{ s.symbol }}</th><td class="num">{{ money(s.amount) }}</td><td>{{ s.reason }}</td>
                <td>@if (ticket(s); as t) { <button type="button" class="btn btn-sm btn-outline-secondary" (click)="prefill.emit(t)" [attr.aria-label]="'Prefill ticket to ' + s.side + ' ' + t.quantity + ' ' + s.symbol">Prefill ticket</button> }</td></tr>
            }</tbody>
          </table></div>
          <p class="note m-3">Suggestions are text. Nothing is ordered until you review and confirm a ticket.</p>
        </section>
      }
      <p class="note">Measured over up to a year of daily returns at today's weights, against {{ d.benchmark }}. Research aid, not investment advice.</p>
    }
  }
</div>
  `,
  styles: [`
:host { display: block; }
.risk { display: grid; gap: 1rem; }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr)); gap: 0.6rem; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr)); gap: 1rem; }
.num { text-align: right; white-space: nowrap; }
.note { font-size: 0.78rem; color: var(--fs-muted); }
.bar { display: inline-block; width: 4rem; height: 6px; margin-left: 0.5rem; border-radius: 3px; background: var(--fs-surface-3); vertical-align: middle; overflow: hidden;
  span { display: block; height: 100%; background: var(--fs-accent); } }
.heat td, .heat th { text-align: center; font-variant-numeric: tabular-nums; }
  `],
})
export class PortfolioRiskComponent {
  private readonly api = inject(MarketService);
  private readonly orders = inject(OrdersState);

  /** Fires when the user asks to prefill the order ticket; the page applies it. */
  readonly prefill = output<TicketPrefill>();

  readonly data = signal<PortfolioAnalyticsResponse | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  readonly alertText = alertText;
  readonly driftText = driftText;
  readonly labels = CLASS_LABELS;

  readonly tiles = computed(() => (this.data() ? riskTiles(this.data()!.analytics, v => this.money(v)) : []));
  readonly drift = computed(() => (this.data() ? driftRows(this.data()!.analytics) : []));
  readonly bars = computed(() => (this.data() ? riskBars(this.data()!.analytics) : []));

  constructor() { this.load(); }

  money = (v: number): string => formatMoney(v, '');
  heat = (c: number): string => heatStyle(c);

  ticket(s: { symbol: string; side: 'buy' | 'sell'; amount: number; reason: string }): TicketPrefill | null {
    const position = this.orders.positions().find(p => p.symbol === s.symbol);
    return ticketFor(s, position ? this.orders.markOf(position.symbol, position.last_price) : undefined);
  }

  load(): void {
    this.loading.set(true);
    this.error.set('');
    this.api.getPortfolioAnalytics().subscribe({
      next: res => { this.data.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.loading.set(false); },
    });
  }
}
