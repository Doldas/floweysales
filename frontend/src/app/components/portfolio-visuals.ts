import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { DecimalPipe, PercentPipe } from '@angular/common';

import { OrdersState } from '../orders-state';
import { type Slice, allocation, conicStops, pnlBars } from '../core/portfolio-visuals';
import { signedPercent } from '../core/quote-board';

/**
 * The paper account at a glance: a donut of how equity is split between positions and cash, the
 * return since the starting cash, and each position's unrealised result as a bar either side of zero.
 */
@Component({
  selector: 'fse-portfolio-visuals',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, PercentPipe],
  template: `
    @if (summary(); as s) {
      <section class="card visuals" aria-labelledby="visuals-title">
        <div class="card-header">
          <h2 class="card-title me-auto" id="visuals-title"><i class="bi bi-pie-chart me-2 text-primary" aria-hidden="true"></i>Account at a glance</h2>
          <span class="ret" [class.up]="ret() >= 0" [class.down]="ret() < 0" title="Equity against the starting cash">
            {{ pct(ret()) }} <small>since start</small>
          </span>
        </div>
        <div class="card-body vis-grid">
          <div class="alloc">
            <div class="donut" role="img" [attr.aria-label]="donutLabel()" [style.--donut]="gradient()">
              <div class="donut-hole">
                <span class="hole-label">Equity</span>
                <strong class="hole-value tabular">{{ s.equity | number: '1.0-0' }}</strong>
                <span class="hole-cur">{{ currency() }}</span>
              </div>
            </div>
            <ul class="legend fs-stagger" aria-label="Allocation">
              @for (sl of slices(); track sl.label) {
                <li [attr.data-slice]="sl.label">
                  <i class="swatch" [style.background]="color(sl)" aria-hidden="true"></i>
                  <span class="legend-label">{{ sl.label }}@if (sl.short) { <small> short</small> }</span>
                  <span class="legend-share tabular">{{ sl.share | percent: '1.1-1' }}</span>
                </li>
              }
            </ul>
          </div>
          <div class="pnl">
            <h3 class="pnl-title">Unrealised P&amp;L</h3>
            @if (bars().length) {
              <ul class="pnl-list fs-stagger" aria-label="Unrealised result by position">
                @for (b of bars(); track b.symbol) {
                  <li class="pnl-row" [class.down]="b.pnl < 0" [attr.data-symbol]="b.symbol">
                    <span class="pnl-sym">{{ b.symbol }}</span>
                    <span class="pnl-track" aria-hidden="true"><span class="pnl-bar fs-grow-x" [style.width.%]="b.width / 2"></span></span>
                    <span class="pnl-val tabular">{{ b.pnl >= 0 ? '+' : '−' }}{{ abs(b.pnl) | number: '1.2-2' }}@if (b.pct !== null) { <small>{{ pct(b.pct) }}</small> }</span>
                  </li>
                }
              </ul>
            } @else {
              <p class="text-muted small mb-0">No open positions. Results show here once an order fills.</p>
            }
          </div>
        </div>
      </section>
    }
  `,
  styles: [`
    @property --sweep { syntax: '<number>'; inherits: false; initial-value: 1; }
    :host {
      display: block;
      --tone-0: var(--fs-accent); --tone-1: var(--fs-pos); --tone-2: var(--fs-warn); --tone-3: #a78bfa; --tone-4: #22b8cf; --tone-5: #94a3b8;
      --tone-cash: color-mix(in srgb, var(--fs-muted) 30%, var(--fs-surface-3));
    }
    .ret { font-weight: 650; padding: 0.15rem 0.55rem; border-radius: 999px; font-variant-numeric: tabular-nums; small { font-weight: 500; } }
    .ret.up { color: var(--fs-pos); background: var(--fs-pos-soft); }
    .ret.down { color: var(--fs-neg); background: var(--fs-neg-soft); }
    .vis-grid { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr); gap: 1.25rem 2rem; align-items: start; }
    @media (max-width: 767.98px) { .vis-grid { grid-template-columns: minmax(0, 1fr); } }
    .alloc { display: flex; flex-wrap: wrap; align-items: center; gap: 1rem 1.4rem; }
    .donut {
      position: relative; width: min(9.5rem, 100%); aspect-ratio: 1; border-radius: 50%; background: var(--donut); flex: none;
      -webkit-mask: conic-gradient(#000 calc(var(--sweep) * 1turn), transparent 0); mask: conic-gradient(#000 calc(var(--sweep) * 1turn), transparent 0);
      animation: donut-sweep var(--fs-dur-4) var(--fs-ease-out) backwards;
      transition: transform var(--fs-dur-2) var(--fs-ease-spring);
      &:hover { transform: scale(1.03); }
    }
    @keyframes donut-sweep { from { --sweep: 0; } }
    .donut-hole {
      position: absolute; inset: 18%; border-radius: 50%; background: var(--fs-surface); display: grid; place-content: center; text-align: center;
      box-shadow: inset 0 0 0 1px var(--fs-border); line-height: 1.15;
    }
    .hole-label, .hole-cur { font-size: 0.65rem; color: var(--fs-muted); text-transform: uppercase; letter-spacing: 0.06em; }
    .hole-value { font-size: 1.05rem; }
    .legend { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.3rem; min-width: 9rem; flex: 1; }
    .legend li { display: grid; grid-template-columns: 0.7rem 1fr auto; align-items: center; gap: 0.5rem; font-size: 0.8rem; }
    .swatch { width: 0.7rem; height: 0.7rem; border-radius: 3px; }
    .legend-label { font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; small { color: var(--fs-neg); font-weight: 500; } }
    .legend-share { color: var(--fs-text-2); }
    .pnl { container-type: inline-size; }
    .pnl-title { margin: 0 0 0.5rem; font-size: 0.75rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; color: var(--fs-muted); }
    .pnl-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
    .pnl-row { display: grid; grid-template-columns: minmax(0, 5rem) minmax(2rem, 1fr) auto; align-items: center; gap: 0.3rem 0.6rem; font-size: 0.82rem; }
    /* Narrow: symbol and result on one line, the bar full width beneath. */
    @container (max-width: 22rem) { .pnl-row { grid-template-columns: minmax(0, 1fr) auto; } .pnl-track { grid-column: 1 / -1; grid-row: 2; } }
    .pnl-sym { font-weight: 650; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .pnl-track { position: relative; height: 0.55rem; border-radius: 999px; background: var(--fs-surface-3); animation-delay: inherit;
      &::after { content: ''; position: absolute; left: 50%; top: -0.2rem; bottom: -0.2rem; width: 1px; background: var(--fs-border); } }
    .pnl-bar {
      position: absolute; top: 0; bottom: 0; left: 50%; border-radius: 0 999px 999px 0; animation-delay: inherit;
      background: linear-gradient(90deg, color-mix(in srgb, var(--fs-pos) 55%, transparent), var(--fs-pos));
    }
    .pnl-row.down .pnl-bar {
      left: auto; right: 50%; border-radius: 999px 0 0 999px; transform-origin: right center;
      background: linear-gradient(270deg, color-mix(in srgb, var(--fs-neg) 55%, transparent), var(--fs-neg));
    }
    .pnl-val { text-align: right; font-weight: 650; color: var(--fs-pos); small { display: block; white-space: nowrap; font-weight: 500; font-size: 0.7rem; color: var(--fs-muted); } }
    .pnl-row.down .pnl-val { color: var(--fs-neg); }
    @media (prefers-reduced-motion: reduce) { .donut { animation: none; } .donut:hover { transform: none; } }
  `],
})
export class PortfolioVisualsComponent {
  readonly currency = input('');
  private readonly state = inject(OrdersState);

  readonly summary = this.state.summary;
  readonly slices = computed(() => allocation(this.state.positions(), this.summary()?.cash ?? 0));
  readonly gradient = computed(() => conicStops(this.slices(), s => this.color(s)));
  readonly bars = computed(() => pnlBars(this.state.positions()));
  readonly ret = computed(() => {
    const s = this.summary();
    return s && s.starting_cash > 0 ? ((s.equity - s.starting_cash) / s.starting_cash) * 100 : 0;
  });
  readonly donutLabel = computed(() => 'Allocation: ' + this.slices().map(s => `${s.label} ${(s.share * 100).toFixed(1)}%`).join(', '));

  color(s: Slice): string { return s.cash ? 'var(--tone-cash)' : `var(--tone-${s.tone})`; }
  pct(p: number | null): string { return signedPercent(p); }
  abs(n: number): number { return Math.abs(n); }
}
