import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import type { Subscription } from 'rxjs';

import { MarketService, describeHttpError } from '../services';
import type { BacktestParams, BacktestResult } from '../models';
import { chartSummary, curvePath, metricTiles, warningText } from './backtest-view';

const CHART_W = 640;
const CHART_H = 220;

/**
 * Replays a leaderboard strategy over past prices and reported quarters,
 * rebalanced on a schedule, against a benchmark. The server does the replay
 * (internal/backtest); this panel shows the result and its stated biases.
 */
@Component({
  selector: 'fs-backtest-panel',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<section class="bt" aria-labelledby="bt-title">
  <div class="bt-head">
    <h3 id="bt-title" class="bt-title"><i class="bi bi-clock-history me-2" aria-hidden="true"></i>Backtest: {{ strategyLabel() }}</h3>
    <div class="bt-controls">
      <div class="fs-segmented" role="group" aria-label="Holdings">
        @for (n of topOptions; track n) { <button type="button" [attr.aria-pressed]="top() === n" (click)="set('top', n)">Top {{ n }}</button> }
      </div>
      <div class="fs-segmented" role="group" aria-label="Rebalance">
        <button type="button" [attr.aria-pressed]="rebalance() === 'monthly'" (click)="set('rebalance', 'monthly')">Monthly</button>
        <button type="button" [attr.aria-pressed]="rebalance() === 'quarterly'" (click)="set('rebalance', 'quarterly')">Quarterly</button>
      </div>
      <div class="fs-segmented" role="group" aria-label="Period">
        <button type="button" [attr.aria-pressed]="years() === 3" (click)="set('years', 3)">3Y</button>
        <button type="button" [attr.aria-pressed]="years() === 5" (click)="set('years', 5)">5Y</button>
      </div>
    </div>
  </div>

  @if (loading()) {
    <div class="progress rounded-0" style="height: 2px" role="progressbar" aria-label="Replaying the strategy"><div class="progress-bar progress-bar-striped progress-bar-animated w-100"></div></div>
    <div class="bt-tiles placeholder-glow" aria-hidden="true">
      @for (i of skeleton; track i) { <div class="card fs-kpi"><span class="placeholder col-6"></span><span class="placeholder col-9"></span></div> }
    </div>
    <p class="bt-note">Replaying the strategy month by month on prices and quarters as they were known then. The first run of a universe takes a while; repeats are instant.</p>
  }

  @if (error(); as err) {
    <div class="fs-empty m-3" role="alert">
      <i class="bi bi-exclamation-triangle"></i>
      <h4>Could not run the backtest</h4>
      <p class="mb-0">{{ err }}</p>
      <button type="button" class="btn btn-sm btn-outline-secondary mt-1" (click)="run()">Try again</button>
    </div>
  } @else if (result(); as r) {
    <div class="bt-tiles">
      @for (t of tiles(); track t.id) {
        <div class="card fs-kpi" [attr.data-tone]="t.tone" [title]="t.hint">
          <span class="fs-kpi-label">{{ t.label }}</span>
          <span class="fs-kpi-value">
            @if (t.tone === 'pos') { <i class="bi bi-caret-up-fill" aria-hidden="true"></i> }
            @if (t.tone === 'neg') { <i class="bi bi-caret-down-fill" aria-hidden="true"></i> }
            {{ t.value }}
          </span>
        </div>
      }
    </div>

    <figure class="bt-chart">
      <svg [attr.viewBox]="'0 0 ' + w + ' ' + h" role="img" [attr.aria-label]="summary()" preserveAspectRatio="none">
        <path class="bench" [attr.d]="benchPath()" fill="none" />
        <path class="strategy" [attr.d]="strategyPath()" fill="none" />
      </svg>
      <figcaption class="bt-legend">
        <span><svg width="28" height="8" aria-hidden="true"><line x1="0" y1="4" x2="28" y2="4" class="strategy" /></svg>{{ strategyLabel() }}</span>
        @if (r.benchmark) { <span><svg width="28" height="8" aria-hidden="true"><line x1="0" y1="4" x2="28" y2="4" class="bench" /></svg>{{ r.benchmark }} (price index)</span> }
        <span class="ms-auto">{{ r.curve[0]?.date }} to {{ r.curve[r.curve.length - 1]?.date }}, 100 at the start</span>
      </figcaption>
    </figure>

    <div class="bt-warnings alert alert-warning" role="note">
      <strong>Read this before trusting the result</strong>
      <ul class="mb-0">
        @for (w of r.warnings; track w) { <li>{{ warningText(w) }}</li> }
        <li>Past results do not predict future ones.</li>
      </ul>
    </div>

    <div class="bt-foot">
      <button type="button" class="btn btn-sm btn-ghost" (click)="showTable.set(!showTable())" [attr.aria-expanded]="showTable()" aria-controls="bt-rebalances">
        {{ showTable() ? 'Hide' : 'Show' }} {{ r.rebalances.length }} rebalances
      </button>
    </div>
    @if (showTable()) {
      <div class="fs-scroll-x" id="bt-rebalances">
        <table class="table table-sm align-middle mb-0">
          <caption class="visually-hidden">Holdings chosen at each rebalance</caption>
          <thead><tr><th scope="col">Date</th><th scope="col">Holdings</th><th scope="col" class="num">Turnover</th><th scope="col" class="num">With fundamentals</th></tr></thead>
          <tbody>
            @for (b of r.rebalances; track b.date) {
              <tr><th scope="row">{{ b.date }}</th><td>{{ b.holdings.join(', ') }}</td><td class="num">{{ b.turnover_pct }}%</td><td class="num">{{ b.fundamentals_coverage_pct }}%</td></tr>
            }
          </tbody>
        </table>
      </div>
    }
  }
</section>
  `,
  styles: [`
:host { display: block; min-width: 0; border-top: 1px solid var(--fs-border); }
.bt { padding: 1rem 1.25rem; display: grid; gap: 0.9rem; }
.bt-head { display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: center; }
.bt-title { font-size: 0.95rem; margin: 0; margin-right: auto; }
.bt-controls { display: flex; flex-wrap: wrap; gap: 0.5rem; }
.bt-note { font-size: 0.75rem; color: var(--fs-muted); margin: 0; }
.bt-tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(8.5rem, 1fr)); gap: 0.6rem; }
.bt-tiles .fs-kpi { padding: 0.7rem 0.85rem; .fs-kpi-value { font-size: 1.15rem; } }
.fs-kpi[data-tone='pos'] .fs-kpi-value { color: var(--fs-pos); }
.fs-kpi[data-tone='neg'] .fs-kpi-value { color: var(--fs-neg); }
.bt-chart { margin: 0; svg:first-child { width: 100%; height: 14rem; display: block; background: var(--fs-surface-2, transparent); border-radius: 8px; } }
.strategy { stroke: var(--fs-accent); stroke-width: 2; vector-effect: non-scaling-stroke; }
.bench { stroke: var(--fs-muted); stroke-width: 1.75; stroke-dasharray: 6 4; vector-effect: non-scaling-stroke; }
.bt-legend { display: flex; flex-wrap: wrap; gap: 0.5rem 1.25rem; font-size: 0.75rem; color: var(--fs-muted); margin-top: 0.4rem;
  span { display: inline-flex; align-items: center; gap: 0.4rem; } }
.bt-warnings { font-size: 0.8rem; margin: 0; ul { padding-left: 1.1rem; margin-top: 0.25rem; } }
.bt-foot { display: flex; }
.num { text-align: right; white-space: nowrap; }
@media (max-width: 575.98px) { .bt { padding-inline: 1rem; } }
  `],
})
export class BacktestPanelComponent {
  /** The leaderboard universe id, or "custom" with symbols. */
  readonly universe = input.required<string>();
  readonly strategy = input.required<string>();
  readonly strategyLabel = input<string>('');
  readonly symbols = input<string[]>([]);

  private readonly api = inject(MarketService);
  readonly topOptions = [5, 10, 15] as const;
  readonly skeleton = [1, 2, 3, 4, 5, 6];
  readonly w = CHART_W;
  readonly h = CHART_H;
  readonly warningText = warningText;

  readonly top = signal<number>(10);
  readonly rebalance = signal<BacktestParams['rebalance']>('monthly');
  readonly years = signal<BacktestParams['years']>(5);
  readonly result = signal<BacktestResult | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  readonly showTable = signal(false);
  private request?: Subscription;

  readonly tiles = computed(() => (this.result() ? metricTiles(this.result()!.metrics) : []));
  readonly strategyPath = computed(() => curvePath(this.result()?.curve ?? [], CHART_W, CHART_H, 'equity'));
  readonly benchPath = computed(() => curvePath(this.result()?.curve ?? [], CHART_W, CHART_H, 'benchmark'));
  readonly summary = computed(() => (this.result() ? chartSummary(this.result()!) : ''));

  constructor() {
    inject(DestroyRef).onDestroy(() => this.request?.unsubscribe());
    // Runs once inputs are set, and again when the board switches universe or strategy.
    effect(() => {
      this.universe(); this.strategy(); this.symbols();
      untracked(() => this.run());
    });
  }

  set(key: 'top' | 'rebalance' | 'years', value: number | string): void {
    if (key === 'top') this.top.set(value as number);
    else if (key === 'rebalance') this.rebalance.set(value as BacktestParams['rebalance']);
    else this.years.set(value as BacktestParams['years']);
    this.run();
  }

  run(): void {
    this.request?.unsubscribe();
    this.loading.set(true);
    this.error.set('');
    this.request = this.api.backtest({
      universe: this.universe(), strategy: this.strategy(), symbols: this.symbols(),
      top: this.top(), rebalance: this.rebalance(), years: this.years(),
    }).subscribe({
      next: res => { this.result.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.result.set(null); this.loading.set(false); },
    });
  }
}
